// auth_roles.go 实现角色管理 handler：/api/v1/roles CRUD。
//
// 从 auth.go 拆分而来（纯代码移动，未修改任何逻辑）。依赖 auth.go 中的
// requirePermission 鉴权中间件、userFromToken、randHexID helper，
// 以及 server.go 中的 writeJSON/decodeJSONBody 响应 helper。
package controlplane

import (
	"log"
	"net/http"
	"strings"

	"opsmesh/internal/controlplane/paginate"

	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// ============================================================================
// 角色管理 handler：/api/v1/roles
// ============================================================================

// handleRoles 统一处理 /api/v1/roles：
//   - GET：列出全部角色（需 role:read 权限，但为简化前端展示，登录用户均可查看）
//   - POST：创建角色（需 role:write 权限）
func (s *Server) handleRoles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListRoles(w, r)
	case http.MethodPost:
		s.handleCreateRole(w, r)
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListRoles 处理 GET /api/v1/roles：列出全部角色。
// 鉴权：仅需有效 token（登录用户均可查看角色列表，便于前端角色选择下拉框）。
func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	if _, err := s.userFromToken(r); err != nil {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"roles": s.store.ListRoles()})
}

// handleCreateRole 处理 POST /api/v1/roles：创建角色（需 role:write 权限）。
// 请求体：{name, description, permissions[]}。
func (s *Server) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "role:write")
	if !ok {
		return
	}
	var body struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		log.Printf("controlplane: handleCreateRole 解析请求体失败: %v", err)
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if body.Name == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "role name is required"})
		return
	}
	role := &store.Role{
		ID:          randHexID("role"),
		Name:        body.Name,
		Description: body.Description,
		Permissions: body.Permissions,
	}
	if s.store.CreateRole(role) == nil {
		paginate.WriteJSON(w, http.StatusConflict, map[string]string{"error": "role name already exists"})
		return
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "role_create", Target: role.ID, Detail: sanitizeAuditDetail("name=" + role.Name),
	})
	paginate.WriteJSON(w, http.StatusCreated, role)
}

// handleRoleRouting 分派 /api/v1/roles/{id} 子路径：
//   - PUT /api/v1/roles/{id}：更新角色（需 role:write 权限）
//   - DELETE /api/v1/roles/{id}：删除角色（需 role:delete 权限）
func (s *Server) handleRoleRouting(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/roles/")
	if id == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "role id required"})
		return
	}
	switch r.Method {
	case http.MethodPut:
		s.handleUpdateRole(w, r, id)
	case http.MethodDelete:
		s.handleDeleteRole(w, r, id)
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleUpdateRole 处理 PUT /api/v1/roles/{id}：更新角色 description/permissions（需 role:write 权限）。
func (s *Server) handleUpdateRole(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "role:write")
	if !ok {
		return
	}
	var body struct {
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		log.Printf("controlplane: handleUpdateRole 解析请求体失败: %v", err)
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	existing := s.store.GetRole(id)
	if existing == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "role not found"})
		return
	}
	if body.Description != "" {
		existing.Description = body.Description
	}
	if body.Permissions != nil {
		existing.Permissions = body.Permissions
	}
	if !s.store.UpdateRole(existing) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "role not found"})
		return
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "role_update", Target: id, Detail: "updated via HTTP",
	})
	paginate.WriteJSON(w, http.StatusOK, s.store.GetRole(id))
}

// handleDeleteRole 处理 DELETE /api/v1/roles/{id}：删除角色（需 role:delete 权限）。
func (s *Server) handleDeleteRole(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "role:delete")
	if !ok {
		return
	}
	if s.store.GetRole(id) == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "role not found"})
		return
	}
	if !s.store.DeleteRole(id) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "role not found"})
		return
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "role_delete", Target: id, Detail: "deleted via HTTP",
	})
	w.WriteHeader(http.StatusNoContent)
}
