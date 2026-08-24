// slo_test.go 测试 Phase 1 SLO 管理 HTTP handler（slo.go）。
//
// 覆盖范围：
//   - handleListSLOs：空列表、创建后列表
//   - handleCreateSLO：正常创建、缺必填字段、无效 JSON
//   - handleGetSLO：正常获取、不存在
//   - handleUpdateSLO：正常更新、不存在
//   - handleDeleteSLO：正常删除、不存在
//   - handleSLOStatus：正常获取 SLI 状态、不存在
//   - handleSLOs：method not allowed 分派
//   - handleSLORouting：{id} 路由分派、空 id、status 子路径
//   - 鉴权：无 token 返回 401
//
// 测试策略（与 ticket_test.go 风格一致）：
//   - 白盒（package controlplane），直接装配 Server{store: MemoryStore, jwtSecret: 固定}；
//   - 鉴权用例通过 admin 登录获取 token（requirePermission 校验 slo:read/write/delete）；
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

// newSLOTestServer 构造 SLO API 测试用 Server：
//   - memory store（NewMemoryStore 已 seedRBAC，预置 admin/admin123）；
//   - 固定 jwtSecret（避免随机性）。
func newSLOTestServer() *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3},
		jwtSecret:    []byte("test-jwt-secret-for-slo-test-32bytes!!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

// =============================================================================
// handleListSLOs（GET /api/v1/slos）
// ============================================================================

// TestHandleListSLOs_Empty 验证空列表返回 200 + slos:[]。
func TestHandleListSLOs_Empty(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/slos", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleSLOs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		SLOs []*store.SLO `json:"slos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.SLOs) != 0 {
		t.Fatalf("slos=%d, want 0", len(resp.SLOs))
	}
}

// TestHandleListSLOs_AfterCreate 验证创建后列表含 1 个 SLO。
func TestHandleListSLOs_AfterCreate(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	s.store.CreateSLO("default", &store.SLO{
		Name:        "list-test",
		ServiceName: "api-server",
		Target:      99.9,
		Window:      "30d",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/slos", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleSLOs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		SLOs []*store.SLO `json:"slos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.SLOs) != 1 {
		t.Fatalf("slos=%d, want 1", len(resp.SLOs))
	}
	if resp.SLOs[0].Name != "list-test" {
		t.Fatalf("Name=%q, want list-test", resp.SLOs[0].Name)
	}
}

// TestHandleListSLOs_NoAuth 验证无 Authorization 头返回 401。
func TestHandleListSLOs_NoAuth(t *testing.T) {
	s := newSLOTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/slos", nil)
	w := httptest.NewRecorder()
	s.handleSLOs(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", w.Code)
	}
}

// =============================================================================
// handleCreateSLO（POST /api/v1/slos）
// ============================================================================

// TestHandleCreateSLO 验证正常创建返回 201 + SLO（含 ID）。
func TestHandleCreateSLO(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":"test-slo","description":"test desc","serviceName":"api","target":99.9,"window":"30d","slis":[{"name":"availability","metric":"up","target":99.9,"operator":">="}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/slos", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleSLOs(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", w.Code, w.Body.String())
	}
	var slo store.SLO
	if err := json.Unmarshal(w.Body.Bytes(), &slo); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if slo.ID == "" {
		t.Fatal("ID is empty, want server-assigned")
	}
	if slo.Name != "test-slo" {
		t.Fatalf("Name=%q, want test-slo", slo.Name)
	}
	if slo.Target != 99.9 {
		t.Fatalf("Target=%v, want 99.9", slo.Target)
	}
	if len(slo.SLIs) != 1 {
		t.Fatalf("SLIs=%d, want 1", len(slo.SLIs))
	}
	if slo.SLIs[0].Name != "availability" {
		t.Fatalf("SLIs[0].Name=%q, want availability", slo.SLIs[0].Name)
	}
	// 确认 SLO 已持久化到 store
	got, ok := s.store.GetSLO("default", slo.ID)
	if !ok || got == nil {
		t.Fatal("GetSLO returned nil after create")
	}
}

// TestHandleCreateSLO_MissingName 验证缺 name 返回 400。
func TestHandleCreateSLO_MissingName(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"description":"no name"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/slos", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleSLOs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// =============================================================================
// handleGetSLO（GET /api/v1/slos/{id}）
// ============================================================================

// TestHandleGetSLO 验证正常获取 SLO 详情。
func TestHandleGetSLO(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateSLO("default", &store.SLO{Name: "get-test", Target: 99.5})
	if created == nil {
		t.Fatal("CreateSLO returned nil")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/slos/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleSLORouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var slo store.SLO
	if err := json.Unmarshal(w.Body.Bytes(), &slo); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if slo.ID != created.ID {
		t.Fatalf("ID=%q, want %q", slo.ID, created.ID)
	}
	if slo.Name != "get-test" {
		t.Fatalf("Name=%q, want get-test", slo.Name)
	}
}

// TestHandleGetSLO_NotFound 验证获取不存在的 SLO 返回 404。
func TestHandleGetSLO_NotFound(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/slos/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleSLORouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleUpdateSLO（PUT /api/v1/slos/{id}）
// ============================================================================

// TestHandleUpdateSLO 验证正常更新 SLO。
func TestHandleUpdateSLO(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateSLO("default", &store.SLO{Name: "update-test", Target: 99.0})
	if created == nil {
		t.Fatal("CreateSLO returned nil")
	}

	body := `{"name":"updated-name","target":99.99,"window":"7d"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/slos/"+created.ID, strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleSLORouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var slo store.SLO
	if err := json.Unmarshal(w.Body.Bytes(), &slo); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if slo.Name != "updated-name" {
		t.Fatalf("Name=%q, want updated-name", slo.Name)
	}
	if slo.Target != 99.99 {
		t.Fatalf("Target=%v, want 99.99", slo.Target)
	}
	if slo.Window != "7d" {
		t.Fatalf("Window=%q, want 7d", slo.Window)
	}
}

// =============================================================================
// handleDeleteSLO（DELETE /api/v1/slos/{id}）
// ============================================================================

// TestHandleDeleteSLO 验证正常删除 SLO 返回 204。
func TestHandleDeleteSLO(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateSLO("default", &store.SLO{Name: "delete-test"})
	if created == nil {
		t.Fatal("CreateSLO returned nil")
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/slos/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleSLORouting(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204; body=%s", w.Code, w.Body.String())
	}
	// 确认已删除
	if _, ok := s.store.GetSLO("default", created.ID); ok {
		t.Fatal("SLO still exists after delete")
	}
}

// TestHandleDeleteSLO_NotFound 验证删除不存在的 SLO 返回 404。
func TestHandleDeleteSLO_NotFound(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/slos/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleSLORouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleSLOStatus（GET /api/v1/slos/{id}/status）
// ============================================================================

// TestHandleSLOStatus 验证正常获取 SLI 状态。
func TestHandleSLOStatus(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateSLO("default", &store.SLO{
		Name:   "status-test",
		Target: 99.9,
		SLIs: []store.SLI{
			{Name: "availability", Metric: "up", Target: 99.9, Operator: ">="},
			{Name: "latency_p99", Metric: "latency", Target: 100, Operator: "<="},
		},
	})
	if created == nil {
		t.Fatal("CreateSLO returned nil")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/slos/"+created.ID+"/status", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleSLORouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Statuses []*store.SLIStatus `json:"statuses"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Statuses) != 2 {
		t.Fatalf("statuses=%d, want 2", len(resp.Statuses))
	}
	// MVP 模拟值：CurrentValue=99.5, Status="met"
	if resp.Statuses[0].CurrentValue != 99.5 {
		t.Fatalf("CurrentValue=%v, want 99.5 (MVP)", resp.Statuses[0].CurrentValue)
	}
	if resp.Statuses[0].Status != "met" {
		t.Fatalf("Status=%q, want met (MVP)", resp.Statuses[0].Status)
	}
}

// TestHandleSLOStatus_NotFound 验证获取不存在 SLO 的状态返回 404。
func TestHandleSLOStatus_NotFound(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/slos/nonexistent/status", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleSLORouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleSLORouting 路由分派
// ============================================================================

// TestHandleSLORouting_EmptyID 验证空 id 返回 400。
func TestHandleSLORouting_EmptyID(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/slos/", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleSLORouting(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

// TestHandleSLORouting_UnknownSubPath 验证未知子路径返回 404。
func TestHandleSLORouting_UnknownSubPath(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/slos/some-id/unknown", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleSLORouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// TestHandleSLOs_MethodNotAllowed 验证不支持的方法返回 405。
func TestHandleSLOs_MethodNotAllowed(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/slos", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleSLOs(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", w.Code)
	}
}