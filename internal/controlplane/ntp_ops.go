// ntp_ops.go 时间同步运维（NTP/chrony：状态/校时/时间源/时区/服务端部署）。
//
// 背景：时间漂移会导致日志时间错乱、证书校验失败、集群脑裂误判、审计合规问题（等保要求）。
// security_ops.go 的 cis-time-01 只做"检查+启用"，本文件补齐配置管理形成闭环。
//
// 设计原则（延续 P0-P6"查询走 API、执行走 shell"架构）：全部动作 shell 模板下发到 agent。
//
// API 端点（POST /api/v1/ntp/{action}）：
//   - status   {deviceID}                    — 时间状态（timedatectl + chronyc tracking/sources）
//   - sync     {deviceID}                    — 立即校时（chronyc makestep / ntpdate 回退）
//   - config   {deviceID, servers[]}         — 配置时间源（写 chrony 配置并重启）
//   - timezone {deviceID, timezone}          — 设置时区（timedatectl set-timezone）
//   - deploy   {deviceID, servers[], allowCIDR} — 部署 chrony 服务端（内网时间源）
//   - service  {deviceID, action}            — chronyd 启停（start/stop/restart/status）
//
// 安全：时间源 host 走主机正则；时区走严格正则；网段走 CIDR 正则；5xx 错误走 writeInternalError。

package controlplane

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// NTP 动作白名单。
var allowedNTPActions = map[string]bool{
	"status":   true,
	"sync":     true,
	"config":   true,
	"timezone": true,
	"deploy":   true,
	"service":  true,
}

// NTP 服务操作白名单。
var allowedNTPServiceActions = map[string]bool{
	"start":   true,
	"stop":    true,
	"restart": true,
	"status":  true,
}

// 时区正则（Area/Location，如 Asia/Shanghai、UTC）。
var timezoneRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9_+-]+){0,2}$`)

// 网段 CIDR 正则（粗校验）。
var cidrRe = regexp.MustCompile(`^[0-9]{1,3}(\.[0-9]{1,3}){3}/[0-9]{1,2}$`)

// ntpReq NTP 请求体。
type ntpReq struct {
	DeviceID  string   `json:"deviceID"`
	Action    string   `json:"action"`    // status/sync/config/timezone/deploy/service
	Servers   []string `json:"servers"`   // 时间源（host 或 host:port 可选）
	Timezone  string   `json:"timezone"`  // 时区（timezone 动作用）
	AllowCIDR string   `json:"allowCIDR"` // 允许同步的网段（deploy 服务端用）
	Service   string   `json:"service"`   // chronyd 服务动作（service 动作用）
}

// handleNTP 处理 POST /api/v1/ntp/{action}。
func (s *Server) handleNTP(w http.ResponseWriter, r *http.Request, action string) {
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
	if !allowedNTPActions[action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (status/sync/config/timezone/deploy/service)"})
		return
	}
	var req ntpReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	req.Action = action

	// 输入校验。
	switch action {
	case "config", "deploy":
		if len(req.Servers) == 0 {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "servers is required"})
			return
		}
		for _, srv := range req.Servers {
			// 允许 host 或 host:port 形式；统一走主机正则（含端口时附带冒号数字）。
			hostPart := srv
			if i := strings.LastIndex(srv, ":"); i > 0 {
				hostPart = srv[:i]
			}
			if !monitorHostRe.MatchString(hostPart) {
				paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ntp server: " + srv})
				return
			}
		}
		if action == "deploy" && req.AllowCIDR != "" && !cidrRe.MatchString(req.AllowCIDR) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid allowCIDR"})
			return
		}
	case "timezone":
		if !timezoneRe.MatchString(req.Timezone) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid timezone (e.g. Asia/Shanghai)"})
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

	script := buildNTPScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "ntp_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "ntp."+action, err)
		return
	}
	target := action
	if action == "timezone" {
		target = req.Timezone
	}
	if action == "config" || action == "deploy" {
		target = strings.Join(req.Servers, ",")
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "ntp_" + action, Target: target,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildNTPScript(req ntpReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Action {
	case "status":
		b.WriteString("# 时间状态：timedatectl + chrony 跟踪/时间源\n")
		b.WriteString("timedatectl 2>/dev/null || date\n")
		b.WriteString("echo '=== chrony tracking ==='\n")
		b.WriteString("chronyc tracking 2>/dev/null || echo '(chrony 未安装或未运行)'\n")
		b.WriteString("echo '=== chrony sources ==='\n")
		b.WriteString("chronyc sources 2>/dev/null || echo '(无)'\n")
		b.WriteString("echo '=== ntpstat ==='\n")
		b.WriteString("ntpstat 2>/dev/null || true\n")

	case "sync":
		b.WriteString("# 立即校时（强制步进）\n")
		b.WriteString("if command -v chronyc >/dev/null 2>&1; then\n")
		b.WriteString("  chronyc makestep && echo 'chronyc makestep 完成'\n")
		b.WriteString("elif command -v ntpdate >/dev/null 2>&1; then\n")
		b.WriteString("  ntpdate -u pool.ntp.org && echo 'ntpdate 完成'\n")
		b.WriteString("else echo '无 chronyc/ntpdate，无法立即校时' && exit 1\nfi\n")
		b.WriteString("date\n")

	case "config":
		// 客户端配置：写 /etc/chrony.d/opsmesh.conf（drop-in），重启 chronyd。
		b.WriteString("# 配置 NTP 客户端时间源（drop-in，保留发行版默认配置）\n")
		b.WriteString("mkdir -p /etc/chrony.d /etc/chrony/conf.d 2>/dev/null || true\n")
		b.WriteString("CONF=/etc/chrony.conf\nCONF_DIR=\nfor d in /etc/chrony.d /etc/chrony/conf.d; do [ -d \"$d\" ] && CONF_DIR=\"$d/opsmesh.conf\" && break; done\n")
		b.WriteString("if [ -n \"$CONF_DIR\" ]; then TARGET=\"$CONF_DIR\"; else TARGET=/etc/chrony.conf; fi\n")
		b.WriteString("if [ \"$TARGET\" = \"/etc/chrony.conf\" ]; then\n")
		b.WriteString("  cp /etc/chrony.conf /etc/chrony.conf.bak.$(date +%s) 2>/dev/null || true\nfi\n")
		b.WriteString("cat > \"$TARGET\" <<'OPSMESH_NTP_EOF'\n")
		b.WriteString("# OpsMesh 托管：NTP 时间源配置\n")
		for _, srv := range req.Servers {
			b.WriteString(fmt.Sprintf("server %s iburst\n", srv))
		}
		b.WriteString("OPSMESH_NTP_EOF\n")
		b.WriteString("systemctl restart chronyd 2>/dev/null || systemctl restart chrony 2>/dev/null || service chronyd restart || true\n")
		b.WriteString("echo 'NTP 时间源已配置并重启 chronyd'\n")
		b.WriteString("chronyc sources 2>/dev/null | head -10 || true\n")

	case "deploy":
		// 服务端部署：安装 chrony、配置 allow 网段与上游时间源、开防火墙。
		b.WriteString("# 部署 chrony 服务端（内网时间源）\n")
		b.WriteString("if command -v yum >/dev/null 2>&1; then yum install -y chrony >/dev/null 2>&1 || true; elif command -v apt-get >/dev/null 2>&1; then apt-get install -y chrony >/dev/null 2>&1 || true; fi\n")
		b.WriteString("mkdir -p /etc/chrony.d /etc/chrony/conf.d 2>/dev/null || true\n")
		b.WriteString("CONF_DIR=\nfor d in /etc/chrony.d /etc/chrony/conf.d; do [ -d \"$d\" ] && CONF_DIR=\"$d/opsmesh-server.conf\" && break; done\n")
		b.WriteString("if [ -n \"$CONF_DIR\" ]; then TARGET=\"$CONF_DIR\"; else TARGET=/etc/chrony.conf; fi\n")
		b.WriteString("cat > \"$TARGET\" <<'OPSMESH_NTP_EOF'\n")
		b.WriteString("# OpsMesh 托管：chrony 服务端（内网时间源）\n")
		b.WriteString("local stratum 10\n")
		if req.AllowCIDR != "" {
			b.WriteString(fmt.Sprintf("allow %s\n", req.AllowCIDR))
		} else {
			b.WriteString("allow 0.0.0.0/0\n")
		}
		for _, srv := range req.Servers {
			b.WriteString(fmt.Sprintf("server %s iburst\n", srv))
		}
		b.WriteString("OPSMESH_NTP_EOF\n")
		b.WriteString("systemctl enable --now chronyd 2>/dev/null || systemctl enable --now chrony 2>/dev/null || true\n")
		b.WriteString("# 放行 NTP/123（firewalld）\n")
		b.WriteString("if command -v firewall-cmd >/dev/null 2>&1 && systemctl is-active firewalld >/dev/null 2>&1; then firewall-cmd --permanent --add-service=ntp >/dev/null 2>&1 || true; firewall-cmd --reload >/dev/null 2>&1 || true; fi\n")
		b.WriteString("echo 'chrony 服务端已部署'\n")
		b.WriteString("chronyc tracking 2>/dev/null | head -5 || true\n")

	case "timezone":
		b.WriteString("# 设置时区\n")
		b.WriteString(fmt.Sprintf("timedatectl set-timezone %s\n", req.Timezone))
		b.WriteString("timedatectl | grep -E 'Time zone|Local time' || true\n")

	case "service":
		b.WriteString("# chronyd 服务管理\n")
		switch req.Service {
		case "start":
			b.WriteString("systemctl start chronyd 2>/dev/null || systemctl start chrony 2>/dev/null || true\n")
		case "stop":
			b.WriteString("systemctl stop chronyd 2>/dev/null || systemctl stop chrony 2>/dev/null || true\n")
		case "restart":
			b.WriteString("systemctl restart chronyd 2>/dev/null || systemctl restart chrony 2>/dev/null || true\n")
		case "status":
			b.WriteString("systemctl is-active chronyd 2>/dev/null || systemctl is-active chrony 2>/dev/null || echo 'not running'\n")
		}
		b.WriteString(fmt.Sprintf("echo 'chronyd %s 操作完成'\n", req.Service))
	}
	return b.String()
}

// ============================================================================
// 路由注册
// ============================================================================

// registerNTPRoutes 注册 NTP 时间同步路由。
func (s *Server) registerNTPRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/ntp/status", s.handleNTPStatus)
	mux.HandleFunc("/api/v1/ntp/sync", s.handleNTPSync)
	mux.HandleFunc("/api/v1/ntp/config", s.handleNTPConfig)
	mux.HandleFunc("/api/v1/ntp/timezone", s.handleNTPTimezone)
	mux.HandleFunc("/api/v1/ntp/deploy", s.handleNTPDeploy)
	mux.HandleFunc("/api/v1/ntp/service", s.handleNTPService)
}

func (s *Server) handleNTPStatus(w http.ResponseWriter, r *http.Request) { s.handleNTP(w, r, "status") }
func (s *Server) handleNTPSync(w http.ResponseWriter, r *http.Request)   { s.handleNTP(w, r, "sync") }
func (s *Server) handleNTPConfig(w http.ResponseWriter, r *http.Request) { s.handleNTP(w, r, "config") }
func (s *Server) handleNTPTimezone(w http.ResponseWriter, r *http.Request) {
	s.handleNTP(w, r, "timezone")
}
func (s *Server) handleNTPDeploy(w http.ResponseWriter, r *http.Request) { s.handleNTP(w, r, "deploy") }
func (s *Server) handleNTPService(w http.ResponseWriter, r *http.Request) {
	s.handleNTP(w, r, "service")
}
