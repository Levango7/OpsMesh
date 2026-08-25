// traffic_test.go 测试 Phase 2 流量治理 HTTP handler（traffic.go）。
//
// 覆盖范围：
//   - handleListTrafficPolicies：空列表、创建后列表
//   - handleCreateTrafficPolicy：正常创建、缺必填字段、无效 JSON
//   - handleGetTrafficPolicy：正常获取、不存在
//   - handleUpdateTrafficPolicy：正常更新、不存在
//   - handleDeleteTrafficPolicy：正常删除、不存在
//   - handleEnableTrafficPolicy / handleDisableTrafficPolicy
//   - 鉴权：无 token 返回 401
//
// 测试策略（与 ticket_test.go 风格一致）：
//   - 白盒（package controlplane），直接装配 Server{store: MemoryStore}；
//   - 鉴权用例通过 admin 登录获取 token（requirePermission 校验 traffic:read/write）。
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

// newTrafficTestServer 构造流量治理 API 测试用 Server。
func newTrafficTestServer() *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3},
		jwtSecret:    []byte("test-jwt-secret-for-traffic-test-32bytes!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

// =============================================================================
// handleListTrafficPolicies（GET /api/v1/traffic/policies）
// =============================================================================

// TestHandleListTrafficPolicies_Empty 验证空列表返回 200 + policies:[]。
func TestHandleListTrafficPolicies_Empty(t *testing.T) {
	s := newTrafficTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/traffic/policies", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTrafficPolicies(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Policies []*store.TrafficPolicy `json:"policies"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Policies) != 0 {
		t.Fatalf("policies=%d, want 0", len(resp.Policies))
	}
}

// TestHandleListTrafficPolicies_AfterCreate 验证创建后列表含 1 个策略。
func TestHandleListTrafficPolicies_AfterCreate(t *testing.T) {
	s := newTrafficTestServer()
	auth := loginAsAdmin(t, s)

	s.store.CreatePolicy("default", &store.TrafficPolicy{
		Name:        "canary-test",
		ServiceName: "my-service",
		Type:        "canary",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/traffic/policies", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTrafficPolicies(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Policies []*store.TrafficPolicy `json:"policies"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Policies) != 1 {
		t.Fatalf("policies=%d, want 1", len(resp.Policies))
	}
	if resp.Policies[0].Name != "canary-test" {
		t.Fatalf("Name=%q, want canary-test", resp.Policies[0].Name)
	}
}

// TestHandleListTrafficPolicies_NoAuth 验证无 Authorization 头返回 401。
func TestHandleListTrafficPolicies_NoAuth(t *testing.T) {
	s := newTrafficTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/traffic/policies", nil)
	w := httptest.NewRecorder()
	s.handleTrafficPolicies(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", w.Code)
	}
}

// =============================================================================
// handleCreateTrafficPolicy（POST /api/v1/traffic/policies）
// =============================================================================

// TestHandleCreateTrafficPolicy 验证正常创建返回 201 + 策略（含 ID）。
func TestHandleCreateTrafficPolicy(t *testing.T) {
	s := newTrafficTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":"retry-policy","serviceName":"api-svc","type":"retry","retries":3,"retryTimeout":"5s"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/traffic/policies", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleTrafficPolicies(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", w.Code, w.Body.String())
	}
	var p store.TrafficPolicy
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.ID == "" {
		t.Fatal("ID is empty, want server-assigned")
	}
	if p.Name != "retry-policy" {
		t.Fatalf("Name=%q, want retry-policy", p.Name)
	}
	if p.Retries != 3 {
		t.Fatalf("Retries=%d, want 3", p.Retries)
	}
}

// TestHandleCreateTrafficPolicy_MissingName 验证缺 name 返回 400。
func TestHandleCreateTrafficPolicy_MissingName(t *testing.T) {
	s := newTrafficTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"serviceName":"svc","type":"retry"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/traffic/policies", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleTrafficPolicies(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreateTrafficPolicy_InvalidJSON 验证无效 JSON 返回 400。
func TestHandleCreateTrafficPolicy_InvalidJSON(t *testing.T) {
	s := newTrafficTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":invalid`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/traffic/policies", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleTrafficPolicies(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// =============================================================================
// handleGetTrafficPolicy（GET /api/v1/traffic/policies/{id}）
// =============================================================================

// TestHandleGetTrafficPolicy 验证正常获取策略详情。
func TestHandleGetTrafficPolicy(t *testing.T) {
	s := newTrafficTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreatePolicy("default", &store.TrafficPolicy{Name: "get-test", Type: "timeout"})
	if created == nil {
		t.Fatal("CreatePolicy returned nil")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/traffic/policies/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTrafficPolicyRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var p store.TrafficPolicy
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.ID != created.ID {
		t.Fatalf("ID=%q, want %q", p.ID, created.ID)
	}
	if p.Name != "get-test" {
		t.Fatalf("Name=%q, want get-test", p.Name)
	}
}

// TestHandleGetTrafficPolicy_NotFound 验证获取不存在的策略返回 404。
func TestHandleGetTrafficPolicy_NotFound(t *testing.T) {
	s := newTrafficTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/traffic/policies/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTrafficPolicyRouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleUpdateTrafficPolicy（PUT /api/v1/traffic/policies/{id}）
// =============================================================================

// TestHandleUpdateTrafficPolicy 验证正常更新策略。
func TestHandleUpdateTrafficPolicy(t *testing.T) {
	s := newTrafficTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreatePolicy("default", &store.TrafficPolicy{Name: "update-test", Type: "retry", Retries: 1})
	if created == nil {
		t.Fatal("CreatePolicy returned nil")
	}

	body := `{"name":"updated-name","type":"retry","retries":5,"retryTimeout":"10s"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/traffic/policies/"+created.ID, strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleTrafficPolicyRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var p store.TrafficPolicy
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Name != "updated-name" {
		t.Fatalf("Name=%q, want updated-name", p.Name)
	}
	if p.Retries != 5 {
		t.Fatalf("Retries=%d, want 5", p.Retries)
	}
}

// TestHandleUpdateTrafficPolicy_NotFound 验证更新不存在的策略返回 404。
func TestHandleUpdateTrafficPolicy_NotFound(t *testing.T) {
	s := newTrafficTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":"updated-name"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/traffic/policies/nonexistent", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleTrafficPolicyRouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleDeleteTrafficPolicy（DELETE /api/v1/traffic/policies/{id}）
// =============================================================================

// TestHandleDeleteTrafficPolicy 验证正常删除策略。
func TestHandleDeleteTrafficPolicy(t *testing.T) {
	s := newTrafficTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreatePolicy("default", &store.TrafficPolicy{Name: "delete-test"})
	if created == nil {
		t.Fatal("CreatePolicy returned nil")
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/traffic/policies/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTrafficPolicyRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	// 确认已删除
	if _, ok := s.store.GetPolicy("default", created.ID); ok {
		t.Fatal("policy still exists after delete")
	}
}

// TestHandleDeleteTrafficPolicy_NotFound 验证删除不存在的策略返回 404。
func TestHandleDeleteTrafficPolicy_NotFound(t *testing.T) {
	s := newTrafficTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/traffic/policies/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTrafficPolicyRouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleEnableTrafficPolicy / handleDisableTrafficPolicy
// =============================================================================

// TestHandleEnableTrafficPolicy 验证启用策略。
func TestHandleEnableTrafficPolicy(t *testing.T) {
	s := newTrafficTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreatePolicy("default", &store.TrafficPolicy{Name: "enable-test", Status: "inactive"})
	if created == nil {
		t.Fatal("CreatePolicy returned nil")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/traffic/policies/"+created.ID+"/enable", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTrafficPolicyRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var p store.TrafficPolicy
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Status != "active" {
		t.Fatalf("Status=%q, want active", p.Status)
	}
}

// TestHandleDisableTrafficPolicy 验证禁用策略。
func TestHandleDisableTrafficPolicy(t *testing.T) {
	s := newTrafficTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreatePolicy("default", &store.TrafficPolicy{Name: "disable-test", Status: "active"})
	if created == nil {
		t.Fatal("CreatePolicy returned nil")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/traffic/policies/"+created.ID+"/disable", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTrafficPolicyRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var p store.TrafficPolicy
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Status != "inactive" {
		t.Fatalf("Status=%q, want inactive", p.Status)
	}
}

// =============================================================================
// handleTrafficPolicies method not allowed
// =============================================================================

// TestHandleTrafficPolicies_MethodNotAllowed 验证不支持的方法返回 405。
func TestHandleTrafficPolicies_MethodNotAllowed(t *testing.T) {
	s := newTrafficTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/traffic/policies", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTrafficPolicies(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", w.Code)
	}
}

// TestHandleTrafficPolicyRouting_EmptyID 验证空 id 返回 400。
func TestHandleTrafficPolicyRouting_EmptyID(t *testing.T) {
	s := newTrafficTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/traffic/policies/", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTrafficPolicyRouting(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

// TestHandleTrafficPolicyRouting_UnknownSubPath 验证未知子路径返回 404。
func TestHandleTrafficPolicyRouting_UnknownSubPath(t *testing.T) {
	s := newTrafficTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/traffic/policies/some-id/unknown", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTrafficPolicyRouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}
