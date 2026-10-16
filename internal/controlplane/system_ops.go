// system_ops.go 实现系统运维操作：结构化查询 API + shell 模板执行。
//
// 设计原则（对应"查询走 API、执行走 shell"架构）：
//   - 查询类（用户/组/防火墙规则列表）：从 agent 采集的 DeviceMetrics.System 读取结构化数据，
//     返回 JSON 供前端表格渲染，避免前端解析 shell 文本。
//   - 执行类（增删改：useradd/chmod/iptables -A 等）：构造 shell 脚本作为 task 下发到 agent，
//     复用现有 store.CreateTask + agent executeShell 机制（含白名单 + 元字符防护 + 超时）。
//
// API 端点：
//   - GET  /api/v1/devices/{id}/system          — 结构化查询用户/组/防火墙（handleDeviceSystemInfo）
//   - POST /api/v1/system/users                 — 创建用户（shell 模板下发）
//   - DELETE /api/v1/system/users               — 删除用户（shell 模板下发）
//   - POST /api/v1/system/groups                — 创建用户组
//   - POST /api/v1/system/firewall              — 添加防火墙规则
//   - DELETE /api/v1/system/firewall            — 删除防火墙规则
//
// 参数校验：用户名/组名/端口走严格正则（^[a-zA-Z0-9_.-]+$），杜绝命令注入。
package controlplane

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// 系统操作参数校验正则（严格字符集，防命令注入）。
var (
	safeNameRe  = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)          // 用户名/组名/服务名
	safePortRe  = regexp.MustCompile(`^[0-9]{1,5}(:[0-9]{1,5})?$`) // 端口或端口范围
	safeProtoRe = regexp.MustCompile(`^(tcp|udp)$`)                // 协议
	safeIPRe    = regexp.MustCompile(`^[0-9a-fA-F:.]{1,45}$`)      // IPv4/IPv6 粗校验
)

// handleDeviceSystemInfo 处理 GET /api/v1/devices/{id}/system：返回结构化系统清单。
// 数据来源：agent 采集的 DeviceMetrics.System（用户/组/防火墙，5 分钟节流刷新）。
// 租户隔离：仅返回本租户设备。
func (s *Server) handleDeviceSystemInfo(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "device:read"); !ok {
		return
	}
	dev := s.store.Device(id)
	if dev == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}
	if actx.TenantID != "" && dev.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}
	metrics := s.store.DeviceMetrics(id)
	if metrics == nil || metrics.System == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{
			"error": "no system inventory yet (agent may not have reported; first collection within 5min)",
		})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, metrics.System)
}

// ============================================================================
// 执行类：shell 模板下发（用户/组/防火墙 CRUD）
// ============================================================================

// systemOpRequest 系统操作请求体。
type systemOpRequest struct {
	AgentID   string `json:"agentID"`
	Username  string `json:"username"`
	Group     string `json:"group"`
	Home      string `json:"home"`
	Shell     string `json:"shell"`
	GroupName string `json:"groupName"`
	Port      string `json:"port"`
	Protocol  string `json:"protocol"`
	Source    string `json:"source"`
	Action    string `json:"action"`   // firewall: add/delete
	RuleSpec  string `json:"ruleSpec"` // firewall: 原始规则（高级用法，需校验）
}

// handleSystemUsers 处理 /api/v1/system/users：
//   - POST：创建用户（useradd + 可选设 shell/家目录）
//   - DELETE：删除用户（userdel -r）
func (s *Server) handleSystemUsers(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	var req systemOpRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !safeNameRe.MatchString(req.Username) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid username (allowed: a-zA-Z0-9_.-)"})
		return
	}
	if req.AgentID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "agentID is required"})
		return
	}

	var script string
	var action string
	switch r.Method {
	case http.MethodPost:
		action = "user_add"
		script = buildUserAddScript(req)
	case http.MethodDelete:
		action = "user_del"
		script = fmt.Sprintf("set -e\nif id %q >/dev/null 2>&1; then userdel -r %q; echo 'user deleted: %s'; else echo 'user not found: %s'; fi\n", req.Username, req.Username, req.Username, req.Username)
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	task, err := s.createSystemTask(actx.TenantID, req.AgentID, script, action)
	if err != nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: action, Target: req.Username,
		Detail: sanitizeAuditDetail(fmt.Sprintf("agentID=%s group=%s", req.AgentID, req.Group)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

// buildUserAddScript 构造创建用户的 shell 脚本（幂等：已存在则跳过）。
func buildUserAddScript(req systemOpRequest) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString(fmt.Sprintf("if id %q >/dev/null 2>&1; then echo 'user already exists: %s'; exit 0; fi\n", req.Username, req.Username))
	args := fmt.Sprintf("%q", req.Username)
	if req.Group != "" && safeNameRe.MatchString(req.Group) {
		args += fmt.Sprintf(" -g %q", req.Group)
	}
	if req.Home != "" && strings.HasPrefix(req.Home, "/") {
		args += fmt.Sprintf(" -d %q -m", req.Home)
	}
	if req.Shell != "" && strings.HasPrefix(req.Shell, "/") {
		args += fmt.Sprintf(" -s %q", req.Shell)
	}
	b.WriteString(fmt.Sprintf("useradd %s\n", args))
	b.WriteString(fmt.Sprintf("echo 'user created: %s'\n", req.Username))
	return b.String()
}

// handleSystemGroups 处理 /api/v1/system/groups：
//   - POST：创建用户组
//   - DELETE：删除用户组
func (s *Server) handleSystemGroups(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	var req systemOpRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !safeNameRe.MatchString(req.GroupName) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid group name (allowed: a-zA-Z0-9_.-)"})
		return
	}
	if req.AgentID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "agentID is required"})
		return
	}

	var script, action string
	switch r.Method {
	case http.MethodPost:
		action = "group_add"
		script = fmt.Sprintf("set -e\nif getent group %q >/dev/null 2>&1; then echo 'group already exists: %s'; exit 0; fi\ngroupadd %q\necho 'group created: %s'\n", req.GroupName, req.GroupName, req.GroupName, req.GroupName)
	case http.MethodDelete:
		action = "group_del"
		script = fmt.Sprintf("set -e\nif getent group %q >/dev/null 2>&1; then groupdel %q; echo 'group deleted: %s'; else echo 'group not found: %s'; fi\n", req.GroupName, req.GroupName, req.GroupName, req.GroupName)
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	task, err := s.createSystemTask(actx.TenantID, req.AgentID, script, action)
	if err != nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: action, Target: req.GroupName,
		Detail: sanitizeAuditDetail("agentID=" + req.AgentID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

// handleSystemFirewall 处理 /api/v1/system/firewall：
//   - POST：添加防火墙规则（firewalld 优先，回退 iptables）
//   - DELETE：删除防火墙规则
func (s *Server) handleSystemFirewall(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	var req systemOpRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.AgentID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "agentID is required"})
		return
	}
	// 校验端口/协议/源地址。
	if req.Port != "" && !safePortRe.MatchString(req.Port) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid port"})
		return
	}
	if req.Protocol != "" && !safeProtoRe.MatchString(req.Protocol) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid protocol (tcp/udp)"})
		return
	}
	if req.Source != "" && !safeIPRe.MatchString(req.Source) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid source address"})
		return
	}

	var script, action string
	switch r.Method {
	case http.MethodPost:
		action = "firewall_add"
		script = buildFirewallScript(req, true)
	case http.MethodDelete:
		action = "firewall_del"
		script = buildFirewallScript(req, false)
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	task, err := s.createSystemTask(actx.TenantID, req.AgentID, script, action)
	if err != nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: action, Target: req.Port + "/" + req.Protocol,
		Detail: sanitizeAuditDetail("agentID=" + req.AgentID + " source=" + req.Source),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

// buildFirewallScript 构造防火墙规则增删脚本（firewalld 优先，回退 iptables）。
// add=true 添加，false 删除。
func buildFirewallScript(req systemOpRequest, add bool) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	portSpec := req.Port
	if req.Protocol != "" && !strings.Contains(portSpec, "/") {
		portSpec = req.Port + "/" + req.Protocol
	}
	if req.Port == "" {
		portSpec = ""
	}
	b.WriteString("if command -v firewall-cmd >/dev/null 2>&1 && systemctl is-active --quiet firewalld; then\n")
	if portSpec != "" {
		verb := "--add-port"
		if !add {
			verb = "--remove-port"
		}
		b.WriteString(fmt.Sprintf("  firewall-cmd --permanent %s=%s && firewall-cmd --reload\n", verb, portSpec))
	}
	b.WriteString("else\n")
	// iptables 回退。
	chain := "INPUT"
	action := "ACCEPT"
	if !add {
		action = "ACCEPT" // 删除时按规则匹配删除
	}
	if req.Port != "" {
		proto := req.Protocol
		if proto == "" {
			proto = "tcp"
		}
		if add {
			b.WriteString(fmt.Sprintf("  iptables -A %s -p %s --dport %s -j %s\n", chain, proto, req.Port, action))
		} else {
			b.WriteString(fmt.Sprintf("  iptables -D %s -p %s --dport %s -j %s || true\n", chain, proto, req.Port, action))
		}
	}
	b.WriteString("fi\n")
	b.WriteString("echo 'firewall rule applied'\n")
	return b.String()
}

// createSystemTask 下发 shell 任务到指定 agent（复用 store.CreateTask）。
func (s *Server) createSystemTask(tenantID, agentID, script, action string) (*proto.Task, error) {
	task := &proto.Task{
		AgentID:    agentID,
		TenantID:   tenantID,
		Type:       proto.TaskTypeShell,
		Command:    script,
		Status:     "pending",
		Timeout:    120, // 2 分钟超时
		MaxRetries: 1,
		CreatedAt:  time.Now(),
	}
	return s.store.CreateTask(task)
}

// _ 确保 strconv 被使用（预留：未来端口数值校验）。
var _ = strconv.Atoi
