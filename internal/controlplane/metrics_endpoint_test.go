// metrics_endpoint_test.go 测试 Prometheus metrics 端点（metrics_endpoint.go）。
//
// 覆盖范围：
//   - handlePrometheusMetrics：空 store、有数据、Prometheus 格式校验
//   - method not allowed
//
// 测试策略（与 ticket_test.go 风格一致）：
//   - 白盒（package controlplane），直接装配 Server{store: MemoryStore}；
//   - /metrics 不需要鉴权，直接调用 handler；
//   - 用 httptest.NewRequest + httptest.NewRecorder 直接调用 handler，断言 status code 与响应体。
package controlplane

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opsmesh/internal/config"
	"opsmesh/internal/store"
)

// newMetricsTestServer 构造 metrics 端点测试用 Server。
func newMetricsTestServer() *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3},
		jwtSecret:    []byte("test-jwt-secret-for-metrics-test-32b!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

// TestHandlePrometheusMetrics_Empty 验证空 store 时 /metrics 返回 200 + Prometheus 格式。
func TestHandlePrometheusMetrics_Empty(t *testing.T) {
	s := newMetricsTestServer()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	s.handlePrometheusMetrics(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	// 校验 Prometheus text exposition format 关键标记。
	required := []string{
		"# HELP opsmesh_devices_total",
		"# TYPE opsmesh_devices_total gauge",
		"opsmesh_devices_total 0",
		"# HELP opsmesh_tasks_total",
		"# TYPE opsmesh_tasks_total gauge",
		"opsmesh_tasks_total 0",
		"# HELP opsmesh_alerts_active",
		"# TYPE opsmesh_alerts_active gauge",
		"opsmesh_alerts_active 0",
		"# HELP opsmesh_tickets_open",
		"# TYPE opsmesh_tickets_open gauge",
		"opsmesh_tickets_open 0",
	}
	for _, want := range required {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q; got:\n%s", want, body)
		}
	}
}

// TestHandlePrometheusMetrics_WithTickets 验证有工单时 opsmesh_tickets_open 反映正确计数。
func TestHandlePrometheusMetrics_WithTickets(t *testing.T) {
	s := newMetricsTestServer()

	// 创建 2 个 open 工单 + 1 个 closed 工单。
	s.store.CreateTicket("default", &store.Ticket{Title: "open-1", Status: "open"})
	s.store.CreateTicket("default", &store.Ticket{Title: "open-2", Status: "open"})
	closed := s.store.CreateTicket("default", &store.Ticket{Title: "closed-1", Status: "open"})
	s.store.CloseTicket("default", closed.ID)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	s.handlePrometheusMetrics(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	// opsmesh_tickets_open 应为 2（仅 open 状态）。
	if !strings.Contains(body, "opsmesh_tickets_open 2") {
		t.Fatalf("metrics body missing 'opsmesh_tickets_open 2'; got:\n%s", body)
	}
}

// TestHandlePrometheusMetrics_MethodNotAllowed 验证非 GET 方法返回 405。
func TestHandlePrometheusMetrics_MethodNotAllowed(t *testing.T) {
	s := newMetricsTestServer()

	req := httptest.NewRequest(http.MethodPost, "/metrics", nil)
	w := httptest.NewRecorder()
	s.handlePrometheusMetrics(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", w.Code)
	}
}

// TestHandlePrometheusMetrics_ContentType 验证 Content-Type 为 Prometheus text format。
func TestHandlePrometheusMetrics_ContentType(t *testing.T) {
	s := newMetricsTestServer()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	s.handlePrometheusMetrics(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type=%q, want text/plain prefix", ct)
	}
}
