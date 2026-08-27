package controlplane

// apikey.go 实现 Phase 6 API Key 管理 HTTP handler（CRUD + 启停 + 生成）。
//
// API 端点：
//   - GET    /api/v1/apikeys        列出 API Key
//   - POST   /api/v1/apikeys        创建 API Key（返回明文 key，仅此一次）
//   - GET    /api/v1/apikeys/{id}   API Key 详情
//   - PUT    /api/v1/apikeys/{id}   更新 API Key
//   - DELETE /api/v1/apikeys/{id}   删除 API Key
//   - POST   /api/v1/apikeys/{id}/enable   启用 API Key
//   - POST   /api/v1/apikeys/{id}/disable  禁用 API Key

import (
	"opsmesh/internal/controlplane/paginate"
	"net/http"
	"strings"

	"opsmesh/internal/platform"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// handleAPIKeys 统一处理 /api/v1/apikeys。
func (s *Server) handleAPIKeys(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListAPIKeys(w, r)
	case http.MethodPost:
		s.handleCreateAPIKey(w, r)
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListAPIKeys 处理 GET /api/v1/apikeys：列出 API Key。
func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "apikey:read"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	keys := s.store.ListAPIKeys(actx.TenantID)
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"apiKeys": keys})
}

// handleCreateAPIKey 处理 POST /api/v1/apikeys：创建 API Key（返回明文 key，仅此一次）。
func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "apikey:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	var body store.APIKey
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Name == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	// 生成明文 key 与 hash。
	plainKey, hash, err := platform.GenerateAPIKey()
	if err != nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "generate api key failed: " + err.Error()})
		return
	}
	body.Key = hash
	body.Enabled = true
	created := s.store.CreateAPIKey(actx.TenantID, &body)
	if created == nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "create api key failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "apikey_create", Target: created.ID, Detail: sanitizeAuditDetail("name=" + created.Name),
	})
	// 返回创建结果 + 明文 key（仅此一次）。
	paginate.WriteJSON(w, http.StatusCreated, map[string]interface{}{
		"apiKey":   created,
		"plainKey": plainKey,
	})
}

// handleAPIKeyRouting 分派 /api/v1/apikeys/{id} 子路径。
func (s *Server) handleAPIKeyRouting(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/apikeys/")
	if rest == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "api key id required"})
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "api key id required"})
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			s.handleGetAPIKey(w, r, id)
		case http.MethodPut:
			s.handleUpdateAPIKey(w, r, id)
		case http.MethodDelete:
			s.handleDeleteAPIKey(w, r, id)
		default:
			paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}
	action := parts[1]
	switch action {
	case "enable":
		s.handleEnableAPIKey(w, r, id)
	case "disable":
		s.handleDisableAPIKey(w, r, id)
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + action})
	}
}

// handleGetAPIKey 处理 GET /api/v1/apikeys/{id}。
func (s *Server) handleGetAPIKey(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "apikey:read"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	k, ok := s.store.GetAPIKey(actx.TenantID, id)
	if !ok || k == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "api key not found"})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, k)
}

// handleUpdateAPIKey 处理 PUT /api/v1/apikeys/{id}。
//
// 安全：白名单字段合并，防止提权。请求体仅允许更新 Name 与 Scopes；
// Enabled 必须走 /enable|/disable 端点（此处强制保留 existing.Enabled）；
// Key（SHA-256 hash）/ ID / TenantID / CreatedAt 等敏感字段强制保留 existing 值。
// Scopes 不允许清空（len==0 返 400），防止客户端把权限范围置空绕过校验。
func (s *Server) handleUpdateAPIKey(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "apikey:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	// 先取 existing，不存在返 404。
	existing, ok := s.store.GetAPIKey(actx.TenantID, id)
	if !ok || existing == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "api key not found"})
		return
	}
	var body store.APIKey
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	// 白名单：scopes 不允许清空（防提权）。
	if len(body.Scopes) == 0 {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "scopes must not be empty"})
		return
	}
	// 按白名单覆盖可编辑字段。
	if body.Name != "" {
		existing.Name = body.Name
	}
	existing.Scopes = body.Scopes
	// 强制保留：Enabled / Key / ID / TenantID / CreatedAt 等忽略 PUT 值。
	// （body.Key 因 json:"-" 标签始终为空，此处显式不动 existing.Key 即可。）
	updated, ok := s.store.UpdateAPIKey(actx.TenantID, existing)
	if !ok || updated == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "api key not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "apikey_update", Target: id, Detail: sanitizeAuditDetail("name=" + updated.Name),
	})
	paginate.WriteJSON(w, http.StatusOK, updated)
}

// handleDeleteAPIKey 处理 DELETE /api/v1/apikeys/{id}。
func (s *Server) handleDeleteAPIKey(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "apikey:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	if !s.store.DeleteAPIKey(actx.TenantID, id) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "api key not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "apikey_delete", Target: id, Detail: "",
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleEnableAPIKey 处理 POST /api/v1/apikeys/{id}/enable。
func (s *Server) handleEnableAPIKey(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "apikey:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	k, ok := s.store.GetAPIKey(actx.TenantID, id)
	if !ok || k == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "api key not found"})
		return
	}
	k.Enabled = true
	updated, ok := s.store.UpdateAPIKey(actx.TenantID, k)
	if !ok || updated == nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "enable api key failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "apikey_enable", Target: id, Detail: "",
	})
	paginate.WriteJSON(w, http.StatusOK, updated)
}

// handleDisableAPIKey 处理 POST /api/v1/apikeys/{id}/disable。
func (s *Server) handleDisableAPIKey(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "apikey:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	k, ok := s.store.GetAPIKey(actx.TenantID, id)
	if !ok || k == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "api key not found"})
		return
	}
	k.Enabled = false
	updated, ok := s.store.UpdateAPIKey(actx.TenantID, k)
	if !ok || updated == nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "disable api key failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "apikey_disable", Target: id, Detail: "",
	})
	paginate.WriteJSON(w, http.StatusOK, updated)
}
