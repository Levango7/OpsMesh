// system_ops.go 实现系统运维操作：结构化查询 API + shell 模板执行。
//
// 设计原则（对应"查询走 API、执行走 shell"架构）：
//   - 查询类（用户/组/防火墙规则列表）：从 agent 采集的 DeviceMetrics.System 读取结构化数据，
//     返回 JSON 供前端表格渲染，避免前端解析 shell 文本。
//   - 执行类（增删改：useradd/chmod/iptables -A/yum install 等）：构造 shell 脚本作为 task 下发到 agent，
//     复用现有 store.CreateTask + agent executeShell 机制（含白名单 + 元字符防护 + 超时）。
//
// API 端点：
//   - GET  /api/v1/devices/{id}/system          — 结构化查询用户/组/防火墙（handleDeviceSystemInfo）
//   - POST /api/v1/system/users                 — 创建用户（shell 模板下发）
//   - DELETE /api/v1/system/users               — 删除用户（shell 模板下发）
//   - POST /api/v1/system/groups                — 创建用户组
//   - POST /api/v1/system/firewall              — 添加防火墙规则
//   - DELETE /api/v1/system/firewall            — 删除防火墙规则
//   - POST /api/v1/system/packages              — 安装/升级包（shell 模板下发，yum/apt）
//   - DELETE /api/v1/system/packages            — 卸载包（shell 模板下发，yum/apt）
//
// 参数校验：用户名/组名/端口/包名走严格正则，杜绝命令注入。
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
	safeNameRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`) // 用户名/组名/服务名
	// 端口或端口范围：1-65535
	safePortRe         = regexp.MustCompile(`^(?:(?:[1-9][0-9]{0,3}|[1-5][0-9]{4}|6[0-4][0-9]{3}|65[0-4][0-9]{2}|655[0-2][0-9]|6553[0-5]))(?::(?:[1-9][0-9]{0,3}|[1-5][0-9]{4}|6[0-4][0-9]{3}|65[0-4][0-9]{2}|655[0-2][0-9]|6553[0-5]))?$`)
	safeProtoRe        = regexp.MustCompile(`^(tcp|udp)$`)           // 协议
	safeIPRe           = regexp.MustCompile(`^[0-9a-fA-F:.]{1,45}$`) // IPv4/IPv6 粗校验
	safeJournalSinceRe = regexp.MustCompile(`^[a-zA-Z0-9 :\-.]+$`)   // journalctl --since 参数（防注入）
	// 包名：允许字母数字点下划线连字符，兼容 nginx-1.20 / git-all 等；不允许元字符，杜绝注入。
	safePackageRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)
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

	// userdel 是高风险操作，需审批通过后才下发。
	var task *proto.Task
	var err error
	if r.Method == http.MethodDelete {
		task, err = s.createSystemTask(actx.TenantID, req.AgentID, script, action, true)
	} else {
		task, err = s.createSystemTask(actx.TenantID, req.AgentID, script, action)
	}
	if err != nil {
		writeInternalError(r.Context(), w, "system.user", err)
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
	fmt.Fprintf(&b, "if id %q >/dev/null 2>&1; then echo 'user already exists: %s'; exit 0; fi\n", req.Username, req.Username)
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
	fmt.Fprintf(&b, "useradd %s\n", args)
	fmt.Fprintf(&b, "echo 'user created: %s'\n", req.Username)
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

	// groupdel 是高风险操作，需审批通过后才下发。
	var task *proto.Task
	var err error
	if r.Method == http.MethodDelete {
		task, err = s.createSystemTask(actx.TenantID, req.AgentID, script, action, true)
	} else {
		task, err = s.createSystemTask(actx.TenantID, req.AgentID, script, action)
	}
	if err != nil {
		writeInternalError(r.Context(), w, "system.group", err)
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

	task, err := s.createSystemTask(actx.TenantID, req.AgentID, script, action, true)
	if err != nil {
		writeInternalError(r.Context(), w, "system.firewall", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: action, Target: req.Port + "/" + req.Protocol,
		Detail: sanitizeAuditDetail("agentID=" + req.AgentID + " source=" + req.Source),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

// handleListFirewallRules 处理 GET /api/v1/system/firewall/rules：结构化查询防火墙规则。
// 数据来源：agent 采集的 DeviceMetrics.System.Firewall（iptables/firewalld 统一视图）。
// 租户隔离：仅返回本租户 agent 的防火墙规则；agent 必须属于调用方租户（requireTenantAgent）。
func (s *Server) handleListFirewallRules(w http.ResponseWriter, r *http.Request) {
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
	agentID := r.URL.Query().Get("agentID")
	if agentID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "agentID is required"})
		return
	}
	// 校验 agent 存在且属于调用方租户（防止跨租户查询防火墙规则）。
	if _, ok := s.requireTenantAgent(w, agentID, actx.TenantID); !ok {
		return
	}
	// 从设备监控指标中读取防火墙规则（MVP agent 即设备，deviceID=agentID）。
	metrics := s.store.DeviceMetrics(agentID)
	if metrics == nil || metrics.System == nil {
		paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"agentID":     agentID,
			"firewall":    []proto.FirewallRule{},
			"backend":     "",
			"collectedAt": time.Time{},
		})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"agentID":     agentID,
		"firewall":    metrics.System.Firewall,
		"backend":     metrics.System.FirewallBackend,
		"collectedAt": metrics.System.CollectedAt,
	})
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
		fmt.Fprintf(&b, "  firewall-cmd --permanent %s=%s && firewall-cmd --reload\n", verb, portSpec)
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
			fmt.Fprintf(&b, "  iptables -A %s -p %s --dport %s -j %s\n", chain, proto, req.Port, action)
		} else {
			fmt.Fprintf(&b, "  iptables -D %s -p %s --dport %s -j %s || true\n", chain, proto, req.Port, action)
		}
	}
	b.WriteString("fi\n")
	b.WriteString("echo 'firewall rule applied'\n")
	return b.String()
}

// ============================================================================
// 执行类：包管理（yum/apt，shell 模板下发）
// ============================================================================

// packageOpRequest 包管理请求体。
type packageOpRequest struct {
	AgentID string `json:"agentID"`
	Package string `json:"package"`
	Action  string `json:"action"`  // install/remove/upgrade
	Manager string `json:"manager"` // yum/apt/auto（auto 自动探测）
}

// allowedPackageActions 包操作白名单。
var allowedPackageActions = map[string]bool{
	"install": true,
	"remove":  true,
	"upgrade": true,
}

// handleSystemPackages 处理 POST/DELETE /api/v1/system/packages：
//   - POST  安装或升级包（action=install/upgrade）
//   - DELETE 卸载包（action=remove）
//
// Body: {agentID, package, action, manager}
// 安全：package 名走 safePackageRe 校验；action 走白名单；manager 限 yum/apt/auto。
func (s *Server) handleSystemPackages(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	var req packageOpRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !safePackageRe.MatchString(req.Package) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid package name (allowed: a-zA-Z0-9_.-)"})
		return
	}
	if !allowedPackageActions[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid action (allowed: install/remove/upgrade)"})
		return
	}
	if req.Manager != "" && req.Manager != "yum" && req.Manager != "apt" && req.Manager != "auto" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid manager (allowed: yum/apt/auto)"})
		return
	}
	if req.AgentID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "agentID is required"})
		return
	}

	script := buildPackageScript(req)
	action := "pkg_" + req.Action

	task, err := s.createSystemTask(actx.TenantID, req.AgentID, script, action, true)
	if err != nil {
		writeInternalError(r.Context(), w, "system.package", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: action, Target: req.Package,
		Detail: sanitizeAuditDetail(fmt.Sprintf("agentID=%s manager=%s", req.AgentID, req.Manager)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

// buildPackageScript 构造包管理 shell 脚本（yum/apt 自动探测，幂等）。
func buildPackageScript(req packageOpRequest) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("# 自动探测包管理器：yum（RHEL/CentOS/Fedora）或 apt（Debian/Ubuntu）\n")
	b.WriteString("if command -v yum >/dev/null 2>&1; then\n")
	fmt.Fprintf(&b, "  PKG_MGR=yum\n")
	b.WriteString("elif command -v dnf >/dev/null 2>&1; then\n")
	fmt.Fprintf(&b, "  PKG_MGR=dnf\n")
	b.WriteString("elif command -v apt-get >/dev/null 2>&1; then\n")
	fmt.Fprintf(&b, "  PKG_MGR=apt-get\n")
	b.WriteString("else\n")
	fmt.Fprintf(&b, "  echo '未找到 yum/dnf/apt-get 包管理器' && exit 1\n")
	b.WriteString("fi\n")
	fmt.Fprintf(&b, "PKG=%q\n", req.Package)
	switch req.Action {
	case "install":
		b.WriteString("if $PKG_MGR list installed \"$PKG\" >/dev/null 2>&1; then\n")
		fmt.Fprintf(&b, "  echo '包已安装: $PKG'\n")
		b.WriteString("else\n")
		if req.Manager == "apt" || req.Manager == "apt-get" {
			b.WriteString("  apt-get update -qq && apt-get install -y \"$PKG\"\n")
		} else {
			b.WriteString("  $PKG_MGR install -y \"$PKG\"\n")
		}
		b.WriteString("fi\n")
		fmt.Fprintf(&b, "echo '包安装完成: $PKG'\n")
	case "remove":
		b.WriteString("if $PKG_MGR list installed \"$PKG\" >/dev/null 2>&1; then\n")
		fmt.Fprintf(&b, "  $PKG_MGR remove -y \"$PKG\"\n")
		b.WriteString("  echo '包已卸载: $PKG'\n")
		b.WriteString("else\n")
		fmt.Fprintf(&b, "  echo '包未安装: $PKG'\n")
		b.WriteString("fi\n")
	case "upgrade":
		fmt.Fprintf(&b, "  $PKG_MGR install -y \"$PKG\"\n")
		fmt.Fprintf(&b, "echo '包已升级: $PKG'\n")
	}
	return b.String()
}

// createSystemTask 下发 shell 任务到指定 agent（复用 store.CreateTask）。
// approvalRequired 为 true 时任务下发前需管理员审批（进入 pending_approval 状态）。
func (s *Server) createSystemTask(tenantID, agentID, script, action string, approvalRequired ...bool) (*proto.Task, error) {
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
	if len(approvalRequired) > 0 && approvalRequired[0] {
		task.ApprovalRequired = true
	}
	return s.store.CreateTask(task)
}

// _ 确保 strconv 被使用（预留：未来端口数值校验）。
var _ = strconv.Atoi
