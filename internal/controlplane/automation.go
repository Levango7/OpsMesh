package controlplane

// automation.go 实现 Phase 4 自动化闭环 HTTP handler（规则 CRUD + 启停 + 测试 + 执行历史）。
//
// API 端点：
//   - GET    /api/v1/automation/rules           列出规则
//   - POST   /api/v1/automation/rules           创建规则
//   - GET    /api/v1/automation/rules/{id}      规则详情
//   - PUT    /api/v1/automation/rules/{id}      更新规则
//   - DELETE /api/v1/automation/rules/{id}      删除规则
//   - POST   /api/v1/automation/rules/{id}/enable  启用规则
//   - POST   /api/v1/automation/rules/{id}/disable 禁用规则
//   - POST   /api/v1/automation/rules/{id}/test    测试规则
//   - GET    /api/v1/automation/executions      执行历史
//   - GET    /api/v1/automation/executions/{id}    执行详情
//
// 设计要点（与 traffic.go 风格一致）：
//   - 用 s.requireTenantContext(w, r) 提取租户；
//   - 错误响应统一 {"error": "message"} 格式；
//   - 用 decodeJSONBody 解析请求体；
//   - 鉴权：需 automation:read/automation:write 权限。
//   - 真实执行：通过 automationExecutor 接口执行动作（execute_task/send_notify/scale/restart/isolate）。

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"opsmesh/internal/controlplane/paginate"

	"opsmesh/internal/automation"
	"opsmesh/internal/events"
	"opsmesh/internal/notify"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// automationExecutor 实现 automation.Executor 接口，将动作路由到 store/notifier。
type automationExecutor struct {
	store    store.Store
	notifier *notify.Notifier
	bus      events.Bus
}

// ExecuteTask 在指定设备上创建并下发 shell 任务。
func (a *automationExecutor) ExecuteTask(tenantID, deviceID, command string, params map[string]string) (string, error) {
	task := &proto.Task{
		Type:     "shell",
		Command:  command,
		AgentID:  deviceID,
		TenantID: tenantID,
		Status:   "pending",
	}
	created := a.store.CreateTask(task)
	if created == nil {
		return "", fmt.Errorf("failed to create task for device %s", deviceID)
	}
	return created.TaskID, nil
}

// SendNotify 发送通知到指定通道。
func (a *automationExecutor) SendNotify(tenantID, channel, message string, params map[string]string) error {
	if a.notifier == nil {
		return fmt.Errorf("notifier not configured")
	}
	msg := &notify.Message{
		Title:     "自动化规则通知",
		Body:      message,
		Format:    "text",
		Severity:  params["severity"],
		Source:    tenantID,
		Timestamp: time.Now(),
		Data: map[string]string{
			"channel": channel,
			"tenant":  tenantID,
		},
	}
	return a.notifier.Notify(msg)
}

// Scale 扩缩容：创建 scale 任务。
func (a *automationExecutor) Scale(tenantID, service string, replicas int, params map[string]string) (string, error) {
	task := &proto.Task{
		Type:     "scale",
		Command:  fmt.Sprintf("scale %s to %d replicas", service, replicas),
		AgentID:  params["device_id"],
		TenantID: tenantID,
		Status:   "pending",
	}
	created := a.store.CreateTask(task)
	if created == nil {
		return "", fmt.Errorf("failed to create scale task for service %s", service)
	}
	return created.TaskID, nil
}

// Restart 重启：创建 restart 任务。
func (a *automationExecutor) Restart(tenantID, target string, params map[string]string) (string, error) {
	task := &proto.Task{
		Type:     "restart",
		Command:  fmt.Sprintf("restart %s", target),
		AgentID:  params["device_id"],
		TenantID: tenantID,
		Status:   "pending",
	}
	created := a.store.CreateTask(task)
	if created == nil {
		return "", fmt.Errorf("failed to create restart task for target %s", target)
	}
	return created.TaskID, nil
}

// Isolate 隔离：创建 isolate 任务。
func (a *automationExecutor) Isolate(tenantID, deviceID string, params map[string]string) (string, error) {
	task := &proto.Task{
		Type:     "isolate",
		Command:  fmt.Sprintf("isolate device %s", deviceID),
		AgentID:  deviceID,
		TenantID: tenantID,
		Status:   "pending",
	}
	created := a.store.CreateTask(task)
	if created == nil {
		return "", fmt.Errorf("failed to create isolate task for device %s", deviceID)
	}
	return created.TaskID, nil
}

// automationEngine 自动化规则引擎（包级单例，无状态，线程安全）。
// 真实执行器通过 SetExecutor 注入，未注入时返回模拟记录（向后兼容）。
var automationEngine = automation.NewEngine()

// handleAutomationRules 统一处理 /api/v1/automation/rules：
//   - GET：列出规则
//   - POST：创建规则
func (s *Server) handleAutomationRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListAutomationRules(w, r)
	case http.MethodPost:
		s.handleCreateAutomationRule(w, r)
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListAutomationRules 处理 GET /api/v1/automation/rules：列出规则。
func (s *Server) handleListAutomationRules(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "automation:read"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	rules := s.store.ListAutomationRules(actx.TenantID)
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"rules": rules})
}

// handleCreateAutomationRule 处理 POST /api/v1/automation/rules：创建规则。
func (s *Server) handleCreateAutomationRule(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "automation:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	var body store.AutomationRule
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	// 校验规则合法性
	tmp := automationRuleToDomain(&body)
	if err := automation.ValidateRule(tmp); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	created := s.store.CreateAutomationRule(actx.TenantID, &body)
	if created == nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "create automation rule failed"})
		return
	}
	// 审计：记录创建人（H9 写路径审计补齐，与 webhook/slo 风格一致）。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "automation_rule_create", Target: created.ID, Detail: sanitizeAuditDetail("name=" + created.Name),
	})
	paginate.WriteJSON(w, http.StatusCreated, created)
}

// handleAutomationRuleRouting 分派 /api/v1/automation/rules/{id} 子路径：
//   - GET    /api/v1/automation/rules/{id}        规则详情
//   - PUT    /api/v1/automation/rules/{id}        更新规则
//   - DELETE /api/v1/automation/rules/{id}        删除规则
//   - POST   /api/v1/automation/rules/{id}/enable  启用
//   - POST   /api/v1/automation/rules/{id}/disable 禁用
//   - POST   /api/v1/automation/rules/{id}/test    测试
func (s *Server) handleAutomationRuleRouting(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/automation/rules/")
	if rest == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "rule id required"})
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "rule id required"})
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			s.handleGetAutomationRule(w, r, id)
		case http.MethodPut:
			s.handleUpdateAutomationRule(w, r, id)
		case http.MethodDelete:
			s.handleDeleteAutomationRule(w, r, id)
		default:
			paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}
	action := parts[1]
	switch action {
	case "enable":
		s.handleEnableAutomationRule(w, r, id)
	case "disable":
		s.handleDisableAutomationRule(w, r, id)
	case "test":
		s.handleTestAutomationRule(w, r, id)
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + action})
	}
}

// handleGetAutomationRule 处理 GET /api/v1/automation/rules/{id}：规则详情。
func (s *Server) handleGetAutomationRule(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "automation:read"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	rule, ok := s.store.GetAutomationRule(actx.TenantID, id)
	if !ok || rule == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "automation rule not found"})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, rule)
}

// handleUpdateAutomationRule 处理 PUT /api/v1/automation/rules/{id}：更新规则。
func (s *Server) handleUpdateAutomationRule(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "automation:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	var body store.AutomationRule
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	body.ID = id
	tmp := automationRuleToDomain(&body)
	if err := automation.ValidateRule(tmp); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	updated, ok := s.store.UpdateAutomationRule(actx.TenantID, &body)
	if !ok || updated == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "automation rule not found"})
		return
	}
	// 审计：记录更新人（H9 写路径审计补齐）。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "automation_rule_update", Target: id, Detail: sanitizeAuditDetail("name=" + updated.Name),
	})
	paginate.WriteJSON(w, http.StatusOK, updated)
}

// handleDeleteAutomationRule 处理 DELETE /api/v1/automation/rules/{id}：删除规则。
func (s *Server) handleDeleteAutomationRule(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "automation:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	if !s.store.DeleteAutomationRule(actx.TenantID, id) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "automation rule not found"})
		return
	}
	// 审计：记录删除人（H9 写路径审计补齐）。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "automation_rule_delete", Target: id,
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleEnableAutomationRule 处理 POST /api/v1/automation/rules/{id}/enable：启用规则。
func (s *Server) handleEnableAutomationRule(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	caller, ok := s.requirePermission(w, r, "automation:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	rule, ok := s.store.EnableAutomationRule(actx.TenantID, id)
	if !ok || rule == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "automation rule not found"})
		return
	}
	// 审计：记录启用人（H9 写路径审计补齐）。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "automation_rule_enable", Target: id,
	})
	paginate.WriteJSON(w, http.StatusOK, rule)
}

// handleDisableAutomationRule 处理 POST /api/v1/automation/rules/{id}/disable：禁用规则。
func (s *Server) handleDisableAutomationRule(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	caller, ok := s.requirePermission(w, r, "automation:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	rule, ok := s.store.DisableAutomationRule(actx.TenantID, id)
	if !ok || rule == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "automation rule not found"})
		return
	}
	// 审计：记录禁用人（H9 写路径审计补齐）。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "automation_rule_disable", Target: id,
	})
	paginate.WriteJSON(w, http.StatusOK, rule)
}

// handleTestAutomationRule 处理 POST /api/v1/automation/rules/{id}/test：测试规则。
func (s *Server) handleTestAutomationRule(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requirePermission(w, r, "automation:write"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	rule, ok := s.store.GetAutomationRule(actx.TenantID, id)
	if !ok || rule == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "automation rule not found"})
		return
	}
	// 测试规则（不实际执行）
	domainRule := automationRuleToDomain(rule)
	exec := automationEngine.TestRule(domainRule)
	// 落库执行记录
	storeExec := automationExecToStore(exec, actx.TenantID)
	s.store.CreateAutomationExecution(actx.TenantID, storeExec)
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"execution": storeExec, "triggered": true})
}

// handleAutomationExecutions 处理 GET /api/v1/automation/executions：执行历史。
func (s *Server) handleAutomationExecutions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requirePermission(w, r, "automation:read"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	executions := s.store.ListAutomationExecutions(actx.TenantID, 100)
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"executions": executions})
}

// handleAutomationExecutionRouting 分派 /api/v1/automation/executions/{id}：执行详情。
func (s *Server) handleAutomationExecutionRouting(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requirePermission(w, r, "automation:read"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/automation/executions/")
	if id == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "execution id required"})
		return
	}
	exec, ok := s.store.GetAutomationExecution(actx.TenantID, id)
	if !ok || exec == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "execution not found"})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, exec)
}

// ============================================================================
// store.AutomationRule ↔ automation.Rule 转换
// ============================================================================

// automationRuleToDomain 把 store.AutomationRule 转为 automation.Rule（用于引擎校验/测试）。
func automationRuleToDomain(r *store.AutomationRule) *automation.Rule {
	if r == nil {
		return nil
	}
	actions := make([]automation.Action, 0, len(r.Actions))
	for _, a := range r.Actions {
		actions = append(actions, automation.Action{
			Type:   automation.ActionType(a.Type),
			Params: a.Params,
		})
	}
	return &automation.Rule{
		ID:          r.ID,
		TenantID:    r.TenantID,
		Name:        r.Name,
		Description: r.Description,
		Trigger: automation.Trigger{
			Type:   automation.TriggerType(r.TriggerType),
			Params: r.TriggerParams,
		},
		Actions: actions,
		Enabled: r.Enabled,
	}
}

// automationExecToStore 把 automation.Execution 转为 store.AutomationExecution（用于落库）。
func automationExecToStore(e *automation.Execution, tenantID string) *store.AutomationExecution {
	if e == nil {
		return nil
	}
	return &store.AutomationExecution{
		ID:        e.ID,
		TenantID:  tenantID,
		RuleID:    e.RuleID,
		RuleName:  e.RuleName,
		Status:    string(e.Status),
		Detail:    e.Detail,
		StartedAt: e.StartedAt,
		EndedAt:   e.EndedAt,
	}
}
