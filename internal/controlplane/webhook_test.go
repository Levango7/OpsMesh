// webhook_test.go 测试 Phase 5 Webhook 管理 HTTP handler（webhook.go）。
//
// 覆盖范围：
//   - handleListWebhooks：空列表、创建后列表
//   - handleCreateWebhook：正常创建、缺必填字段、无效 JSON
//   - handleGetWebhook：正常获取、不存在
//   - handleUpdateWebhook：正常更新、不存在
//   - handleDeleteWebhook：正常删除、不存在
//   - handleWebhookTest：正常测试投递、不存在
//   - handleWebhookDeliveries：投递记录列表
//   - handleWebhooks：method not allowed 分派
//   - handleWebhookRouting：{id} 路由分派、空 id、未知子路径
//   - 鉴权：无 token 返回 401
//
// 测试策略（与 ticket_test.go 风格一致）：
//   - 白盒（package controlplane），直接装配 Server{store: MemoryStore, jwtSecret: 固定}；
//   - 鉴权用例通过 admin 登录获取 token（requirePermission 校验 webhook:read/write）；
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

// newWebhookTestServer 构造 Webhook API 测试用 Server。
func newWebhookTestServer() *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3},
		jwtSecret:    []byte("test-jwt-secret-for-webhook-test-32bytes!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

// =============================================================================
// handleListWebhooks（GET /api/v1/webhooks）
// ============================================================================

// TestHandleListWebhooks_Empty 验证空列表返回 200 + webhooks:[]。
func TestHandleListWebhooks_Empty(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleWebhooks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Webhooks []*store.Webhook `json:"webhooks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Webhooks) != 0 {
		t.Fatalf("webhooks=%d, want 0", len(resp.Webhooks))
	}
}

// TestHandleListWebhooks_AfterCreate 验证创建后列表含 1 个 Webhook。
func TestHandleListWebhooks_AfterCreate(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	s.store.CreateWebhook("default", &store.Webhook{
		Name: "list-test",
		URL:  "http://example.com/hook",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleWebhooks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Webhooks []*store.Webhook `json:"webhooks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Webhooks) != 1 {
		t.Fatalf("webhooks=%d, want 1", len(resp.Webhooks))
	}
	if resp.Webhooks[0].Name != "list-test" {
		t.Fatalf("Name=%q, want list-test", resp.Webhooks[0].Name)
	}
}

// TestHandleListWebhooks_NoAuth 验证无 Authorization 头返回 401。
func TestHandleListWebhooks_NoAuth(t *testing.T) {
	s := newWebhookTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks", nil)
	w := httptest.NewRecorder()
	s.handleWebhooks(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", w.Code)
	}
}

// =============================================================================
// handleCreateWebhook（POST /api/v1/webhooks）
// ============================================================================

// TestHandleCreateWebhook 验证正常创建返回 201 + Webhook（含 ID）。
func TestHandleCreateWebhook(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":"test-webhook","url":"http://example.com/hook","events":["alert.created"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleWebhooks(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", w.Code, w.Body.String())
	}
	var wh store.Webhook
	if err := json.Unmarshal(w.Body.Bytes(), &wh); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if wh.ID == "" {
		t.Fatal("ID is empty, want server-assigned")
	}
	if wh.Name != "test-webhook" {
		t.Fatalf("Name=%q, want test-webhook", wh.Name)
	}
	// 确认已持久化到 store
	got, ok := s.store.GetWebhook("default", wh.ID)
	if !ok || got == nil {
		t.Fatal("GetWebhook returned nil after create")
	}
}

// TestHandleCreateWebhook_MissingName 验证缺 name 返回 400。
func TestHandleCreateWebhook_MissingName(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"url":"http://example.com/hook"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleWebhooks(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreateWebhook_MissingURL 验证缺 url 返回 400。
func TestHandleCreateWebhook_MissingURL(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":"test-webhook"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleWebhooks(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreateWebhook_InvalidJSON 验证无效 JSON 返回 400。
func TestHandleCreateWebhook_InvalidJSON(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":invalid`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleWebhooks(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// =============================================================================
// handleGetWebhook（GET /api/v1/webhooks/{id}）
// ============================================================================

// TestHandleGetWebhook 验证正常获取 Webhook 详情。
func TestHandleGetWebhook(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateWebhook("default", &store.Webhook{Name: "get-test", URL: "http://example.com/hook"})
	if created == nil {
		t.Fatal("CreateWebhook returned nil")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleWebhookRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var wh store.Webhook
	if err := json.Unmarshal(w.Body.Bytes(), &wh); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if wh.ID != created.ID {
		t.Fatalf("ID=%q, want %q", wh.ID, created.ID)
	}
}

// TestHandleGetWebhook_NotFound 验证获取不存在的 Webhook 返回 404。
func TestHandleGetWebhook_NotFound(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleWebhookRouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleUpdateWebhook（PUT /api/v1/webhooks/{id}）
// ============================================================================

// TestHandleUpdateWebhook 验证正常更新 Webhook。
func TestHandleUpdateWebhook(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateWebhook("default", &store.Webhook{Name: "update-test", URL: "http://example.com/hook"})
	if created == nil {
		t.Fatal("CreateWebhook returned nil")
	}

	body := `{"name":"updated-name","url":"http://example.com/hook2"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/webhooks/"+created.ID, strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleWebhookRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var wh store.Webhook
	if err := json.Unmarshal(w.Body.Bytes(), &wh); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if wh.Name != "updated-name" {
		t.Fatalf("Name=%q, want updated-name", wh.Name)
	}
	if wh.URL != "http://example.com/hook2" {
		t.Fatalf("URL=%q, want http://example.com/hook2", wh.URL)
	}
}

// TestHandleUpdateWebhook_NotFound 验证更新不存在的 Webhook 返回 404。
func TestHandleUpdateWebhook_NotFound(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":"updated-name","url":"http://example.com/hook"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/webhooks/nonexistent", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleWebhookRouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleDeleteWebhook（DELETE /api/v1/webhooks/{id}）
// ============================================================================

// TestHandleDeleteWebhook 验证正常删除 Webhook。
func TestHandleDeleteWebhook(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateWebhook("default", &store.Webhook{Name: "delete-test", URL: "http://example.com/hook"})
	if created == nil {
		t.Fatal("CreateWebhook returned nil")
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/webhooks/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleWebhookRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	// 确认已删除
	if _, ok := s.store.GetWebhook("default", created.ID); ok {
		t.Fatal("GetWebhook returned ok after delete")
	}
}

// TestHandleDeleteWebhook_NotFound 验证删除不存在的 Webhook 返回 404。
func TestHandleDeleteWebhook_NotFound(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/webhooks/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleWebhookRouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleWebhookTest（POST /api/v1/webhooks/{id}/test）
// ============================================================================

// TestHandleWebhookTest 验证正常测试投递返回 200 + 投递记录。
func TestHandleWebhookTest(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateWebhook("default", &store.Webhook{Name: "test-hook", URL: "http://example.com/hook"})
	if created == nil {
		t.Fatal("CreateWebhook returned nil")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/"+created.ID+"/test", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleWebhookRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var d store.WebhookDelivery
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d.WebhookID != created.ID {
		t.Fatalf("WebhookID=%q, want %q", d.WebhookID, created.ID)
	}
	if d.StatusCode != 200 {
		t.Fatalf("StatusCode=%d, want 200", d.StatusCode)
	}
}

// TestHandleWebhookTest_NotFound 验证测试不存在的 Webhook 返回 404。
func TestHandleWebhookTest_NotFound(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/nonexistent/test", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleWebhookRouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleWebhookDeliveries（GET /api/v1/webhooks/{id}/deliveries）
// ============================================================================

// TestHandleWebhookDeliveries 验证投递记录列表。
func TestHandleWebhookDeliveries(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateWebhook("default", &store.Webhook{Name: "del-test", URL: "http://example.com/hook"})
	if created == nil {
		t.Fatal("CreateWebhook returned nil")
	}
	// 先记录一条投递
	ms := s.store.(*store.MemoryStore)
	ms.RecordWebhookDelivery("default", created.ID, "test.event", "{}", 200, "ok", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/"+created.ID+"/deliveries", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleWebhookRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Deliveries []*store.WebhookDelivery `json:"deliveries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Deliveries) != 1 {
		t.Fatalf("deliveries=%d, want 1", len(resp.Deliveries))
	}
}

// TestHandleWebhookDeliveries_Empty 验证空投递记录返回 200 + deliveries:[]。
func TestHandleWebhookDeliveries_Empty(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateWebhook("default", &store.Webhook{Name: "del-empty", URL: "http://example.com/hook"})
	if created == nil {
		t.Fatal("CreateWebhook returned nil")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/"+created.ID+"/deliveries", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleWebhookRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Deliveries []*store.WebhookDelivery `json:"deliveries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Deliveries) != 0 {
		t.Fatalf("deliveries=%d, want 0", len(resp.Deliveries))
	}
}

// =============================================================================
// handleWebhookRouting 路由分派
// ============================================================================

// TestHandleWebhookRouting_EmptyID 验证空 id 返回 400。
func TestHandleWebhookRouting_EmptyID(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleWebhookRouting(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

// TestHandleWebhookRouting_UnknownSubPath 验证未知子路径返回 404。
func TestHandleWebhookRouting_UnknownSubPath(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/some-id/unknown", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleWebhookRouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// TestHandleWebhooks_MethodNotAllowed 验证不支持的方法返回 405。
func TestHandleWebhooks_MethodNotAllowed(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/webhooks", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleWebhooks(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", w.Code)
	}
}