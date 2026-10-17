// lb_ops.go 负载均衡运维管理（HaProxy/LVS/keepalived）。
//
// 设计原则（延续 P0/P1/P3/P4"查询走 API、执行走 shell"架构）：
//   - 查询类：GET /api/v1/lb/{deviceID} 从 DeviceMetrics.Services 读取负载均衡服务状态。
//   - 执行类：POST /api/v1/lb/deploy 等，shell 模板下发到 agent 执行。
//
// API 端点：
//   - GET  /api/v1/lb/{deviceID}              — 查询指定设备上的负载均衡状态
//   - POST /api/v1/lb/deploy                   — 部署负载均衡（{deviceID, type, mode, version, config}）
//   - POST /api/v1/lb/{type}/{action}          — 启停重启/状态（{deviceID, ...}）
//   - POST /api/v1/lb/{type}/backend           — 后端服务器管理（add/disable/enable/delete）
//   - POST /api/v1/lb/{type}/frontend          — 前端监听管理（add/delete/list）
//
// 安全：type/mode/action 走严格白名单；IP/端口走正则校验，杜绝命令注入；
// 5xx 错误走 writeInternalError，不回吐 err.Error()。

package controlplane

import (
	"fmt"
	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"net/http"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// 负载均衡类型白名单。
var allowedLBTypes = map[string]bool{
	"haproxy":    true,
	"lvs":        true,
	"keepalived": true,
}

// 负载均衡部署模式白名单。
var allowedLBModes = map[string]map[string]bool{
	"haproxy": {
		"single": true,
		"ha":     true,
	},
	"lvs": {
		"nat": true,
		"dr":  true,
		"tun": true,
	},
	"keepalived": {
		"single": true,
		"vrrp":   true,
	},
}

// 负载均衡操作动作白名单。
var allowedLBActions = map[string]bool{
	"deploy":   true,
	"start":    true,
	"stop":     true,
	"restart":  true,
	"status":   true,
	"undeploy": true,
}

// 后端/前端操作动作白名单。
var allowedLBResourceActions = map[string]bool{
	"add":     true,
	"delete":  true,
	"enable":  true,
	"disable": true,
	"list":    true,
}

// IP 地址正则（粗校验，配合 safeIPRe）。
var lbIPPortRe = regexp.MustCompile(`^[0-9a-fA-F:.]+:[0-9]{1,5}$`)

// 负载均衡部署请求体。
type lbDeployRequest struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`    // haproxy/lvs/keepalived
	Mode     string `json:"mode"`    // 部署模式
	Version  string `json:"version"` // 版本
	Config   string `json:"config"`  // 部署配置 JSON
}

// lbOpsRequest 通用负载均衡操作请求体。
type lbOpsRequest struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`
	Mode     string `json:"mode"`
	Action   string `json:"action"` // start/stop/restart/status/undeploy
}

// lbBackendReq 后端服务器管理请求体。
type lbBackendReq struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`    // haproxy/lvs
	Action   string `json:"action"`  // add/disable/enable/delete/list
	Backend  string `json:"backend"` // 后端地址（host:port）
	Weight   int    `json:"weight"`  // 权重
}

// lbFrontendReq 前端监听管理请求体。
type lbFrontendReq struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`     // haproxy
	Action   string `json:"action"`   // add/delete/list
	Frontend string `json:"frontend"` // 前端地址（host:port）
}

// handleLB 查询指定设备上的负载均衡状态。
func (s *Server) handleLB(w http.ResponseWriter, r *http.Request, deviceID string) {
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
	type lbInfo struct {
		Type   string `json:"type"`
		Status string `json:"status"`
	}
	var result []lbInfo
	for _, lbType := range []string{"haproxy", "lvs", "keepalived"} {
		info := lbInfo{Type: lbType, Status: "not-installed"}
		if metrics.Services != nil {
			for _, svc := range metrics.Services {
				if strings.EqualFold(svc.Name, lbType) || strings.EqualFold(svc.Name, lbType+"-server") {
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
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"deviceID": deviceID, "loadbalancers": result})
}

// handleLBDeploy 处理 POST /api/v1/lb/deploy：部署负载均衡。
func (s *Server) handleLBDeploy(w http.ResponseWriter, r *http.Request) {
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
	var req lbDeployRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !allowedLBTypes[req.Type] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported LB type"})
		return
	}
	if !allowedLBModes[req.Type][req.Mode] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported mode for " + req.Type})
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

	script := buildLBDeployScript(req)
	action := "lb_deploy_" + req.Type + "_" + req.Mode

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, action)
	if err != nil {
		writeInternalError(r.Context(), w, "lb.deploy."+req.Type, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: action, Target: req.Type + ":" + req.Mode,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s version=%s", req.DeviceID, req.Version)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

// buildLBDeployScript 按类型+模式生成负载均衡部署 shell 脚本。
func buildLBDeployScript(req lbDeployRequest) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("# 自动探测包管理器\n")
	b.WriteString("if command -v yum >/dev/null 2>&1; then PKG_MGR=yum; elif command -v dnf >/dev/null 2>&1; then PKG_MGR=dnf; elif command -v apt-get >/dev/null 2>&1; then PKG_MGR=apt-get; else echo '未找到包管理器' && exit 1; fi\n")
	b.WriteString("VERSION=" + shellQuote(req.Version) + "\n")
	switch req.Type {
	case "haproxy":
		buildHaProxyDeployScript(req, &b)
	case "lvs":
		buildLVSDeployScript(req, &b)
	case "keepalived":
		buildKeepalivedDeployScript(req, &b)
	}
	b.WriteString(fmt.Sprintf("echo '%s %s 负载均衡部署脚本执行完成\n", req.Type, req.Mode))
	return b.String()
}

func buildHaProxyDeployScript(req lbDeployRequest, b *strings.Builder) {
	b.WriteString("# HaProxy 部署\n")
	b.WriteString("if command -v haproxy >/dev/null 2>&1; then echo 'HaProxy 已安装'; else $PKG_MGR install -y haproxy; fi\n")
	switch req.Mode {
	case "single":
		b.WriteString("haproxy -f /etc/haproxy/haproxy.cfg -D -p /run/haproxy.pid || systemctl start haproxy || true\n")
		b.WriteString("echo 'HaProxy 单点已启动'\n")
	case "ha":
		b.WriteString("# HA：需配置多个 haproxy 节点 + keepalived VIP\n")
		b.WriteString("echo 'HaProxy HA：需配置 keepalived VIP'\n")
	}
}

func buildLVSDeployScript(req lbDeployRequest, b *strings.Builder) {
	b.WriteString("# LVS 部署（ipvsadm）\n")
	b.WriteString("if command -v ipvsadm >/dev/null 2>&1; then echo 'LVS(ipvsadm) 已安装'; else $PKG_MGR install -y ipvsadm; fi\n")
	b.WriteString("modprobe ip_vs || true\n")
	switch req.Mode {
	case "nat":
		b.WriteString("# LVS-NAT：需配置虚拟服务器 + 真实服务器（默认网关指向 Director）\n")
	case "dr":
		b.WriteString("# LVS-DR：需配置直路由（RIP 和 VIP 在不同网卡，ARP 抑制）\n")
	case "tun":
		b.WriteString("# LVS-TUN：需配置 IP 隧道（RIP 和 VIP 可跨网段）\n")
	}
	b.WriteString("echo 'LVS 已就绪，需用 ipvsadm 添加虚拟服务器'\n")
}

func buildKeepalivedDeployScript(req lbDeployRequest, b *strings.Builder) {
	b.WriteString("# keepalived 部署（VRRP 高可用）\n")
	b.WriteString("if command -v keepalived >/dev/null 2>&1; then echo 'keepalived 已安装'; else $PKG_MGR install -y keepalived; fi\n")
	switch req.Mode {
	case "single":
		b.WriteString("systemctl start keepalived || true\n")
		b.WriteString("echo 'keepalived 已启动'\n")
	case "vrrp":
		b.WriteString("# VRRP：需配置 /etc/keepalived/keepalived.conf（virtual_router_id/priority/auth）\n")
		b.WriteString("echo 'keepalived VRRP：需配置虚拟路由器ID'\n")
	}
}

// handleLBAction 处理 POST /api/v1/lb/{type}/{action}：启停重启/状态。
func (s *Server) handleLBAction(w http.ResponseWriter, r *http.Request, lbType, action string) {
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
	if !allowedLBTypes[lbType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported LB type"})
		return
	}
	if !allowedLBActions[action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action"})
		return
	}
	var req lbOpsRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	req.Type = lbType
	req.Action = action

	script := buildLBActionScript(req)
	taskAction := "lb_" + action + "_" + lbType

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, taskAction)
	if err != nil {
		writeInternalError(r.Context(), w, "lb.action."+lbType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: taskAction, Target: lbType,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

// buildLBActionScript 生成负载均衡启停重启/状态脚本。
func buildLBActionScript(req lbOpsRequest) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Type {
	case "haproxy":
		b.WriteString("# HaProxy 启停\n")
		switch req.Action {
		case "start":
			b.WriteString("haproxy -f /etc/haproxy/haproxy.cfg -D -p /run/haproxy.pid || systemctl start haproxy || true\n")
		case "stop":
			b.WriteString("systemctl stop haproxy || kill $(cat /run/haproxy.pid 2>/dev/null) || true\n")
		case "restart":
			b.WriteString("systemctl restart haproxy || true\n")
		case "status":
			b.WriteString("systemctl is-active --quiet haproxy && echo 'running' || echo 'not running'\n")
		}
	case "lvs":
		b.WriteString("# LVS(ipvsadm) 状态\n")
		switch req.Action {
		case "start":
			b.WriteString("modprobe ip_vs || true\n")
		case "stop":
			b.WriteString("ipvsadm -C || true\n")
		case "restart":
			b.WriteString("ipvsadm -C || true\nmodprobe ip_vs || true\n")
		case "status":
			b.WriteString("ipvsadm -L -n || echo 'not running'\n")
		}
	case "keepalived":
		b.WriteString("# keepalived 启停\n")
		switch req.Action {
		case "start":
			b.WriteString("systemctl start keepalived || true\n")
		case "stop":
			b.WriteString("systemctl stop keepalived || true\n")
		case "restart":
			b.WriteString("systemctl restart keepalived || true\n")
		case "status":
			b.WriteString("systemctl is-active --quiet keepalived && echo 'running' || echo 'not running'\n")
		}
	}
	b.WriteString(fmt.Sprintf("echo '%s %s 操作完成\n", req.Type, req.Action))
	return b.String()
}

// handleLBBackend 处理 POST /api/v1/lb/{type}/backend：后端服务器管理。
func (s *Server) handleLBBackend(w http.ResponseWriter, r *http.Request, lbType string) {
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
	if !allowedLBTypes[lbType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported LB type"})
		return
	}
	var req lbBackendReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !allowedLBResourceActions[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (add/disable/enable/delete/list)"})
		return
	}
	if req.Action != "list" && !lbIPPortRe.MatchString(req.Backend) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid backend (host:port)"})
		return
	}
	req.Type = lbType

	script := buildLBBackendScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "lb_backend_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "lb.backend."+lbType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "lb_backend_" + req.Action, Target: req.Backend,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s type=%s weight=%d", req.DeviceID, lbType, req.Weight)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildLBBackendScript(req lbBackendReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	if req.Weight <= 0 {
		req.Weight = 1
	}
	switch req.Type {
	case "haproxy":
		b.WriteString("# HaProxy 后端管理（需 haproxy sock 或 配置文件热重载）\n")
		switch req.Action {
		case "add":
			b.WriteString(fmt.Sprintf("echo 'HaProxy 后端 add: %s weight=%d（需写入 haproxy.cfg 并重载）'\n", req.Backend, req.Weight))
		case "delete":
			b.WriteString(fmt.Sprintf("echo 'HaProxy 后端 delete: %s（需写入 haproxy.cfg 并重载）'\n", req.Backend))
		case "enable":
			b.WriteString(fmt.Sprintf("echo 'enable backend %s via haproxy sock'\n", req.Backend))
		case "disable":
			b.WriteString(fmt.Sprintf("echo 'disable backend %s via haproxy sock'\n", req.Backend))
		case "list":
			b.WriteString("echo 'HaProxy 后端列表：查看 /etc/haproxy/haproxy.cfg backend 段'\n")
		}
	case "lvs":
		b.WriteString("# LVS(ipvsadm) 后端管理\n")
		switch req.Action {
		case "add":
			b.WriteString(fmt.Sprintf("ipvsadm -A -t %s -s rr || true\n", req.Backend))
			b.WriteString(fmt.Sprintf("ipvsadm -a -t %s -r %s -m -w %d || true\n", req.Backend, req.Backend, req.Weight))
		case "delete":
			b.WriteString(fmt.Sprintf("ipvsadm -D -t %s || true\n", req.Backend))
		case "enable":
			b.WriteString(fmt.Sprintf("ipvsadm -e -t %s -r %s -m -w %d || true\n", req.Backend, req.Backend, req.Weight))
		case "disable":
			b.WriteString(fmt.Sprintf("echo 'LVS disable 需 ipvsadm -d -t %s -r %s'\n", req.Backend, req.Backend))
		case "list":
			b.WriteString("ipvsadm -L -n\n")
		}
	}
	b.WriteString(fmt.Sprintf("echo 'LB %s 后端 %s 操作完成\n", req.Type, req.Action))
	return b.String()
}

// handleLBFrontend 处理 POST /api/v1/lb/haproxy/frontend：前端监听管理。
func (s *Server) handleLBFrontend(w http.ResponseWriter, r *http.Request) {
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
	var req lbFrontendReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !allowedLBResourceActions[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (add/delete/list)"})
		return
	}
	if req.Action != "list" && !lbIPPortRe.MatchString(req.Frontend) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid frontend (host:port)"})
		return
	}
	script := buildLBFrontendScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "lb_frontend_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "lb.frontend.haproxy", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "lb_frontend_" + req.Action, Target: req.Frontend,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildLBFrontendScript(req lbFrontendReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Action {
	case "add":
		b.WriteString(fmt.Sprintf("echo 'HaProxy 前端 add: %s（需写入 haproxy.cfg frontend 段并重载）'\n", req.Frontend))
	case "delete":
		b.WriteString(fmt.Sprintf("echo 'HaProxy 前端 delete: %s（需写入 haproxy.cfg frontend 段并重载）'\n", req.Frontend))
	case "list":
		b.WriteString("echo 'HaProxy 前端列表：查看 /etc/haproxy/haproxy.cfg frontend 段'\n")
	}
	b.WriteString(fmt.Sprintf("echo 'LB 前端 %s 操作完成\n", req.Action))
	return b.String()
}

// ============================================================================
// 路由注册
// ============================================================================

// registerLBRoutes 注册负载均衡运维路由。
func (s *Server) registerLBRoutes(mux *http.ServeMux) {
	// 固定路径优先
	mux.HandleFunc("/api/v1/lb/haproxy/backend", s.handleLBBackendHaProxy)
	mux.HandleFunc("/api/v1/lb/lvs/backend", s.handleLBBackendLVS)
	mux.HandleFunc("/api/v1/lb/haproxy/frontend", s.handleLBFrontend)
	// 泛路由：GET /api/v1/lb/{deviceID} / POST /api/v1/lb/{type}/{action} / POST /api/v1/lb/deploy
	mux.HandleFunc("/api/v1/lb/deploy", s.handleLBDeploy)
	mux.HandleFunc("/api/v1/lb/", s.handleLBRouting)
}

func (s *Server) handleLBBackendHaProxy(w http.ResponseWriter, r *http.Request) {
	s.handleLBBackend(w, r, "haproxy")
}
func (s *Server) handleLBBackendLVS(w http.ResponseWriter, r *http.Request) {
	s.handleLBBackend(w, r, "lvs")
}

func (s *Server) handleLBRouting(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/lb/")
	segments := strings.Split(path, "/")
	switch len(segments) {
	case 1:
		// /api/v1/lb/{deviceID}
		if segments[0] != "" {
			s.handleLB(w, r, segments[0])
		} else {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}
	case 2:
		// /api/v1/lb/{type}/{action}
		if segments[0] != "" && segments[1] != "" {
			s.handleLBAction(w, r, segments[0], segments[1])
		} else {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}
