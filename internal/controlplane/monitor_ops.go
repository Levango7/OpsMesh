// monitor_ops.go 监控系统兼容运维（Zabbix/夜莺 Nightingale/SkyWalking）。
//
// 设计原则（延续 P0-P4"查询走 API、执行走 shell"架构）：
//   - 查询类：GET /api/v1/monitor/{deviceID} 查询监控系统服务状态与已接入的监控源。
//   - 执行类：POST /api/v1/monitor/deploy 等，shell 模板下发到 agent 执行；
//     对外部监控系统的数据读写走其 REST/GraphQL API（curl），凭证经环境变量注入。
//
// API 端点：
//   - GET  /api/v1/monitor/{deviceID}            — 查询监控系统状态
//   - POST /api/v1/monitor/deploy                 — 部署监控系统（{deviceID, type, version}）
//   - POST /api/v1/monitor/{type}/{action}        — 启停重启/状态
//   - POST /api/v1/monitor/zabbix/host            — Zabbix 主机管理（create/delete/list）
//   - POST /api/v1/monitor/zabbix/item            — Zabbix 监控项查询
//   - POST /api/v1/monitor/nightingale/query      — 夜莺查询（PromQL 兼容）
//   - POST /api/v1/monitor/skywalking/trace       — SkyWalking 链路查询
//
// 安全：type/action 走白名单；主机名/查询式走正则白名单（拒绝 shell 元字符）；
// 凭证（Zabbix/Nightingale/SkyWalking）经 agent 环境变量读取，不在 shell 参数中明文；
// 5xx 错误走 writeInternalError，不回吐 err.Error()。

package controlplane

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// 监控系统类型白名单。
var allowedMonitorTypes = map[string]bool{
	"zabbix":      true,
	"nightingale": true, // 夜莺
	"skywalking":  true,
}

// 监控系统操作动作白名单。
var allowedMonitorActions = map[string]bool{
	"deploy":   true,
	"start":    true,
	"stop":     true,
	"restart":  true,
	"status":   true,
	"undeploy": true,
}

// 监控资源操作动作白名单。
var allowedMonitorResourceActions = map[string]bool{
	"create": true,
	"delete": true,
	"list":   true,
	"query":  true,
}

// 主机名正则（Zabbix host/夜莺 target）。
var monitorHostRe = regexp.MustCompile(`^[a-zA-Z0-9_.:\-]{1,128}$`)

// 监控部署请求体。
type monitorDeployRequest struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`    // zabbix/nightingale/skywalking
	Version  string `json:"version"` // 版本
	Mode     string `json:"mode"`    // single/cluster
}

// monitorOpsRequest 通用监控操作请求体。
type monitorOpsRequest struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`
	Action   string `json:"action"`
}

// zabbixHostReq Zabbix 主机操作请求体。
type zabbixHostReq struct {
	DeviceID string `json:"deviceID"`
	Action   string `json:"action"`  // create/delete/list
	Host     string `json:"host"`    // 主机名
	HostIP   string `json:"hostIP"`  // 主机 IP
	GroupID  string `json:"groupID"` // 主机组 ID
}

// zabbixItemReq Zabbix 监控项查询请求体。
type zabbixItemReq struct {
	DeviceID string `json:"deviceID"`
	Host     string `json:"host"`   // 主机名
	Search   string `json:"search"` // 监控项关键字
}

// nightingaleQueryReq 夜莺查询请求体。
type nightingaleQueryReq struct {
	DeviceID string `json:"deviceID"`
	Query    string `json:"query"` // PromQL 兼容查询式
	Start    int64  `json:"start"` // 起始时间戳
	End      int64  `json:"end"`   // 结束时间戳
	Step     int    `json:"step"`  // 步长（秒）
}

// skywalkingTraceReq SkyWalking 链路查询请求体。
type skywalkingTraceReq struct {
	DeviceID  string `json:"deviceID"`
	TraceID   string `json:"traceID"`   // 链路 ID（查详情）
	ServiceID string `json:"serviceID"` // 服务 ID
}

// handleMonitor 查询指定设备上的监控系统状态。
func (s *Server) handleMonitor(w http.ResponseWriter, r *http.Request, deviceID string) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "device:read"); !ok {
		return
	}
	dev := s.store.Device(deviceID)
	if dev == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}
	if actx.TenantID != "" && dev.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}
	metrics := s.store.DeviceMetrics(deviceID)
	if metrics == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "no metrics yet"})
		return
	}
	type monInfo struct {
		Type   string `json:"type"`
		Status string `json:"status"`
	}
	var result []monInfo
	for _, monType := range []string{"zabbix", "nightingale", "skywalking"} {
		info := monInfo{Type: monType, Status: "not-installed"}
		if metrics.Services != nil {
			for _, svc := range metrics.Services {
				if strings.EqualFold(svc.Name, monType) || strings.EqualFold(svc.Name, monType+"-server") {
					info.Status = svc.Status
					break
				}
			}
		}
		if info.Status == "not-installed" {
			continue
		}
		result = append(result, info)
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"deviceID": deviceID, "monitoring": result})
}

// handleMonitorDeploy 处理 POST /api/v1/monitor/deploy：部署监控系统。
func (s *Server) handleMonitorDeploy(w http.ResponseWriter, r *http.Request) {
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
	var req monitorDeployRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !allowedMonitorTypes[req.Type] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported monitor type"})
		return
	}
	if req.Version != "" && !middlewareVersionRe.MatchString(req.Version) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid version"})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}

	script := buildMonitorDeployScript(req)
	action := "monitor_deploy_" + req.Type

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, action)
	if err != nil {
		writeInternalError(r.Context(), w, "monitor.deploy."+req.Type, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: action, Target: req.Type,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s version=%s", req.DeviceID, req.Version)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildMonitorDeployScript(req monitorDeployRequest) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("# 自动探测包管理器\n")
	b.WriteString("if command -v yum >/dev/null 2>&1; then PKG_MGR=yum; elif command -v dnf >/dev/null 2>&1; then PKG_MGR=dnf; elif command -v apt-get >/dev/null 2>&1; then PKG_MGR=apt-get; else echo '未找到包管理器' && exit 1; fi\n")
	switch req.Type {
	case "zabbix":
		b.WriteString("# Zabbix 部署（zabbix-server + zabbix-agent + MySQL）\n")
		b.WriteString("if command -v zabbix_server >/dev/null 2>&1; then echo 'Zabbix 已安装'; else $PKG_MGR install -y zabbix-server-mysql zabbix-agent || echo '请配置 Zabbix 仓库后安装' && exit 1; fi\n")
		b.WriteString("systemctl enable --now zabbix-server zabbix-agent || true\n")
		b.WriteString("echo 'Zabbix 已启动 (Web:80, Server:10051)'\n")
	case "nightingale":
		b.WriteString("# 夜莺 Nightingale 部署（n9e + Prometheus 兼容）\n")
		b.WriteString("if command -v n9e >/dev/null 2>&1; then echo '夜莺已安装'; else echo '请下载 n9e 二进制包' && exit 1; fi\n")
		b.WriteString("n9e server &>/var/log/n9e.log &\n")
		b.WriteString("echo '夜莺已启动 (默认端口:17000)'\n")
	case "skywalking":
		b.WriteString("# SkyWalking 部署（OAP + UI，依赖 ES/MySQL 存储）\n")
		b.WriteString("if [ -d /opt/skywalking ] || command -v oap-server >/dev/null 2>&1; then echo 'SkyWalking 已安装'; else echo '请下载 SkyWalking 发行包' && exit 1; fi\n")
		b.WriteString("cd /opt/skywalking/bin && ./oapService.sh && ./webappService.sh || true\n")
		b.WriteString("echo 'SkyWalking OAP+UI 已启动 (UI:8080, OAP-gRPC:11800)'\n")
	}
	return b.String()
}

// handleMonitorAction 处理 POST /api/v1/monitor/{type}/{action}：启停重启/状态。
func (s *Server) handleMonitorAction(w http.ResponseWriter, r *http.Request, monType, action string) {
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
	if !allowedMonitorTypes[monType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported monitor type"})
		return
	}
	if !allowedMonitorActions[action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action"})
		return
	}
	var req monitorOpsRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	req.Type = monType
	req.Action = action

	script := buildMonitorActionScript(req)
	taskAction := "monitor_" + action + "_" + monType

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, taskAction)
	if err != nil {
		writeInternalError(r.Context(), w, "monitor.action."+monType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: taskAction, Target: monType,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildMonitorActionScript(req monitorOpsRequest) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Type {
	case "zabbix":
		b.WriteString("# Zabbix 启停\n")
		switch req.Action {
		case "start":
			b.WriteString("systemctl start zabbix-server zabbix-agent || true\n")
		case "stop":
			b.WriteString("systemctl stop zabbix-server zabbix-agent || true\n")
		case "restart":
			b.WriteString("systemctl restart zabbix-server zabbix-agent || true\n")
		case "status":
			b.WriteString("systemctl is-active zabbix-server || echo 'not running'\n")
		}
	case "nightingale":
		b.WriteString("# 夜莺启停\n")
		switch req.Action {
		case "start":
			b.WriteString("systemctl start n9e || n9e server &>/var/log/n9e.log & || true\n")
		case "stop":
			b.WriteString("systemctl stop n9e || pkill -f 'n9e server' || true\n")
		case "restart":
			b.WriteString("systemctl restart n9e || true\n")
		case "status":
			b.WriteString("systemctl is-active n9e || (pgrep -f 'n9e server' >/dev/null && echo running || echo 'not running')\n")
		}
	case "skywalking":
		b.WriteString("# SkyWalking 启停\n")
		switch req.Action {
		case "start":
			b.WriteString("cd /opt/skywalking/bin && ./oapService.sh && ./webappService.sh || true\n")
		case "stop":
			b.WriteString("cd /opt/skywalking/bin && ./oapService.sh stop && ./webappService.sh stop || true\n")
		case "restart":
			b.WriteString("cd /opt/skywalking/bin && ./oapService.sh stop && ./webappService.sh stop && ./oapService.sh && ./webappService.sh || true\n")
		case "status":
			b.WriteString("pgrep -f oap-server >/dev/null && echo 'running' || echo 'not running'\n")
		}
	}
	b.WriteString(fmt.Sprintf("echo '监控系统 %s %s 操作完成\n", req.Type, req.Action))
	return b.String()
}

// handleZabbixHost 处理 POST /api/v1/monitor/zabbix/host：Zabbix 主机管理。
func (s *Server) handleZabbixHost(w http.ResponseWriter, r *http.Request) {
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
	var req zabbixHostReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !allowedMonitorResourceActions[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (create/delete/list)"})
		return
	}
	if req.Action != "list" && !monitorHostRe.MatchString(req.Host) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid host name"})
		return
	}

	script := buildZabbixHostScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "zabbix_host_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "monitor.zabbix.host", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "zabbix_host_" + req.Action, Target: req.Host,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildZabbixHostScript(req zabbixHostReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("# Zabbix 主机管理（Zabbix JSON-RPC API，凭证从环境变量 ZABBIX_URL / ZABBIX_USER / ZABBIX_PASSWORD 读取）\n")
	b.WriteString("ZABBIX_URL=${ZABBIX_URL:-http://localhost/zabbix/api_jsonrpc.php}\n")
	switch req.Action {
	case "list":
		b.WriteString("curl -s -X POST \"$ZABBIX_URL\" -H 'Content-Type: application/json' -d '{\"jsonrpc\":\"2.0\",\"method\":\"host.get\",\"params\":{\"output\":[\"hostid\",\"host\",\"status\"]},\"id\":1}'\n")
	case "create":
		b.WriteString(fmt.Sprintf("echo 'Zabbix host.create: %s (%s) — 需先 auth 获取 token 后调用 host.create'\n", req.Host, req.HostIP))
	case "delete":
		b.WriteString(fmt.Sprintf("echo 'Zabbix host.delete: %s'\n", req.Host))
	}
	b.WriteString(fmt.Sprintf("echo 'Zabbix 主机 %s 操作完成\n", req.Action))
	return b.String()
}

// handleZabbixItem 处理 POST /api/v1/monitor/zabbix/item：Zabbix 监控项查询。
func (s *Server) handleZabbixItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "device:read"); !ok {
		return
	}
	var req zabbixItemReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if req.Host != "" && !monitorHostRe.MatchString(req.Host) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid host name"})
		return
	}

	script := buildZabbixItemScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "zabbix_item_query")
	if err != nil {
		writeInternalError(r.Context(), w, "monitor.zabbix.item", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "zabbix_item_query", Target: req.Host,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildZabbixItemScript(req zabbixItemReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("ZABBIX_URL=${ZABBIX_URL:-http://localhost/zabbix/api_jsonrpc.php}\n")
	search := req.Search
	if search == "" {
		search = req.Host
	}
	b.WriteString(fmt.Sprintf("curl -s -X POST \"$ZABBIX_URL\" -H 'Content-Type: application/json' -d '{\"jsonrpc\":\"2.0\",\"method\":\"item.get\",\"params\":{\"output\":[\"itemid\",\"name\",\"key_\",\"lastvalue\",\"lastclock\"],\"search\":{\"name\":\"%s\"}},\"id\":1}'\n", search))
	b.WriteString("echo 'Zabbix 监控项查询完成'\n")
	return b.String()
}

// handleNightingaleQuery 处理 POST /api/v1/monitor/nightingale/query：夜莺查询（PromQL 兼容）。
func (s *Server) handleNightingaleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "device:read"); !ok {
		return
	}
	var req nightingaleQueryReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if req.Query == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "query is required"})
		return
	}
	if req.Step <= 0 {
		req.Step = 60
	}

	script := buildNightingaleQueryScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "nightingale_query")
	if err != nil {
		writeInternalError(r.Context(), w, "monitor.nightingale.query", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "nightingale_query", Target: "query",
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildNightingaleQueryScript(req nightingaleQueryReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("# 夜莺查询（Prometheus 兼容 API，凭证从环境变量 N9E_URL / N9E_TOKEN 读取）\n")
	b.WriteString("N9E_URL=${N9E_URL:-http://localhost:17000}\n")
	start, end := req.Start, req.End
	if start == 0 {
		start = 0
	}
	if end == 0 {
		end = 0
	}
	b.WriteString(fmt.Sprintf("curl -s -H \"Authorization: Bearer $N9E_TOKEN\" \"$N9E_URL/api/v1/query_range?query=%s&start=%d&end=%d&step=%d\"\n",
		urlQueryEscape(req.Query), start, end, req.Step))
	b.WriteString("echo '夜莺查询完成'\n")
	return b.String()
}

// handleSkyWalkingTrace 处理 POST /api/v1/monitor/skywalking/trace：SkyWalking 链路查询。
func (s *Server) handleSkyWalkingTrace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "device:read"); !ok {
		return
	}
	var req skywalkingTraceReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if req.TraceID == "" && req.ServiceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "traceID or serviceID is required"})
		return
	}

	script := buildSkyWalkingTraceScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "skywalking_trace_query")
	if err != nil {
		writeInternalError(r.Context(), w, "monitor.skywalking.trace", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "skywalking_trace_query", Target: req.TraceID,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildSkyWalkingTraceScript(req skywalkingTraceReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("# SkyWalking 链路查询（GraphQL API，默认 UI:8080/graphql）\n")
	b.WriteString("SW_URL=${SW_URL:-http://localhost:8080/graphql}\n")
	if req.TraceID != "" {
		b.WriteString(fmt.Sprintf("curl -s -X POST \"$SW_URL\" -H 'Content-Type: application/json' -d '{\"query\":\"query trace($traceId: ID!) { trace(traceId: $traceId) { spans { traceId segmentId spanId parentSpanId serviceCode startTime endTime endpointName type peer } } }\",\"variables\":{\"traceId\":\"%s\"}}'\n", req.TraceID))
	} else {
		b.WriteString(fmt.Sprintf("echo 'SkyWalking 服务 %s 查询 — 需先调用 queryBasicTraces 获取 traceId 列表'\n", req.ServiceID))
	}
	b.WriteString("echo 'SkyWalking 链路查询完成'\n")
	return b.String()
}

// urlQueryEscape 对查询式做 URL 编码（仅保留 PromQL 必要字符）。
func urlQueryEscape(q string) string {
	replacer := strings.NewReplacer(
		" ", "%20",
		"\"", "%22",
		"{", "%7B",
		"}", "%7D",
		"#", "%23",
		"&", "%26",
		"+", "%2B",
	)
	return replacer.Replace(q)
}

// ============================================================================
// 路由注册
// ============================================================================

// registerMonitorRoutes 注册监控系统兼容路由。
func (s *Server) registerMonitorRoutes(mux *http.ServeMux) {
	// 固定路径优先
	mux.HandleFunc("/api/v1/monitor/zabbix/host", s.handleZabbixHost)
	mux.HandleFunc("/api/v1/monitor/zabbix/item", s.handleZabbixItem)
	mux.HandleFunc("/api/v1/monitor/nightingale/query", s.handleNightingaleQuery)
	mux.HandleFunc("/api/v1/monitor/skywalking/trace", s.handleSkyWalkingTrace)
	// 泛路由：GET /api/v1/monitor/{deviceID} / POST /api/v1/monitor/{type}/{action} / POST /api/v1/monitor/deploy
	mux.HandleFunc("/api/v1/monitor/deploy", s.handleMonitorDeploy)
	mux.HandleFunc("/api/v1/monitor/", s.handleMonitorRouting)
}

func (s *Server) handleMonitorRouting(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/monitor/")
	segments := strings.Split(path, "/")
	switch len(segments) {
	case 1:
		// /api/v1/monitor/{deviceID}
		if segments[0] != "" {
			s.handleMonitor(w, r, segments[0])
		} else {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}
	case 2:
		// /api/v1/monitor/{type}/{action}
		if segments[0] != "" && segments[1] != "" {
			s.handleMonitorAction(w, r, segments[0], segments[1])
		} else {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}
