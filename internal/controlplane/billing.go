package controlplane
// billing.go 实现 Phase 6 计费 HTTP handler（计划/订阅/账单 CRUD）。
//
// API 端点：
//   - GET    /api/v1/billing/plans           列出订阅计划
//   - POST   /api/v1/billing/plans           创建订阅计划
//   - GET    /api/v1/billing/plans/{id}      计划详情
//   - PUT    /api/v1/billing/plans/{id}      更新计划
//   - DELETE /api/v1/billing/plans/{id}      删除计划
//   - GET    /api/v1/billing/subscriptions   列出订阅
//   - POST   /api/v1/billing/subscriptions   创建订阅
//   - GET    /api/v1/billing/subscriptions/{id}  订阅详情
//   - PUT    /api/v1/billing/subscriptions/{id}  更新订阅
//   - DELETE /api/v1/billing/subscriptions/{id}  删除订阅
//   - GET    /api/v1/billing/invoices        列出账单
//   - GET    /api/v1/billing/invoices/{id}   账单详情


import (
	"net/http"
	"strings"

	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// ============================================================================
// 订阅计划
// ============================================================================

// handleBillingPlans 统一处理 /api/v1/billing/plans。
func (s *Server) handleBillingPlans(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListBillingPlans(w, r)
	case http.MethodPost:
		s.handleCreateBillingPlan(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListBillingPlans 处理 GET /api/v1/billing/plans。
func (s *Server) handleListBillingPlans(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "billing:read"); !ok {
		return
	}
	plans := s.store.ListBillingPlans()
	writeJSON(w, http.StatusOK, map[string]interface{}{"plans": plans})
}

// handleCreateBillingPlan 处理 POST /api/v1/billing/plans。
func (s *Server) handleCreateBillingPlan(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "billing:write")
	if !ok {
		return
	}
	var body store.SubscriptionPlan
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if body.Interval == "" {
		body.Interval = "monthly"
	}
	created := s.store.CreateBillingPlan(&body)
	if created == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "create plan failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "billing_plan_create", Target: created.ID, Detail: sanitizeAuditDetail("name=" + created.Name),
	})
	writeJSON(w, http.StatusCreated, created)
}

// handleBillingPlanRouting 分派 /api/v1/billing/plans/{id} 子路径。
func (s *Server) handleBillingPlanRouting(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/billing/plans/")
	if rest == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "plan id required"})
		return
	}
	id := rest
	if strings.Contains(id, "/") {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleGetBillingPlan(w, r, id)
	case http.MethodPut:
		s.handleUpdateBillingPlan(w, r, id)
	case http.MethodDelete:
		s.handleDeleteBillingPlan(w, r, id)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleGetBillingPlan 处理 GET /api/v1/billing/plans/{id}。
func (s *Server) handleGetBillingPlan(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "billing:read"); !ok {
		return
	}
	p, ok := s.store.GetBillingPlan(id)
	if !ok || p == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "plan not found"})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// handleUpdateBillingPlan 处理 PUT /api/v1/billing/plans/{id}。
func (s *Server) handleUpdateBillingPlan(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "billing:write")
	if !ok {
		return
	}
	var body store.SubscriptionPlan
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	body.ID = id
	updated, ok := s.store.UpdateBillingPlan(&body)
	if !ok || updated == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "plan not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "billing_plan_update", Target: id, Detail: "",
	})
	writeJSON(w, http.StatusOK, updated)
}

// handleDeleteBillingPlan 处理 DELETE /api/v1/billing/plans/{id}。
func (s *Server) handleDeleteBillingPlan(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "billing:write")
	if !ok {
		return
	}
	if !s.store.DeleteBillingPlan(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "plan not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "billing_plan_delete", Target: id, Detail: "",
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ============================================================================
// 订阅
// ============================================================================

// handleBillingSubscriptions 统一处理 /api/v1/billing/subscriptions。
func (s *Server) handleBillingSubscriptions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListSubscriptions(w, r)
	case http.MethodPost:
		s.handleCreateSubscription(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListSubscriptions 处理 GET /api/v1/billing/subscriptions。
func (s *Server) handleListSubscriptions(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "billing:read"); !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	subs := s.store.ListSubscriptions(tenant)
	writeJSON(w, http.StatusOK, map[string]interface{}{"subscriptions": subs})
}

// handleCreateSubscription 处理 POST /api/v1/billing/subscriptions。
func (s *Server) handleCreateSubscription(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "billing:write")
	if !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	var body store.Subscription
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.PlanID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "planId is required"})
		return
	}
	body.TenantID = tenant
	if body.Status == "" {
		body.Status = "active"
	}
	created := s.store.CreateSubscription(&body)
	if created == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "create subscription failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenant, UserID: caller.ID, Action: "subscription_create", Target: created.ID, Detail: sanitizeAuditDetail("plan=" + created.PlanID),
	})
	writeJSON(w, http.StatusCreated, created)
}

// handleBillingSubscriptionRouting 分派 /api/v1/billing/subscriptions/{id} 子路径。
func (s *Server) handleBillingSubscriptionRouting(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/billing/subscriptions/")
	if rest == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "subscription id required"})
		return
	}
	id := rest
	if strings.Contains(id, "/") {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleGetSubscription(w, r, id)
	case http.MethodPut:
		s.handleUpdateSubscription(w, r, id)
	case http.MethodDelete:
		s.handleDeleteSubscription(w, r, id)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleGetSubscription 处理 GET /api/v1/billing/subscriptions/{id}。
func (s *Server) handleGetSubscription(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "billing:read"); !ok {
		return
	}
	sub, ok := s.store.GetSubscription(id)
	if !ok || sub == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "subscription not found"})
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

// handleUpdateSubscription 处理 PUT /api/v1/billing/subscriptions/{id}。
func (s *Server) handleUpdateSubscription(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "billing:write")
	if !ok {
		return
	}
	var body store.Subscription
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	body.ID = id
	updated, ok := s.store.UpdateSubscription(&body)
	if !ok || updated == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "subscription not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: updated.TenantID, UserID: caller.ID, Action: "subscription_update", Target: id, Detail: "",
	})
	writeJSON(w, http.StatusOK, updated)
}

// handleDeleteSubscription 处理 DELETE /api/v1/billing/subscriptions/{id}。
func (s *Server) handleDeleteSubscription(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "billing:write")
	if !ok {
		return
	}
	if !s.store.DeleteSubscription(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "subscription not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "subscription_delete", Target: id, Detail: "",
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ============================================================================
// 账单
// ============================================================================

// handleBillingInvoices 处理 GET /api/v1/billing/invoices：列出账单。
func (s *Server) handleBillingInvoices(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "billing:read"); !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	invoices := s.store.ListInvoices(tenant)
	writeJSON(w, http.StatusOK, map[string]interface{}{"invoices": invoices})
}

// handleBillingInvoiceRouting 处理 /api/v1/billing/invoices/{id}：账单详情。
func (s *Server) handleBillingInvoiceRouting(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requirePermission(w, r, "billing:read"); !ok {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/billing/invoices/")
	if rest == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invoice id required"})
		return
	}
	id := rest
	if strings.Contains(id, "/") {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path"})
		return
	}
	inv, ok := s.store.GetInvoice(id)
	if !ok || inv == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "invoice not found"})
		return
	}
	writeJSON(w, http.StatusOK, inv)
}