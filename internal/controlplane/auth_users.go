// auth_users.go 实现用户管理 handler：/api/v1/users CRUD + 审批/拒绝。
//
// 从 auth.go 拆分而来（纯代码移动，未修改任何逻辑）。依赖 auth.go 中的
// requirePermission 鉴权中间件、hashPassword、randHexID、validateStrongPassword helper，
// 以及 server.go 中的 writeJSON/decodeJSONBody 响应 helper。
package controlplane

import (
	"io"
	"log"
	"net/http"
	"strings"

	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// ============================================================================
// 用户管理 handler：/api/v1/users
// ============================================================================

// handleUsers 统一处理 /api/v1/users：
//   - GET：列出全部用户（需 user:read 权限）
//   - POST：创建用户（需 user:write 权限）
func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListUsers(w, r)
	case http.MethodPost:
		s.handleCreateUser(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListUsers 处理 GET /api/v1/users：列出全部用户（需 user:read 权限）。
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "user:read"); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"users": s.store.ListUsers()})
}

// handleCreateUser 处理 POST /api/v1/users：创建用户（需 user:write 权限）。
// 请求体：{username, password, email?, role_ids?}；密码最短 6 字符，bcrypt 哈希后存库。
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "user:write")
	if !ok {
		return
	}
	var body struct {
		Username string   `json:"username"`
		Password string   `json:"password"`
		Email    string   `json:"email"`
		RoleIDs  []string `json:"role_ids"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		log.Printf("controlplane: handleCreateUser 解析请求体失败: %v", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if body.Username == "" || body.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password are required"})
		return
	}
	if msg := validateStrongPassword(body.Password); msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}
	// P3 角色引用校验：role_ids 若存在须全部指向真实角色，避免写入无效角色引用。
	for _, rid := range body.RoleIDs {
		if rid != "" && s.store.GetRole(rid) == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown role id: " + rid})
			return
		}
	}
	if s.store.GetUserByUsername(body.Username) != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "username already exists"})
		return
	}
	hash, err := hashPassword(body.Password)
	if err != nil {
		log.Printf("controlplane: handleCreateUser 哈希密码失败: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	u := &store.User{
		ID:           randHexID("user"),
		Username:     body.Username,
		Email:        body.Email,
		PasswordHash: hash,
		Status:       "active",
		RoleIDs:      body.RoleIDs,
	}
	if s.store.CreateUser(u) == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "username already exists"})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "user_create", Target: u.ID, Detail: "username=" + u.Username,
	})
	writeJSON(w, http.StatusCreated, u)
}

// handleUserRouting 分派 /api/v1/users/{id} 子路径：
//   - PUT /api/v1/users/{id}：更新用户（需 user:write 权限）
//   - DELETE /api/v1/users/{id}：删除用户（需 user:delete 权限）
//   - POST /api/v1/users/{id}/approve：审批用户（需 user:approve 权限，P1-7 注册安全）
//   - POST /api/v1/users/{id}/reject：拒绝用户（需 user:approve 权限，P1-7 注册安全）
func (s *Server) handleUserRouting(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/users/")
	if rest == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user id required"})
		return
	}
	// 解析 {id} 或 {id}/approve 或 {id}/reject。
	id := rest
	subAction := ""
	if idx := strings.Index(rest, "/"); idx > 0 {
		id = rest[:idx]
		subAction = rest[idx+1:]
	}
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user id required"})
		return
	}
	// 子路径分发（approve/reject）。
	if subAction != "" {
		switch subAction {
		case "approve":
			s.handleApproveUser(w, r, id)
			return
		case "reject":
			s.handleRejectUser(w, r, id)
			return
		default:
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + subAction})
			return
		}
	}
	// 主路径分发（PUT/DELETE）。
	switch r.Method {
	case http.MethodPut:
		s.handleUpdateUser(w, r, id)
	case http.MethodDelete:
		s.handleDeleteUser(w, r, id)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleApproveUser 处理 POST /api/v1/users/{id}/approve：管理员审批用户注册（P1-7 注册安全）。
// 将用户 Status 从 "pending" 改为 "active"；仅 pending 状态可审批，其他状态返回 409。
// 鉴权：需 user:approve 权限（admin 角色自动拥有）。
func (s *Server) handleApproveUser(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	caller, ok := s.requirePermission(w, r, "user:approve")
	if !ok {
		return
	}
	existing := s.store.GetUser(id)
	if existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	if existing.Status != "pending" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "user is not pending (current status: " + existing.Status + ")"})
		return
	}
	existing.Status = "active"
	if !s.store.UpdateUser(existing) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "user_approve", Target: id, Detail: "approved user " + existing.Username,
	})
	writeJSON(w, http.StatusOK, s.store.GetUser(id))
}

// handleRejectUser 处理 POST /api/v1/users/{id}/reject：管理员拒绝用户注册（P1-7 注册安全）。
// 将用户 Status 改为 "rejected"；仅 pending 状态可拒绝，其他状态返回 409。
// 鉴权：需 user:approve 权限（admin 角色自动拥有）。
// 请求体可选：{reason?: "拒绝原因"}，记录到审计日志。
func (s *Server) handleRejectUser(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	caller, ok := s.requirePermission(w, r, "user:approve")
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	// 请求体可选；解析失败时记录日志（兼容空 body 调用）。
	if err := decodeJSONBody(w, r, &body); err != nil && err != io.EOF {
		log.Printf("controlplane: handleApproveUser 解析请求体失败: %v", err)
	}
	existing := s.store.GetUser(id)
	if existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	if existing.Status != "pending" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "user is not pending (current status: " + existing.Status + ")"})
		return
	}
	existing.Status = "rejected"
	if !s.store.UpdateUser(existing) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	detail := "rejected user " + existing.Username
	if body.Reason != "" {
		detail += " reason: " + body.Reason
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "user_reject", Target: id, Detail: detail,
	})
	writeJSON(w, http.StatusOK, s.store.GetUser(id))
}

// handleUpdateUser 处理 PUT /api/v1/users/{id}：更新用户 email/roles/status（需 user:write 权限）。
// 请求体：{email?, role_ids?, status?}；仅更新非空字段。
func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "user:write")
	if !ok {
		return
	}
	var body struct {
		Email   string   `json:"email"`
		RoleIDs []string `json:"role_ids"`
		Status  string   `json:"status"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		log.Printf("controlplane: handleUpdateUser 解析请求体失败: %v", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	// P2 状态变更需更高权限：仅 user:write 不能激活/禁用账号，须 user:approve（与 P1-7 审批模型一致），
	// 防止低权限用户自行把 Status 置 active/rejected 绕过审批流。
	if body.Status != "" {
		if _, ok := s.requirePermission(w, r, "user:approve"); !ok {
			return
		}
	}
	existing := s.store.GetUser(id)
	if existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	if body.Email != "" {
		existing.Email = body.Email
	}
	if body.RoleIDs != nil {
		existing.RoleIDs = body.RoleIDs
	}
	if body.Status != "" {
		existing.Status = body.Status
	}
	if !s.store.UpdateUser(existing) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "user_update", Target: id, Detail: "updated via HTTP",
	})
	writeJSON(w, http.StatusOK, s.store.GetUser(id))
}

// handleDeleteUser 处理 DELETE /api/v1/users/{id}：删除用户（需 user:delete 权限）。
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "user:delete")
	if !ok {
		return
	}
	if s.store.GetUser(id) == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	if !s.store.DeleteUser(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "user_delete", Target: id, Detail: "deleted via HTTP",
	})
	w.WriteHeader(http.StatusNoContent)
}
