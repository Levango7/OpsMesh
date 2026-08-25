// automation_test.go 测试 Phase 4 自动化闭环 HTTP handler（automation.go）。
//
// 覆盖范围：
//   - handleListAutomationRules：空列表、创建后列表
//   - handleCreateAutomationRule：正常创建、缺必填字段、无效触发器类型
//   - handleGetAutomationRule：正常获取、不存在
//   - handleUpdateAutomationRule：正常更新
//   - handleDeleteAutomationRule：正常删除
//   - handleEnableAutomationRule/handleDisableAutomationRule：启停
//   - handleTestAutomationRule：测试规则
//   - handleAutomationExecutions：执行历史
//   - 鉴权：无 token 返回 401
//
// 测试策略（与 ticket_test.go 风格一致）：
//   - 白盒（package controlplane），直接装配 Server{store: MemoryStore, jwtSecret: 固定}；
//   - 鉴权用例通过 admin 登录获取 token（requirePermission 校验 automation:read/write）；
//   - 用 httptest.NewRequest + httptest.NewRecorder 直接调用 handler，断言 status code 与响应体。
package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opsmesh/internal/config"
	"opsmesh/internal/store"
)

// newAutomationTestServer 构造自动化 API 测试用 Server。
func newAutomationTestServer() *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3},
		jwtSecret:    []byte("test-jwt-secret-for-automation-test-32bytes!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

// =============================================================================
// handleListAutomationRules（GET /api/v1/automation/rules）
// =============================================================================

// TestHandleListAutomationRules_Empty 验证空列表返回 200 + rules:[]。
func TestHandleListAutomationRules_Empty(t *testing.T) {
	s := newAutomationTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/automation/rules", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAutomationRules(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Rules []*store.AutomationRule `json:"rules"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Rules) != 0 {
		t.Fatalf("rules=%d, want 0", len(resp.Rules))
	}
}

// TestHandleListAutomationRules_AfterCreate 验证创建后列表含 1 个规则。
func TestHandleListAutomationRules_AfterCreate(t *testing.T) {
	s := newAutomationTestServer()
	auth := loginAsAdmin(t, s)

	s.store.CreateAutomationRule("default", &store.AutomationRule{
		Name:        "cpu-alert-scale",
		TriggerType: "metric_threshold",
		TriggerParams: map[string]string{"metric": "cpu", "threshold": "90"},
		Actions: []store.AutomationAction{
			{Type: "scale", Params: map[string]string{"target": "web", "replicas": "+1"}},
		},
		Enabled: true,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/automation/rules", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAutomationRules(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Rules []*store.AutomationRule `json:"rules"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Rules) != 1 {
		t.Fatalf("rules=%d, want 1", len(resp.Rules))
	}
}

// =============================================================================
// handleCreateAutomationRule（POST /api/v1/automation/rules）
// =============================================================================

// TestHandleCreateAutomationRule_Success 验证正常创建返回 201。
func TestHandleCreateAutomationRule_Success(t *testing.T) {
	s := newAutomationTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":"disk-full-notify","triggerType":"alert","triggerParams":{"alert":"disk_full"},"actions":[{"type":"send_notify","params":{"channel":"email"}}],"enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/automation/rules", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAutomationRules(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var rule store.AutomationRule
	if err := json.Unmarshal(w.Body.Bytes(), &rule); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rule.Name != "disk-full-notify" {
		t.Fatalf("name=%s", rule.Name)
	}
	if rule.ID == "" {
		t.Fatal("ID is empty")
	}
}

// TestHandleCreateAutomationRule_MissingName 验证缺 name 返回 400。
func TestHandleCreateAutomationRule_MissingName(t *testing.T) {
	s := newAutomationTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"triggerType":"alert","actions":[{"type":"send_notify"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/automation/rules", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAutomationRules(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

// TestHandleCreateAutomationRule_InvalidTrigger 验证无效触发器类型返回 400。
func TestHandleCreateAutomationRule_InvalidTrigger(t *testing.T) {
	s := newAutomationTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":"bad-rule","triggerType":"unknown_trigger","actions":[{"type":"send_notify"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/automation/rules", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAutomationRules(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

// TestHandleCreateAutomationRule_NoActions 验证无动作返回 400。
func TestHandleCreateAutomationRule_NoActions(t *testing.T) {
	s := newAutomationTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":"no-action-rule","triggerType":"alert"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/automation/rules", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAutomationRules(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

// =============================================================================
// handleGetAutomationRule（GET /api/v1/automation/rules/{id}）
// =============================================================================

// TestHandleGetAutomationRule_Success 验证正常获取返回 200。
func TestHandleGetAutomationRule_Success(t *testing.T) {
	s := newAutomationTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateAutomationRule("default", &store.AutomationRule{
		Name:        "get-rule",
		TriggerType: "schedule",
		Actions:     []store.AutomationAction{{Type: "execute_task"}},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/automation/rules/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAutomationRuleRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var rule store.AutomationRule
	if err := json.Unmarshal(w.Body.Bytes(), &rule); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rule.Name != "get-rule" {
		t.Fatalf("name=%s", rule.Name)
	}
}

// TestHandleGetAutomationRule_NotFound 验证不存在返回 404。
func TestHandleGetAutomationRule_NotFound(t *testing.T) {
	s := newAutomationTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/automation/rules/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAutomationRuleRouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleUpdateAutomationRule（PUT /api/v1/automation/rules/{id}）
// =============================================================================

// TestHandleUpdateAutomationRule_Success 验证正常更新返回 200。
func TestHandleUpdateAutomationRule_Success(t *testing.T) {
	s := newAutomationTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateAutomationRule("default", &store.AutomationRule{
		Name:        "update-rule",
		TriggerType: "alert",
		Actions:     []store.AutomationAction{{Type: "send_notify"}},
	})

	body := `{"name":"updated-rule","triggerType":"alert","actions":[{"type":"send_notify"}]}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/automation/rules/"+created.ID, strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAutomationRuleRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var rule store.AutomationRule
	if err := json.Unmarshal(w.Body.Bytes(), &rule); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rule.Name != "updated-rule" {
		t.Fatalf("name=%s, want updated-rule", rule.Name)
	}
}

// =============================================================================
// handleDeleteAutomationRule（DELETE /api/v1/automation/rules/{id}）
// =============================================================================

// TestHandleDeleteAutomationRule_Success 验证正常删除返回 200。
func TestHandleDeleteAutomationRule_Success(t *testing.T) {
	s := newAutomationTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateAutomationRule("default", &store.AutomationRule{
		Name:        "delete-rule",
		TriggerType: "alert",
		Actions:     []store.AutomationAction{{Type: "send_notify"}},
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/automation/rules/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAutomationRuleRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
}

// =============================================================================
// handleEnableAutomationRule / handleDisableAutomationRule
// =============================================================================

// TestHandleEnableDisableAutomationRule 验证启用/禁用规则。
func TestHandleEnableDisableAutomationRule(t *testing.T) {
	s := newAutomationTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateAutomationRule("default", &store.AutomationRule{
		Name:        "toggle-rule",
		TriggerType: "alert",
		Actions:     []store.AutomationAction{{Type: "send_notify"}},
		Enabled:     false,
	})

	// 启用
	req := httptest.NewRequest(http.MethodPost, "/api/v1/automation/rules/"+created.ID+"/enable", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAutomationRuleRouting(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("enable status=%d, body=%s", w.Code, w.Body.String())
	}
	var enabled store.AutomationRule
	if err := json.Unmarshal(w.Body.Bytes(), &enabled); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !enabled.Enabled {
		t.Fatal("after enable, Enabled=false")
	}

	// 禁用
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/automation/rules/"+created.ID+"/disable", nil)
	req2.Header.Set("Authorization", auth)
	req2.Header.Set("X-Tenant-ID", "default")
	w2 := httptest.NewRecorder()
	s.handleAutomationRuleRouting(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("disable status=%d, body=%s", w2.Code, w2.Body.String())
	}
	var disabled store.AutomationRule
	if err := json.Unmarshal(w2.Body.Bytes(), &disabled); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if disabled.Enabled {
		t.Fatal("after disable, Enabled=true")
	}
}

// =============================================================================
// handleTestAutomationRule（POST /api/v1/automation/rules/{id}/test）
// =============================================================================

// TestHandleTestAutomationRule_Success 验证测试规则返回 200。
func TestHandleTestAutomationRule_Success(t *testing.T) {
	s := newAutomationTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateAutomationRule("default", &store.AutomationRule{
		Name:        "test-rule",
		TriggerType: "alert",
		Actions:     []store.AutomationAction{{Type: "send_notify"}},
		Enabled:     true,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/automation/rules/"+created.ID+"/test", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAutomationRuleRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
}

// =============================================================================
// handleAutomationExecutions（GET /api/v1/automation/executions）
// =============================================================================

// TestHandleAutomationExecutions_Empty 验证空执行历史返回 200。
func TestHandleAutomationExecutions_Empty(t *testing.T) {
	s := newAutomationTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/automation/executions", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAutomationExecutions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Executions []*store.AutomationExecution `json:"executions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Executions) != 0 {
		t.Fatalf("executions=%d, want 0", len(resp.Executions))
	}
}

// =============================================================================
// 鉴权
// =============================================================================

// TestHandleAutomationRules_NoAuth 验证无 token 返回 401。
func TestHandleAutomationRules_NoAuth(t *testing.T) {
	s := newAutomationTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/automation/rules", nil)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAutomationRules(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", w.Code)
	}
}