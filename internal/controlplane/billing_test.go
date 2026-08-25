// billing_test.go 测试 Phase 6 计费 HTTP handler（billing.go）。
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

// newBillingTestServer 构造计费 API 测试用 Server。
func newBillingTestServer() *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3},
		jwtSecret:    []byte("test-jwt-secret-for-billing-test-32bytes!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

// ============================================================================
// 订阅计划
// ============================================================================

// TestHandleListBillingPlans_Empty 验证空列表返回 200。
func TestHandleListBillingPlans_Empty(t *testing.T) {
	s := newBillingTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing/plans", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleBillingPlans(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreateBillingPlan 验证正常创建返回 201。
func TestHandleCreateBillingPlan(t *testing.T) {
	s := newBillingTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"name":"basic","price":9900,"interval":"monthly","features":["10 devices"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/plans", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleBillingPlans(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", w.Code, w.Body.String())
	}
	var plan store.SubscriptionPlan
	if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if plan.ID == "" {
		t.Fatal("ID is empty")
	}
}

// TestHandleCreateBillingPlan_MissingName 验证缺 name 返回 400。
func TestHandleCreateBillingPlan_MissingName(t *testing.T) {
	s := newBillingTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"price":9900}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/plans", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleBillingPlans(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

// TestHandleGetBillingPlan 验证正常获取计划详情。
func TestHandleGetBillingPlan(t *testing.T) {
	s := newBillingTestServer()
	auth := loginAsAdmin(t, s)
	created := s.store.CreateBillingPlan(&store.SubscriptionPlan{Name: "basic", Price: 9900})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing/plans/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleBillingPlanRouting(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleDeleteBillingPlan 验证正常删除计划。
func TestHandleDeleteBillingPlan(t *testing.T) {
	s := newBillingTestServer()
	auth := loginAsAdmin(t, s)
	created := s.store.CreateBillingPlan(&store.SubscriptionPlan{Name: "basic", Price: 9900})
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/billing/plans/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleBillingPlanRouting(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// ============================================================================
// 订阅
// ============================================================================

// TestHandleListSubscriptions_Empty 验证空列表返回 200。
func TestHandleListSubscriptions_Empty(t *testing.T) {
	s := newBillingTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing/subscriptions", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleBillingSubscriptions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreateSubscription 验证正常创建订阅返回 201。
func TestHandleCreateSubscription(t *testing.T) {
	s := newBillingTestServer()
	auth := loginAsAdmin(t, s)
	plan := s.store.CreateBillingPlan(&store.SubscriptionPlan{Name: "basic", Price: 9900})
	body := `{"planId":"` + plan.ID + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/subscriptions", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleBillingSubscriptions(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleGetSubscription 验证正常获取订阅详情。
func TestHandleGetSubscription(t *testing.T) {
	s := newBillingTestServer()
	auth := loginAsAdmin(t, s)
	created := s.store.CreateSubscription(&store.Subscription{TenantID: "default", PlanID: "p1", Status: "active"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing/subscriptions/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleBillingSubscriptionRouting(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleDeleteSubscription 验证正常删除订阅。
func TestHandleDeleteSubscription(t *testing.T) {
	s := newBillingTestServer()
	auth := loginAsAdmin(t, s)
	created := s.store.CreateSubscription(&store.Subscription{TenantID: "default", PlanID: "p1", Status: "active"})
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/billing/subscriptions/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleBillingSubscriptionRouting(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// ============================================================================
// 账单
// ============================================================================

// TestHandleListInvoices_Empty 验证空列表返回 200。
func TestHandleListInvoices_Empty(t *testing.T) {
	s := newBillingTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing/invoices", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleBillingInvoices(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
}

// TestHandleGetInvoice 验证正常获取账单详情。
func TestHandleGetInvoice(t *testing.T) {
	s := newBillingTestServer()
	auth := loginAsAdmin(t, s)
	created := s.store.CreateInvoice(&store.Invoice{TenantID: "default", Amount: 9900, Status: "pending"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing/invoices/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleBillingInvoiceRouting(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleGetInvoice_NotFound 验证获取不存在账单返回 404。
func TestHandleGetInvoice_NotFound(t *testing.T) {
	s := newBillingTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing/invoices/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleBillingInvoiceRouting(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// TestHandleBillingPlans_NoAuth 验证无 Authorization 头返回 401。
func TestHandleBillingPlans_NoAuth(t *testing.T) {
	s := newBillingTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing/plans", nil)
	w := httptest.NewRecorder()
	s.handleBillingPlans(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", w.Code)
	}
}