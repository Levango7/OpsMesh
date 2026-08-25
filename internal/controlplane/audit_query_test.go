// audit_query_test.go 测试 Phase 3 审计日志查询 HTTP handler（audit_query.go）。
//
// 覆盖范围：
//   - handleAuditEvents：空列表、有事件、user 过滤、limit
//   - handleAuditExport：导出 JSON 数组
//   - 鉴权：无 token 返回 401
//
// 测试策略（与 ticket_test.go 风格一致）。
// 注意：loginAsAdmin 会产生审计事件（login action），测试用独立 action 过滤隔离。
package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"opsmesh/internal/config"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// newAuditQueryTestServer 构造审计查询 API 测试用 Server。
func newAuditQueryTestServer() *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3},
		jwtSecret:    []byte("test-jwt-secret-for-audit-query-test-32!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

// TestHandleAuditEvents_Empty 验证空列表返回 200 + events:[]。
// 用不存在的 action 过滤，避免 loginAsAdmin 产生的审计事件干扰。
func TestHandleAuditEvents_Empty(t *testing.T) {
	s := newAuditQueryTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit/events?action=nonexistent_empty_test", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleAuditEvents(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Events []*proto.AuditEvent `json:"events"`
		Count  int                 `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Count != 0 {
		t.Fatalf("count=%d, want 0", resp.Count)
	}
}

// TestHandleAuditEvents_WithEvents 验证有事件时返回事件列表。
// 用独立 action 过滤，避免 loginAsAdmin 产生的审计事件干扰。
func TestHandleAuditEvents_WithEvents(t *testing.T) {
	s := newAuditQueryTestServer()
	auth := loginAsAdmin(t, s)

	// 直接写审计事件到 store（用独立 action）。
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default",
		UserID:   "admin",
		Action:   "test_with_events",
		Target:   "dev-1",
	})
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default",
		UserID:   "operator",
		Action:   "test_with_events",
		Target:   "dev-2",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit/events?action=test_with_events", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleAuditEvents(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Events []*proto.AuditEvent `json:"events"`
		Count  int                 `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Count != 2 {
		t.Fatalf("count=%d, want 2", resp.Count)
	}
}

// TestHandleAuditEvents_UserFilter 验证 user 查询参数过滤。
// 用独立 action 过滤，避免 loginAsAdmin 产生的审计事件干扰。
func TestHandleAuditEvents_UserFilter(t *testing.T) {
	s := newAuditQueryTestServer()
	auth := loginAsAdmin(t, s)

	s.store.Audit(&proto.AuditEvent{
		TenantID: "default",
		UserID:   "admin",
		Action:   "test_user_filter",
	})
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default",
		UserID:   "operator",
		Action:   "test_user_filter",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit/events?action=test_user_filter&user=admin", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleAuditEvents(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Count != 1 {
		t.Fatalf("count=%d, want 1 (filtered by user=admin)", resp.Count)
	}
}

// TestHandleAuditExport 验证导出返回 JSON 数组。
// 用独立 action 过滤，避免 loginAsAdmin 产生的审计事件干扰。
func TestHandleAuditExport(t *testing.T) {
	s := newAuditQueryTestServer()
	auth := loginAsAdmin(t, s)

	s.store.Audit(&proto.AuditEvent{
		TenantID: "default",
		UserID:   "admin",
		Action:   "test_export",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit/export?action=test_export", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleAuditExport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var events []*proto.AuditEvent
	if err := json.Unmarshal(w.Body.Bytes(), &events); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events=%d, want 1", len(events))
	}
}

// TestHandleAuditEvents_NoToken 验证无 token 返回 401。
func TestHandleAuditEvents_NoToken(t *testing.T) {
	s := newAuditQueryTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit/events", nil)
	w := httptest.NewRecorder()
	s.handleAuditEvents(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", w.Code)
	}
}
