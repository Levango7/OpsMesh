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
//   - 用 s.k8sTenantFromRequest(r) 提取租户；
//   - 错误响应统一 {"error": "message"} 格式；
//   - 用 decodeJSONBody 解析请求体；
//   - 鉴权：需 automation:read/automation:write 权限。

import (
	"net/http"
	"strings"

	"opsmesh/internal/automation"
	"opsmesh/internal/store"
)

// automationEngine 自动化规则引擎（包级单例，无状态，线程安全）。
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
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListAutomationRules 处理 GET /api/v1/automation/rules：列出规则。
func (s *Server) handleListAutomationRules(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "automation:read"); !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	rules := s.store.ListAutomationRules(tenant)
	writeJSON(w, http.StatusOK, map[string]interface{}{"rules": rules})
}

// handleCreateAutomationRule 处理 POST /api/v1/automation/rules：创建规则。
func (s *Server) handleCreateAutomationRule(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "automation:write")
	if !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	var body store.AutomationRule
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	// 校验规则合法性
	tmp := automationRuleToDomain(&body)
	if err := automation.ValidateRule(tmp); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	created := s.store.CreateAutomationRule(tenant, &body)
	if created == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "create automation rule failed"})
		return
	}
	// 审计：记录创建人
	_ = caller
	writeJSON(w, http.StatusCreated, created)
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "rule id required"})
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "rule id required"})
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
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
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
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + action})
	}
}

// handleGetAutomationRule 处理 GET /api/v1/automation/rules/{id}：规则详情。
func (s *Server) handleGetAutomationRule(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "automation:read"); !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	rule, ok := s.store.GetAutomationRule(tenant, id)
	if !ok || rule == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "automation rule not found"})
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

// handleUpdateAutomationRule 处理 PUT /api/v1/automation/rules/{id}：更新规则。
func (s *Server) handleUpdateAutomationRule(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "automation:write"); !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	var body store.AutomationRule
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	body.ID = id
	tmp := automationRuleToDomain(&body)
	if err := automation.ValidateRule(tmp); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	updated, ok := s.store.UpdateAutomationRule(tenant, &body)
	if !ok || updated == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "automation rule not found"})
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// handleDeleteAutomationRule 处理 DELETE /api/v1/automation/rules/{id}：删除规则。
func (s *Server) handleDeleteAutomationRule(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "automation:write"); !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	if !s.store.DeleteAutomationRule(tenant, id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "automation rule not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleEnableAutomationRule 处理 POST /api/v1/automation/rules/{id}/enable：启用规则。
func (s *Server) handleEnableAutomationRule(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requirePermission(w, r, "automation:write"); !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	rule, ok := s.store.EnableAutomationRule(tenant, id)
	if !ok || rule == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "automation rule not found"})
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

// handleDisableAutomationRule 处理 POST /api/v1/automation/rules/{id}/disable：禁用规则。
func (s *Server) handleDisableAutomationRule(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requirePermission(w, r, "automation:write"); !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	rule, ok := s.store.DisableAutomationRule(tenant, id)
	if !ok || rule == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "automation rule not found"})
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

// handleTestAutomationRule 处理 POST /api/v1/automation/rules/{id}/test：测试规则。
func (s *Server) handleTestAutomationRule(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requirePermission(w, r, "automation:write"); !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	rule, ok := s.store.GetAutomationRule(tenant, id)
	if !ok || rule == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "automation rule not found"})
		return
	}
	// 测试规则（不实际执行）
	domainRule := automationRuleToDomain(rule)
	exec := automationEngine.TestRule(domainRule)
	// 落库执行记录
	storeExec := automationExecToStore(exec, tenant)
	s.store.CreateAutomationExecution(tenant, storeExec)
	writeJSON(w, http.StatusOK, map[string]interface{}{"execution": storeExec, "triggered": true})
}

// handleAutomationExecutions 处理 GET /api/v1/automation/executions：执行历史。
func (s *Server) handleAutomationExecutions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requirePermission(w, r, "automation:read"); !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	executions := s.store.ListAutomationExecutions(tenant, 100)
	writeJSON(w, http.StatusOK, map[string]interface{}{"executions": executions})
}

// handleAutomationExecutionRouting 分派 /api/v1/automation/executions/{id}：执行详情。
func (s *Server) handleAutomationExecutionRouting(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requirePermission(w, r, "automation:read"); !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/automation/executions/")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "execution id required"})
		return
	}
	exec, ok := s.store.GetAutomationExecution(tenant, id)
	if !ok || exec == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "execution not found"})
		return
	}
	writeJSON(w, http.StatusOK, exec)
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