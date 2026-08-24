// ticket_test.go 测试 Phase 1 工单管理 HTTP handler（ticket.go）。
//
// 覆盖范围：
//   - handleListTickets：空列表、创建后列表、过滤参数
//   - handleCreateTicket：正常创建、缺必填字段、无效 JSON
//   - handleGetTicket：正常获取、不存在
//   - handleUpdateTicket：正常更新、不存在
//   - handleCloseTicket：正常关闭、不存在
//   - handleTickets：method not allowed 分派
//   - handleTicketRouting：{id} 路由分派、空 id、close 子路径
//   - 鉴权：无 token 返回 401
//
// 测试策略（与 k8s_cluster_test.go 风格一致）：
//   - 白盒（package controlplane），直接装配 Server{store: MemoryStore, jwtSecret: 固定}；
//   - 鉴权用例通过 admin 登录获取 token（requirePermission 校验 ticket:read/write）；
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

// newTicketTestServer 构造工单 API 测试用 Server：
//   - memory store（NewMemoryStore 已 seedRBAC，预置 admin/admin123）；
//   - 固定 jwtSecret（避免随机性）。
func newTicketTestServer() *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3},
		jwtSecret:    []byte("test-jwt-secret-for-ticket-test-32bytes!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

// =============================================================================
// handleListTickets（GET /api/v1/tickets）
// ============================================================================

// TestHandleListTickets_Empty 验证空列表返回 200 + tickets:[]。
func TestHandleListTickets_Empty(t *testing.T) {
	s := newTicketTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tickets", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTickets(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Tickets []*store.Ticket `json:"tickets"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tickets) != 0 {
		t.Fatalf("tickets=%d, want 0", len(resp.Tickets))
	}
}

// TestHandleListTickets_AfterCreate 验证创建后列表含 1 个工单。
func TestHandleListTickets_AfterCreate(t *testing.T) {
	s := newTicketTestServer()
	auth := loginAsAdmin(t, s)

	// 先直接写一条工单到 store（绕过 handler）
	s.store.CreateTicket("default", &store.Ticket{
		Title:    "list-test",
		Priority: "high",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tickets", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTickets(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Tickets []*store.Ticket `json:"tickets"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tickets) != 1 {
		t.Fatalf("tickets=%d, want 1", len(resp.Tickets))
	}
	if resp.Tickets[0].Title != "list-test" {
		t.Fatalf("Title=%q, want list-test", resp.Tickets[0].Title)
	}
}

// TestHandleListTickets_Filter 验证过滤参数生效。
func TestHandleListTickets_Filter(t *testing.T) {
	s := newTicketTestServer()
	auth := loginAsAdmin(t, s)

	// 写两条工单，一条 high，一条 low。
	s.store.CreateTicket("default", &store.Ticket{Title: "high-ticket", Priority: "high"})
	s.store.CreateTicket("default", &store.Ticket{Title: "low-ticket", Priority: "low"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tickets?priority=high", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTickets(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Tickets []*store.Ticket `json:"tickets"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tickets) != 1 {
		t.Fatalf("tickets=%d, want 1 (filtered by priority=high)", len(resp.Tickets))
	}
	if resp.Tickets[0].Title != "high-ticket" {
		t.Fatalf("Title=%q, want high-ticket", resp.Tickets[0].Title)
	}
}

// TestHandleListTickets_NoAuth 验证无 Authorization 头返回 401。
func TestHandleListTickets_NoAuth(t *testing.T) {
	s := newTicketTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tickets", nil)
	w := httptest.NewRecorder()
	s.handleTickets(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", w.Code)
	}
}

// =============================================================================
// handleCreateTicket（POST /api/v1/tickets）
// ============================================================================

// TestHandleCreateTicket 验证正常创建返回 201 + 工单（含 ID）。
func TestHandleCreateTicket(t *testing.T) {
	s := newTicketTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"title":"test-ticket","description":"test desc","priority":"high","category":"incident"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tickets", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleTickets(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", w.Code, w.Body.String())
	}
	var tk store.Ticket
	if err := json.Unmarshal(w.Body.Bytes(), &tk); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tk.ID == "" {
		t.Fatal("ID is empty, want server-assigned")
	}
	if tk.Title != "test-ticket" {
		t.Fatalf("Title=%q, want test-ticket", tk.Title)
	}
	if tk.Priority != "high" {
		t.Fatalf("Priority=%q, want high", tk.Priority)
	}
	if tk.Status != "open" {
		t.Fatalf("Status=%q, want open (default)", tk.Status)
	}
	// 确认工单已持久化到 store
	got, ok := s.store.GetTicket("default", tk.ID)
	if !ok || got == nil {
		t.Fatal("GetTicket returned nil after create")
	}
}

// TestHandleCreateTicket_MissingTitle 验证缺 title 返回 400。
func TestHandleCreateTicket_MissingTitle(t *testing.T) {
	s := newTicketTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"description":"no title"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tickets", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleTickets(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreateTicket_InvalidJSON 验证无效 JSON 返回 400。
func TestHandleCreateTicket_InvalidJSON(t *testing.T) {
	s := newTicketTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"title":invalid`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tickets", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleTickets(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// =============================================================================
// handleGetTicket（GET /api/v1/tickets/{id}）
// ============================================================================

// TestHandleGetTicket 验证正常获取工单详情。
func TestHandleGetTicket(t *testing.T) {
	s := newTicketTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateTicket("default", &store.Ticket{Title: "get-test"})
	if created == nil {
		t.Fatal("CreateTicket returned nil")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tickets/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTicketRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var tk store.Ticket
	if err := json.Unmarshal(w.Body.Bytes(), &tk); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tk.ID != created.ID {
		t.Fatalf("ID=%q, want %q", tk.ID, created.ID)
	}
	if tk.Title != "get-test" {
		t.Fatalf("Title=%q, want get-test", tk.Title)
	}
}

// TestHandleGetTicket_NotFound 验证获取不存在的工单返回 404。
func TestHandleGetTicket_NotFound(t *testing.T) {
	s := newTicketTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tickets/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTicketRouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleUpdateTicket（PUT /api/v1/tickets/{id}）
// ============================================================================

// TestHandleUpdateTicket 验证正常更新工单。
func TestHandleUpdateTicket(t *testing.T) {
	s := newTicketTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateTicket("default", &store.Ticket{Title: "update-test", Priority: "low"})
	if created == nil {
		t.Fatal("CreateTicket returned nil")
	}

	body := `{"title":"updated-title","priority":"high","status":"in_progress"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/tickets/"+created.ID, strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleTicketRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var tk store.Ticket
	if err := json.Unmarshal(w.Body.Bytes(), &tk); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tk.Title != "updated-title" {
		t.Fatalf("Title=%q, want updated-title", tk.Title)
	}
	if tk.Priority != "high" {
		t.Fatalf("Priority=%q, want high", tk.Priority)
	}
	if tk.Status != "in_progress" {
		t.Fatalf("Status=%q, want in_progress", tk.Status)
	}
}

// TestHandleUpdateTicket_NotFound 验证更新不存在的工单返回 404。
func TestHandleUpdateTicket_NotFound(t *testing.T) {
	s := newTicketTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"title":"updated-title"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/tickets/nonexistent", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleTicketRouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleCloseTicket（POST /api/v1/tickets/{id}/close）
// ============================================================================

// TestHandleCloseTicket 验证正常关闭工单。
func TestHandleCloseTicket(t *testing.T) {
	s := newTicketTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateTicket("default", &store.Ticket{Title: "close-test"})
	if created == nil {
		t.Fatal("CreateTicket returned nil")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tickets/"+created.ID+"/close", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTicketRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var tk store.Ticket
	if err := json.Unmarshal(w.Body.Bytes(), &tk); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tk.Status != "closed" {
		t.Fatalf("Status=%q, want closed", tk.Status)
	}
	if tk.ResolvedAt == nil {
		t.Fatal("ResolvedAt is nil, want set")
	}
}

// TestHandleCloseTicket_NotFound 验证关闭不存在的工单返回 404。
func TestHandleCloseTicket_NotFound(t *testing.T) {
	s := newTicketTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tickets/nonexistent/close", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTicketRouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleTicketRouting 路由分派
// ============================================================================

// TestHandleTicketRouting_EmptyID 验证空 id 返回 400。
func TestHandleTicketRouting_EmptyID(t *testing.T) {
	s := newTicketTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tickets/", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTicketRouting(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

// TestHandleTicketRouting_UnknownSubPath 验证未知子路径返回 404。
func TestHandleTicketRouting_UnknownSubPath(t *testing.T) {
	s := newTicketTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tickets/some-id/unknown", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTicketRouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// TestHandleTickets_MethodNotAllowed 验证不支持的方法返回 405。
func TestHandleTickets_MethodNotAllowed(t *testing.T) {
	s := newTicketTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tickets", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTickets(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", w.Code)
	}
}