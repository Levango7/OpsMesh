// billing_test.go 测试 Phase 6 计费 HTTP handler（billing.go）。
package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opsmesh/internal/config"
	"opsmesh/internal/proto"
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

// ============================================================================
// M10 错误路径补强：订阅不存在（404）/超限参数（400）/租户隔离（403）/405/401
// ============================================================================

// TestHandleGetSubscription_NotFound 验证获取不存在订阅返回 404。
func TestHandleGetSubscription_NotFound(t *testing.T) {
	s := newBillingTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing/subscriptions/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleBillingSubscriptionRouting(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleDeleteSubscription_NotFound 验证删除不存在订阅返回 404。
func TestHandleDeleteSubscription_NotFound(t *testing.T) {
	s := newBillingTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/billing/subscriptions/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleBillingSubscriptionRouting(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleGetBillingPlan_NotFound 验证获取不存在计划返回 404。
func TestHandleGetBillingPlan_NotFound(t *testing.T) {
	s := newBillingTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing/plans/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleBillingPlanRouting(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleDeleteBillingPlan_NotFound 验证删除不存在计划返回 404。
func TestHandleDeleteBillingPlan_NotFound(t *testing.T) {
	s := newBillingTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/billing/plans/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleBillingPlanRouting(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreateSubscription_MissingPlanID 验证缺 planId 返回 400（参数缺失/超限）。
func TestHandleCreateSubscription_MissingPlanID(t *testing.T) {
	s := newBillingTestServer()
	auth := loginAsAdmin(t, s)
	body := `{}` // 缺 planId
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/subscriptions", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleBillingSubscriptions(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreateSubscription_InvalidJSON 验证非法 JSON body 返回 400。
func TestHandleCreateSubscription_InvalidJSON(t *testing.T) {
	s := newBillingTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"planId":"p1"` // 缺右大括号
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/subscriptions", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleBillingSubscriptions(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreateSubscription_TokenFallbackNoHeader 验证缺 X-Tenant-ID 头时创建走 token 回退。
// admin token 携带 tenant_id=default，requireTenantContext 头空时回退到 token tenant → 201。
func TestHandleCreateSubscription_TokenFallbackNoHeader(t *testing.T) {
	s := newBillingTestServer()
	auth := loginAsAdmin(t, s)
	plan := s.store.CreateBillingPlan(&store.SubscriptionPlan{Name: "basic", Price: 9900})
	body := `{"planId":"` + plan.ID + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/subscriptions", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	// 故意不设 X-Tenant-ID：requireTenantContext 回退到 token tenant_id=default
	w := httptest.NewRecorder()
	s.handleBillingSubscriptions(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201 (token 回退 default 创建成功); body=%s", w.Code, w.Body.String())
	}
}

// TestHandleBillingPlans_MethodNotAllowed 验证 PATCH /plans 返回 405。
func TestHandleBillingPlans_MethodNotAllowed(t *testing.T) {
	s := newBillingTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/billing/plans", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleBillingPlans(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleBillingSubscriptions_MethodNotAllowed 验证 PATCH /subscriptions 返回 405。
func TestHandleBillingSubscriptions_MethodNotAllowed(t *testing.T) {
	s := newBillingTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/billing/subscriptions", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleBillingSubscriptions(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleBillingInvoices_PostTreatedAsGet 验证 handleBillingInvoices 不校验 method。
// handleBillingInvoices 内部无 method switch，路由层只注册 GET；handler 直接走鉴权+列账单逻辑，
// POST 被当作 GET 处理返回 200。锁定当前设计基线：若未来给 invoices 加 method 校验，
// 此测试会失败提示更新断言为 405。
func TestHandleBillingInvoices_PostTreatedAsGet(t *testing.T) {
	s := newBillingTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/invoices", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleBillingInvoices(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 (handleBillingInvoices 不校验 method，POST 当 GET 处理); body=%s", w.Code, w.Body.String())
	}
}

// TestHandleListSubscriptions_TenantIsolation 验证跨租户访问订阅列表返回 403。
// admin token 携带 tenant_id=default，带 X-Tenant-ID:other-tenant 访问，
// requireTenantContext 交叉校验 token tenant ≠ header tenant → 403。
func TestHandleListSubscriptions_TenantIsolation(t *testing.T) {
	s := newBillingTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing/subscriptions", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "other-tenant") // 跨租户头
	w := httptest.NewRecorder()
	s.handleBillingSubscriptions(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleListInvoices_TenantIsolation 验证跨租户访问账单列表返回 403。
func TestHandleListInvoices_TenantIsolation(t *testing.T) {
	s := newBillingTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing/invoices", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "other-tenant") // 跨租户头
	w := httptest.NewRecorder()
	s.handleBillingInvoices(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleBillingUsage 验证获取资源用量统计返回 200。
func TestHandleBillingUsage(t *testing.T) {
	s := newBillingTestServer()
	auth := loginAsAdmin(t, s)

	// 创建一些数据（需要 AgentID 才能正确存储）
	s.store.CreateTask(&proto.Task{TaskID: "task-1", AgentID: "agent-1", TenantID: "default", Status: "pending"})
	s.store.CreateTask(&proto.Task{TaskID: "task-2", AgentID: "agent-1", TenantID: "default", Status: "running"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing/usage", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleBillingUsage(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}

	var resp store.Usage
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.TaskCount < 2 {
		t.Fatalf("TaskCount = %d, want >= 2", resp.TaskCount)
	}
}

// TestHandleBillingUsage_NoAuth 验证未认证返回 401。
func TestHandleBillingUsage_NoAuth(t *testing.T) {
	s := newBillingTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing/usage", nil)
	w := httptest.NewRecorder()
	s.handleBillingUsage(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", w.Code)
	}
}

// TestHandleBillingUsage_MethodNotAllowed 验证 POST 返回 405。
func TestHandleBillingUsage_MethodNotAllowed(t *testing.T) {
	s := newBillingTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/usage", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleBillingUsage(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", w.Code)
	}
}
