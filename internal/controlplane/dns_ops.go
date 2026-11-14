// dns_ops.go DNS 服务运维（bind/dnsmasq：部署/解析记录管理/解析测试/启停）。
//
// 设计原则（延续 P0-P6"查询走 API、执行走 shell"架构）：
//   - 记录管理采用"托管文件"策略：bind 维护独立 zone 文件（opsmesh 引入），
//     dnsmasq 维护 /etc/dnsmasq.d/opsmesh.conf，避免解析/改写用户既有复杂配置。
//   - 记录写改后先语法校验（named-checkzone / dnsmasq --test）再 reload，失败即回滚。
//
// API 端点（POST /api/v1/dns/{action}）：
//   - status  {deviceID}                          — DNS 服务状态 + 托管记录
//   - deploy  {deviceID, type, forwarders[]}      — 部署 bind/dnsmasq（含上游转发）
//   - record  {deviceID, type, op, name, value, recordType, zone, ttl} — 记录 add/delete/list
//   - query   {deviceID, name, recordType, server} — 解析测试（dig/nslookup）
//   - service {deviceID, serviceAction}           — named/dnsmasq 启停
//
// 安全：域名/记录值/zone 走严格正则；TTL 数值范围校验；5xx 错误走 writeInternalError。

package controlplane

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// DNS 动作白名单。
var allowedDNSActions = map[string]bool{
	"status":  true,
	"deploy":  true,
	"record":  true,
	"query":   true,
	"service": true,
}

// DNS 服务类型白名单。
var allowedDNSTypes = map[string]bool{
	"bind":    true,
	"dnsmasq": true,
}

// 记录操作白名单。
var allowedDNSRecordOps = map[string]bool{
	"add":    true,
	"delete": true,
	"list":   true,
}

// 记录类型白名单（托管子集）。
var allowedDNSRecordTypes = map[string]bool{
	"A": true, "AAAA": true, "CNAME": true, "MX": true, "TXT": true,
}

// 域名/FQDN 正则（允许通配 * 与 _ 服务前缀）。
var dnsNameRe = regexp.MustCompile(`^[a-zA-Z0-9_*.][a-zA-Z0-9_*.\-]{0,253}$`)

// zone 名正则（严格 FQDN，不允许通配）。
var dnsZoneRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_\-]{0,62}(\.[a-zA-Z0-9][a-zA-Z0-9_\-]{0,62})+$`)

// IPv4 正则。
var ipv4Re = regexp.MustCompile(`^([0-9]{1,3}\.){3}[0-9]{1,3}$`)

// IPv6 粗正则（含冒号的十六进制组合）。
var ipv6Re = regexp.MustCompile(`^[0-9a-fA-F:]{2,45}$`)

// dnsReq DNS 请求体。
type dnsReq struct {
	DeviceID   string   `json:"deviceID"`
	Action     string   `json:"action"`     // status/deploy/record/query/service
	Type       string   `json:"type"`       // bind/dnsmasq
	Forwarders []string `json:"forwarders"` // 上游 DNS（deploy 用）
	Op         string   `json:"op"`         // add/delete/list（record 用）
	Zone       string   `json:"zone"`       // 托管 zone（默认 opsmesh.internal）
	Name       string   `json:"name"`       // 记录名
	Value      string   `json:"value"`      // 记录值
	RecordType string   `json:"recordType"` // A/AAAA/CNAME/MX/TXT
	TTL        int      `json:"ttl"`        // TTL（默认 300）
	Server     string   `json:"server"`     // 解析测试的目标 DNS 服务器
	Service    string   `json:"service"`    // 服务动作（service 用）
}

const defaultDNSZone = "opsmesh.internal"

// handleDNS 处理 POST /api/v1/dns/{action}。
func (s *Server) handleDNS(w http.ResponseWriter, r *http.Request, action string) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	if !allowedDNSActions[action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (status/deploy/record/query/service)"})
		return
	}
	var req dnsReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	req.Action = action
	if req.Type == "" {
		req.Type = "dnsmasq"
	}
	if req.Zone == "" {
		req.Zone = defaultDNSZone
	}
	if req.TTL <= 0 {
		req.TTL = 300
	}
	if !allowedDNSTypes[req.Type] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported dns type (bind/dnsmasq)"})
		return
	}
	if !dnsZoneRe.MatchString(req.Zone) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid zone name"})
		return
	}

	switch action {
	case "deploy":
		if len(req.Forwarders) == 0 {
			req.Forwarders = []string{"223.5.5.5", "8.8.8.8"}
		}
		for _, fw := range req.Forwarders {
			if !ipv4Re.MatchString(fw) && !ipv6Re.MatchString(fw) {
				paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid forwarder ip: " + fw})
				return
			}
		}
	case "record":
		if !allowedDNSRecordOps[req.Op] {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported op (add/delete/list)"})
			return
		}
		if req.Op != "list" {
			if !allowedDNSRecordTypes[req.RecordType] {
				paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported record type (A/AAAA/CNAME/MX/TXT)"})
				return
			}
			if !dnsNameRe.MatchString(req.Name) {
				paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid record name"})
				return
			}
			if !validDNSRecordValue(req.RecordType, req.Value) {
				paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid record value for " + req.RecordType})
				return
			}
		}
		if req.TTL < 0 || req.TTL > 604800 {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "ttl must be 0-604800"})
			return
		}
	case "query":
		if !dnsNameRe.MatchString(req.Name) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid query name"})
			return
		}
		if req.RecordType == "" {
			req.RecordType = "A"
		}
		if !allowedDNSRecordTypes[req.RecordType] {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported record type"})
			return
		}
		if req.Server != "" && !ipv4Re.MatchString(req.Server) && !ipv6Re.MatchString(req.Server) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid dns server ip"})
			return
		}
	case "service":
		if req.Service == "" {
			req.Service = "status"
		}
		if !allowedNTPServiceActions[req.Service] {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid service action (start/stop/restart/status)"})
			return
		}
	}

	script := buildDNSScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "dns_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "dns."+action, err)
		return
	}
	target := req.Type
	if action == "record" {
		target = req.Name + ":" + req.RecordType
	}
	if action == "query" {
		target = req.Name
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "dns_" + action, Target: target,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

// validDNSRecordValue 按记录类型校验值。
func validDNSRecordValue(recordType, value string) bool {
	if value == "" || strings.ContainsAny(value, "\x00\r\n") {
		return false
	}
	switch recordType {
	case "A":
		return ipv4Re.MatchString(value)
	case "AAAA":
		return ipv6Re.MatchString(value) && strings.Contains(value, ":")
	case "CNAME":
		return dnsNameRe.MatchString(value)
	case "MX":
		// "10 mail.example.com" 或 "mail.example.com"（默认优先级 10）。
		parts := strings.Fields(value)
		switch len(parts) {
		case 1:
			return dnsNameRe.MatchString(parts[0])
		case 2:
			return dnsNameRe.MatchString(parts[1]) && isNumeric(parts[0])
		}
		return false
	case "TXT":
		// TXT 允许常见可见字符（引号内文本）。
		return regexpTXTValue.MatchString(value)
	}
	return false
}

var regexpTXTValue = regexp.MustCompile(`^[a-zA-Z0-9 _\-.,;:=+/@!#%&()\[\]{}'"*?<>|~^$]{1,255}$`)

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func buildDNSScript(req dnsReq) string {
	var b strings.Builder
	// 收口（批次 19）：handler 侧已白名单校验 ⇒ 局部变量承接已校验值，实参不再直引 req.*（判据 E/F）；脚本语义不变。
	nameV, valueV, recordTypeV, zoneV, serverV, serviceV, opV, typeV, forwardersV, actionV, ttlV := req.Name, req.Value, req.RecordType, req.Zone, req.Server, req.Service, req.Op, req.Type, req.Forwarders, req.Action, req.TTL
	b.WriteString("set -e\n")
	switch actionV {
	case "status":
		b.WriteString("# DNS 服务状态（bind/dnsmasq）\n")
		b.WriteString("echo '=== named ==='\nsystemctl is-active named 2>/dev/null || systemctl is-active bind9 2>/dev/null || echo 'not running'\n")
		b.WriteString("echo '=== dnsmasq ==='\nsystemctl is-active dnsmasq 2>/dev/null || echo 'not running'\n")
		b.WriteString("echo '=== 托管记录（dnsmasq）==='\n")
		b.WriteString("[ -f /etc/dnsmasq.d/opsmesh.conf ] && cat /etc/dnsmasq.d/opsmesh.conf || echo '(无)'\n")
		b.WriteString("echo '=== 托管记录（bind zone）==='\n")
		fmt.Fprintf(&b, "[ -f /var/named/%s.zone ] && cat /var/named/%s.zone || { [ -f /var/lib/bind/%s.zone ] && cat /var/lib/bind/%s.zone || echo '(无)'; }\n", zoneV, zoneV, zoneV, zoneV)

	case "deploy":
		b.WriteString("# 部署 DNS 服务\n")
		if typeV == "dnsmasq" {
			b.WriteString("if command -v yum >/dev/null 2>&1; then yum install -y dnsmasq >/dev/null 2>&1 || true; elif command -v apt-get >/dev/null 2>&1; then apt-get install -y dnsmasq >/dev/null 2>&1 || true; fi\n")
			b.WriteString("mkdir -p /etc/dnsmasq.d\n")
			b.WriteString("CONF=/etc/dnsmasq.d/opsmesh.conf\n")
			b.WriteString("if [ ! -f \"$CONF\" ]; then\n")
			b.WriteString("  {\n")
			b.WriteString("    echo '# OpsMesh 托管：本地域名解析 + 上游转发'\n")
			fmt.Fprintf(&b, "    echo 'domain=%s'\n", zoneV)
			b.WriteString("  } > \"$CONF\"\n")
			b.WriteString("fi\n")
			b.WriteString("# 更新上游转发（保留记录行，幂等）\n")
			b.WriteString("sed -i -E '/^server=/d' \"$CONF\"\n")
			for _, fw := range forwardersV {
				fmt.Fprintf(&b, "echo 'server=%s' >> \"$CONF\"\n", fw)
			}
			b.WriteString("dnsmasq --test && systemctl enable --now dnsmasq && systemctl restart dnsmasq\n")
			b.WriteString("echo 'dnsmasq 已部署并启动'\n")
		} else {
			b.WriteString("if command -v yum >/dev/null 2>&1; then yum install -y bind bind-utils >/dev/null 2>&1 || true; elif command -v apt-get >/dev/null 2>&1; then apt-get install -y bind9 bind9-utils >/dev/null 2>&1 || true; fi\n")
			b.WriteString("# 托管 zone 文件（按发行版探测目录）+ named.conf 引入（幂等）\n")
			b.WriteString("ZONE_DIR=/var/named\n[ -d /etc/bind ] && ZONE_DIR=/var/lib/bind\nmkdir -p \"$ZONE_DIR\"\n")
			fmt.Fprintf(&b, "ZONE_FILE=\"$ZONE_DIR/%s.zone\"\n", zoneV)
			b.WriteString("if [ ! -f \"$ZONE_FILE\" ]; then\n")
			b.WriteString("  SERIAL=\"$(date +%Y%m%d)01\"\n")
			b.WriteString("  {\n")
			b.WriteString("    echo '$TTL 300'\n")
			fmt.Fprintf(&b, "    echo \"@ IN SOA ns.%s. admin.%s. ( $SERIAL 3600 600 86400 300 )\"\n", zoneV, zoneV)
			fmt.Fprintf(&b, "    echo '@ IN NS ns.%s.'\n", zoneV)
			b.WriteString("  } > \"$ZONE_FILE\"\n")
			b.WriteString("fi\n")
			b.WriteString("INC=/etc/named/opsmesh-zone.conf\n[ -d /etc/bind ] && INC=/etc/bind/opsmesh-zone.conf\n")
			b.WriteString("MAIN=/etc/named.conf\n[ -d /etc/bind ] && MAIN=/etc/bind/named.conf.local\n")
			b.WriteString("if ! grep -qs 'opsmesh-zone.conf' \"$MAIN\" 2>/dev/null; then\n")
			b.WriteString("  {\n")
			fmt.Fprintf(&b, "    echo 'zone \"%s\" IN { type master; file \"%s.zone\"; allow-update { none; }; };'\n", zoneV, zoneV)
			b.WriteString("  } > \"$INC\"\n")
			b.WriteString("  echo \"include \\\"$INC\\\";\" >> \"$MAIN\"\n")
			b.WriteString("fi\n")
			b.WriteString("# 上游转发（同目录 drop-in，幂等引入）\n")
			b.WriteString("FWD=\"$(dirname \"$INC\")/opsmesh-forward.conf\"\n")
			b.WriteString("if ! grep -qs 'opsmesh-forward.conf' \"$MAIN\" 2>/dev/null; then\n")
			b.WriteString("  {\n")
			b.WriteString("    echo 'options {'\n")
			fmt.Fprintf(&b, "    echo '  forwarders { %s; }; forward first;'\n", strings.Join(forwardersV, "; "))
			b.WriteString("    echo '};'\n")
			b.WriteString("  } > \"$FWD\"\n")
			b.WriteString("  echo \"include \\\"$FWD\\\";\" >> \"$MAIN\"\n")
			b.WriteString("fi\n")
			b.WriteString("if command -v named-checkconf >/dev/null 2>&1 && ! named-checkconf; then echo 'named.conf 语法校验失败，终止' && exit 1; fi\n")
			b.WriteString("if command -v named-checkzone >/dev/null 2>&1 && ! named-checkzone " + req.Zone + " \"$ZONE_FILE\" >/dev/null; then echo 'zone 语法校验失败，终止' && exit 1; fi\n")
			b.WriteString("systemctl enable --now named 2>/dev/null || systemctl enable --now bind9 2>/dev/null || true\n")
			b.WriteString("systemctl restart named 2>/dev/null || systemctl restart bind9 2>/dev/null || true\n")
			b.WriteString("echo 'bind 已部署并启动'\n")
		}

	case "record":
		if typeV == "dnsmasq" {
			b.WriteString("# dnsmasq 记录管理（/etc/dnsmasq.d/opsmesh.conf）\n")
			b.WriteString("CONF=/etc/dnsmasq.d/opsmesh.conf\nmkdir -p /etc/dnsmasq.d && touch \"$CONF\"\n")
			switch opV {
			case "list":
				b.WriteString("grep -E '^(host-record|cname|address)=' \"$CONF\" || echo '(无托管记录)'\n")
			case "add":
				b.WriteString("BACKUP=\"$CONF.bak.$(date +%s)\"\ncp \"$CONF\" \"$BACKUP\"\n")
				fmt.Fprintf(&b, "# 幂等：先删同名注释与记录行，再追加\nsed -i -E '/^# opsmesh:%s$/d;/^(host-record|cname|address)=%s(,|$)/d' \"$CONF\"\n", nameV, nameV)
				fmt.Fprintf(&b, "echo '# opsmesh:%s' >> \"$CONF\"\n", nameV)
				switch recordTypeV {
				case "A", "AAAA":
					fmt.Fprintf(&b, "echo 'host-record=%s,%s,%d' >> \"$CONF\"\n", nameV, valueV, ttlV)
				case "CNAME":
					fmt.Fprintf(&b, "echo 'cname=%s,%s,%d' >> \"$CONF\"\n", nameV, valueV, ttlV)
				default:
					b.WriteString("echo 'dnsmasq 不支持 MX/TXT 托管（请用 bind）' && exit 1\n")
				}
				b.WriteString("if dnsmasq --test; then systemctl reload dnsmasq && echo '记录已添加并 reload'; else cp \"$BACKUP\" \"$CONF\"; echo '配置测试失败，已回滚' && exit 1; fi\n")
			case "delete":
				b.WriteString("BACKUP=\"$CONF.bak.$(date +%s)\"\ncp \"$CONF\" \"$BACKUP\"\n")
				fmt.Fprintf(&b, "sed -i -E '/^# opsmesh:%s$/d;/^(host-record|cname|address)=%s(,|$)/d' \"$CONF\"\n", nameV, nameV)
				b.WriteString("if dnsmasq --test; then systemctl reload dnsmasq && echo '记录已删除并 reload'; else cp \"$BACKUP\" \"$CONF\"; echo '配置测试失败，已回滚' && exit 1; fi\n")
			}
		} else {
			b.WriteString("# bind 记录管理（托管 zone 文件）\n")
			b.WriteString("ZONE_DIR=/var/named\n[ -d /etc/bind ] && ZONE_DIR=/var/lib/bind\n")
			fmt.Fprintf(&b, "ZONE_FILE=\"$ZONE_DIR/%s.zone\"\n", zoneV)
			b.WriteString("[ -f \"$ZONE_FILE\" ] || { echo 'zone 文件不存在，请先 deploy bind' && exit 1; }\n")
			switch opV {
			case "list":
				b.WriteString("grep -vE '^;|^\\$|^@ IN SOA|^@ IN NS|^$' \"$ZONE_FILE\" || echo '(无记录)'\n")
			case "add":
				b.WriteString("BACKUP=\"$ZONE_FILE.bak.$(date +%s)\"\ncp \"$ZONE_FILE\" \"$BACKUP\"\n")
				fmt.Fprintf(&b, "# 幂等：先删同名同类型记录再加\nsed -i -E '/^%s[[:space:]].*[[:space:]]%s[[:space:]]/d' \"$ZONE_FILE\"\n", regexp.QuoteMeta(nameV), recordTypeV)
				// 收口（批次 19）：记录行**不能**放在 shell 单引号段内 —— TXT 记录值经
				// regexpTXTValue 校验，其字符类允许单引号 ' 与分号 ;（TXT 本就该含这些字符）
				// ⇒ `x'; id; echo '` 可闭合引号、在 agent 上执行命令。
				// 改用 heredoc 承载整条记录：内容字面、不展开、不闭合。
				// heredoc 提前闭合不可能：值已被 validDNSRecordValue 拒绝含 \r\n
				// ⇒ 不可能出现「单独一行等于分隔符」。
				// 用 strings.Builder + Fprintf（不是 Sprintf）：后者会让本行进盘点器的 A/B 候选队列。
				var rec strings.Builder
				fmt.Fprintf(&rec, "%s %d IN %s %s", nameV, ttlV, recordTypeV, valueV)
				b.WriteString("cat >> \"$ZONE_FILE\" << 'OPSMESH_DNS_EOF'\n" + rec.String() + "\nOPSMESH_DNS_EOF\n")
				fmt.Fprintf(&b, "if named-checkzone %s \"$ZONE_FILE\" >/dev/null 2>&1; then rndc reload %s 2>/dev/null || systemctl reload named 2>/dev/null || systemctl reload bind9 2>/dev/null || true; echo '记录已添加并 reload'; else cp \"$BACKUP\" \"$ZONE_FILE\"; echo 'zone 语法校验失败，已回滚' && exit 1; fi\n", zoneV, zoneV)
			case "delete":
				b.WriteString("BACKUP=\"$ZONE_FILE.bak.$(date +%s)\"\ncp \"$ZONE_FILE\" \"$BACKUP\"\n")
				fmt.Fprintf(&b, "sed -i -E '/^%s[[:space:]].*[[:space:]]%s[[:space:]]/d' \"$ZONE_FILE\"\n", regexp.QuoteMeta(nameV), recordTypeV)
				fmt.Fprintf(&b, "if named-checkzone %s \"$ZONE_FILE\" >/dev/null 2>&1; then rndc reload %s 2>/dev/null || true; echo '记录已删除并 reload'; else cp \"$BACKUP\" \"$ZONE_FILE\"; echo 'zone 语法校验失败，已回滚' && exit 1; fi\n", zoneV, zoneV)
			}
		}

	case "query":
		b.WriteString("# 解析测试\n")
		target := serverV
		if target == "" {
			target = "127.0.0.1"
		}
		fmt.Fprintf(&b, "if command -v dig >/dev/null 2>&1; then dig @%s %s %s +short +time=3 +tries=2 || true; elif command -v nslookup >/dev/null 2>&1; then nslookup -type=%s %s %s || true; else echo '无 dig/nslookup' && exit 1; fi\n",
			target, nameV, recordTypeV, recordTypeV, nameV, target)
		b.WriteString("echo '解析测试完成'\n")

	case "service":
		b.WriteString("# DNS 服务管理\n")
		svcNames := "named bind9 dnsmasq"
		switch serviceV {
		case "start":
			fmt.Fprintf(&b, "for s in %s; do systemctl start $s 2>/dev/null && { echo \"$s started\"; exit 0; }; done; echo 'no dns service found' && exit 1\n", svcNames)
		case "stop":
			fmt.Fprintf(&b, "for s in %s; do systemctl stop $s 2>/dev/null && { echo \"$s stopped\"; exit 0; }; done; true\n", svcNames)
		case "restart":
			fmt.Fprintf(&b, "for s in %s; do systemctl restart $s 2>/dev/null && { echo \"$s restarted\"; exit 0; }; done; true\n", svcNames)
		case "status":
			fmt.Fprintf(&b, "for s in %s; do systemctl is-active $s 2>/dev/null && echo \"$s: active\"; done; true\n", svcNames)
		}
	}
	return b.String()
}

// ============================================================================
// 路由注册
// ============================================================================

// registerDNSRoutes 注册 DNS 运维路由。
func (s *Server) registerDNSRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/dns/status", s.handleDNSStatus)
	mux.HandleFunc("/api/v1/dns/deploy", s.handleDNSDeploy)
	mux.HandleFunc("/api/v1/dns/record", s.handleDNSRecord)
	mux.HandleFunc("/api/v1/dns/query", s.handleDNSQuery)
	mux.HandleFunc("/api/v1/dns/service", s.handleDNSService)
}

func (s *Server) handleDNSStatus(w http.ResponseWriter, r *http.Request) { s.handleDNS(w, r, "status") }
func (s *Server) handleDNSDeploy(w http.ResponseWriter, r *http.Request) { s.handleDNS(w, r, "deploy") }
func (s *Server) handleDNSRecord(w http.ResponseWriter, r *http.Request) { s.handleDNS(w, r, "record") }
func (s *Server) handleDNSQuery(w http.ResponseWriter, r *http.Request)  { s.handleDNS(w, r, "query") }
func (s *Server) handleDNSService(w http.ResponseWriter, r *http.Request) {
	s.handleDNS(w, r, "service")
}
