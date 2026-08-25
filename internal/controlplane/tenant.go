package controlplane
// tenant.go 实现 Phase 6 租户管理 HTTP handler（CRUD + 启停）。
//
// API 端点：
//   - GET    /api/v1/tenants        列出租户
//   - POST   /api/v1/tenants        创建租户
//   - GET    /api/v1/tenants/{id}   租户详情
//   - PUT    /api/v1/tenants/{id}   更新租户
//   - DELETE /api/v1/tenants/{id}   删除租户
//   - POST   /api/v1/tenants/{id}/suspend  暂停租户
//   - POST   /api/v1/tenants/{id}/activate 激活租户
//
// 设计要点（与 webhook.go 风格一致）：
//   - 鉴权：需 tenant:read/tenant:write 权限；
//   - 错误响应统一 {"error": "message"} 格式；
//   - 用 decodeJSONBody 解析请求体。


import (
	"net/http"
	"strings"

	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// handleTenants 统一处理 /api/v1/tenants：
//   - GET：列出租户
//   - POST：创建租户
func (s *Server) handleTenants(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListTenants(w, r)
	case http.MethodPost:
		s.handleCreateTenant(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListTenants 处理 GET /api/v1/tenants：列出租户。
func (s *Server) handleListTenants(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "tenant:read"); !ok {
		return
	}
	tenants := s.store.ListTenants()
	writeJSON(w, http.StatusOK, map[string]interface{}{"tenants": tenants})
}

// handleCreateTenant 处理 POST /api/v1/tenants：创建租户。
func (s *Server) handleCreateTenant(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "tenant:write")
	if !ok {
		return
	}
	var body store.Tenant
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if body.Status == "" {
		body.Status = store.TenantStatusActive
	}
	created := s.store.CreateTenant(&body)
	if created == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "create tenant failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "tenant_create", Target: created.ID, Detail: sanitizeAuditDetail("name=" + created.Name),
	})
	writeJSON(w, http.StatusCreated, created)
}

// handleTenantRouting 分派 /api/v1/tenants/{id} 子路径。
func (s *Server) handleTenantRouting(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/tenants/")
	if rest == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant id required"})
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant id required"})
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			s.handleGetTenant(w, r, id)
		case http.MethodPut:
			s.handleUpdateTenant(w, r, id)
		case http.MethodDelete:
			s.handleDeleteTenant(w, r, id)
		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}
	action := parts[1]
	switch action {
	case "suspend":
		s.handleSuspendTenant(w, r, id)
	case "activate":
		s.handleActivateTenant(w, r, id)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + action})
	}
}

// handleGetTenant 处理 GET /api/v1/tenants/{id}：租户详情。
func (s *Server) handleGetTenant(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "tenant:read"); !ok {
		return
	}
	t, ok := s.store.GetTenant(id)
	if !ok || t == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "tenant not found"})
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// handleUpdateTenant 处理 PUT /api/v1/tenants/{id}：更新租户。
func (s *Server) handleUpdateTenant(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "tenant:write")
	if !ok {
		return
	}
	var body store.Tenant
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	body.ID = id
	updated, ok := s.store.UpdateTenant(&body)
	if !ok || updated == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "tenant not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "tenant_update", Target: id, Detail: sanitizeAuditDetail("name=" + updated.Name),
	})
	writeJSON(w, http.StatusOK, updated)
}

// handleDeleteTenant 处理 DELETE /api/v1/tenants/{id}：删除租户。
func (s *Server) handleDeleteTenant(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "tenant:write")
	if !ok {
		return
	}
	if !s.store.DeleteTenant(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "tenant not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "tenant_delete", Target: id, Detail: "",
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleSuspendTenant 处理 POST /api/v1/tenants/{id}/suspend：暂停租户。
func (s *Server) handleSuspendTenant(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "tenant:write")
	if !ok {
		return
	}
	t, ok := s.store.GetTenant(id)
	if !ok || t == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "tenant not found"})
		return
	}
	t.Status = store.TenantStatusSuspended
	updated, ok := s.store.UpdateTenant(t)
	if !ok || updated == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "suspend tenant failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "tenant_suspend", Target: id, Detail: "",
	})
	writeJSON(w, http.StatusOK, updated)
}

// handleActivateTenant 处理 POST /api/v1/tenants/{id}/activate：激活租户。
func (s *Server) handleActivateTenant(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "tenant:write")
	if !ok {
		return
	}
	t, ok := s.store.GetTenant(id)
	if !ok || t == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "tenant not found"})
		return
	}
	t.Status = store.TenantStatusActive
	updated, ok := s.store.UpdateTenant(t)
	if !ok || updated == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "activate tenant failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "tenant_activate", Target: id, Detail: "",
	})
	writeJSON(w, http.StatusOK, updated)
}