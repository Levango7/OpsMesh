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

	"opsmesh/internal/controlplane/paginate"

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
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListTenants 处理 GET /api/v1/tenants：列出租户。
func (s *Server) handleListTenants(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "tenant:read"); !ok {
		return
	}
	tenants := s.store.ListTenants()
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"tenants": tenants})
}

// handleCreateTenant 处理 POST /api/v1/tenants：创建租户。
func (s *Server) handleCreateTenant(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "tenant:write")
	if !ok {
		return
	}
	var body store.Tenant
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Name == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if body.Status == "" {
		body.Status = store.TenantStatusActive
	}
	created := s.store.CreateTenant(&body)
	if created == nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "create tenant failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "tenant_create", Target: created.ID, Detail: sanitizeAuditDetail("name=" + created.Name),
	})
	paginate.WriteJSON(w, http.StatusCreated, created)
}

// handleTenantRouting 分派 /api/v1/tenants/{id} 子路径。
func (s *Server) handleTenantRouting(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/tenants/")
	if rest == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant id required"})
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant id required"})
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
			paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
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
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + action})
	}
}

// handleGetTenant 处理 GET /api/v1/tenants/{id}：租户详情。
func (s *Server) handleGetTenant(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "tenant:read"); !ok {
		return
	}
	t, ok := s.store.GetTenant(id)
	if !ok || t == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "tenant not found"})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, t)
}

// handleUpdateTenant 处理 PUT /api/v1/tenants/{id}：更新租户。
func (s *Server) handleUpdateTenant(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "tenant:write")
	if !ok {
		return
	}
	var body store.Tenant
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	body.ID = id
	updated, ok := s.store.UpdateTenant(&body)
	if !ok || updated == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "tenant not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "tenant_update", Target: id, Detail: sanitizeAuditDetail("name=" + updated.Name),
	})
	paginate.WriteJSON(w, http.StatusOK, updated)
}

// handleDeleteTenant 处理 DELETE /api/v1/tenants/{id}：删除租户。
//
// L3 级联清理加固：
//   - 平台租户 "default" 拒绝删除（409 Conflict），防止误删平台根租户导致系统瘫痪；
//   - 删除成功后对该租户的 APIKey/Webhook/Script 三域执行循环 Delete，
//     避免租户删除后子资源成为孤儿（store 已有按 tenantID 的 delete 方法）；
//   - 其余域（Device/Task/Alert/Agent/Subscription/Invoice/Plugin 等）暂未级联，
//     列入 TODO 待后续补齐。
func (s *Server) handleDeleteTenant(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "tenant:write")
	if !ok {
		return
	}
	// L3 平台租户保护：禁止删除 "default" 租户。
	if id == "default" {
		paginate.WriteJSON(w, http.StatusConflict, map[string]string{"error": "cannot delete platform tenant 'default'"})
		return
	}
	if !s.store.DeleteTenant(id) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "tenant not found"})
		return
	}
	// L3 三域级联清理：APIKey/Webhook/Script。
	// store 已有按 tenantID 的 List+Delete 方法，handler 侧聚合调用确保孤儿资源被清。
	for _, k := range s.store.ListAPIKeys(id) {
		if k != nil {
			s.store.DeleteAPIKey(id, k.ID)
		}
	}
	for _, wh := range s.store.ListWebhooks(id) {
		if wh != nil {
			s.store.DeleteWebhook(id, wh.ID)
		}
	}
	for _, sc := range s.store.ListScripts(id) {
		if sc != nil {
			s.store.DeleteScript(id, sc.ID)
		}
	}
	// TODO(L3-future): 级联清理 Device/Task/Alert/Agent/Subscription/Invoice/Plugin 等域。
	// 当前 store 已有部分按 tenantID 的 List/Delete，待 handler 侧逐一补齐聚合调用。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "tenant_delete", Target: id, Detail: "cascade:apikeys,webhooks,scripts",
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleSuspendTenant 处理 POST /api/v1/tenants/{id}/suspend：暂停租户。
func (s *Server) handleSuspendTenant(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "tenant:write")
	if !ok {
		return
	}
	t, ok := s.store.GetTenant(id)
	if !ok || t == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "tenant not found"})
		return
	}
	t.Status = store.TenantStatusSuspended
	updated, ok := s.store.UpdateTenant(t)
	if !ok || updated == nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "suspend tenant failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "tenant_suspend", Target: id, Detail: "",
	})
	paginate.WriteJSON(w, http.StatusOK, updated)
}

// handleActivateTenant 处理 POST /api/v1/tenants/{id}/activate：激活租户。
func (s *Server) handleActivateTenant(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "tenant:write")
	if !ok {
		return
	}
	t, ok := s.store.GetTenant(id)
	if !ok || t == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "tenant not found"})
		return
	}
	t.Status = store.TenantStatusActive
	updated, ok := s.store.UpdateTenant(t)
	if !ok || updated == nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "activate tenant failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "tenant_activate", Target: id, Detail: "",
	})
	paginate.WriteJSON(w, http.StatusOK, updated)
}
