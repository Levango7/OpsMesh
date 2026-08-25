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
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListAPIKeys 处理 GET /api/v1/apikeys：列出 API Key。
func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "apikey:read"); !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	keys := s.store.ListAPIKeys(tenant)
	writeJSON(w, http.StatusOK, map[string]interface{}{"apiKeys": keys})
}

// handleCreateAPIKey 处理 POST /api/v1/apikeys：创建 API Key（返回明文 key，仅此一次）。
func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "apikey:write")
	if !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	var body store.APIKey
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	// 生成明文 key 与 hash。
	plainKey, hash, err := platform.GenerateAPIKey()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "generate api key failed: " + err.Error()})
		return
	}
	body.Key = hash
	body.Enabled = true
	created := s.store.CreateAPIKey(tenant, &body)
	if created == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "create api key failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenant, UserID: caller.ID, Action: "apikey_create", Target: created.ID, Detail: sanitizeAuditDetail("name=" + created.Name),
	})
	// 返回创建结果 + 明文 key（仅此一次）。
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"apiKey":   created,
		"plainKey": plainKey,
	})
}

// handleAPIKeyRouting 分派 /api/v1/apikeys/{id} 子路径。
func (s *Server) handleAPIKeyRouting(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/apikeys/")
	if rest == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "api key id required"})
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "api key id required"})
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
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
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
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + action})
	}
}

// handleGetAPIKey 处理 GET /api/v1/apikeys/{id}。
func (s *Server) handleGetAPIKey(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "apikey:read"); !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	k, ok := s.store.GetAPIKey(tenant, id)
	if !ok || k == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "api key not found"})
		return
	}
	writeJSON(w, http.StatusOK, k)
}

// handleUpdateAPIKey 处理 PUT /api/v1/apikeys/{id}。
func (s *Server) handleUpdateAPIKey(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "apikey:write")
	if !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	var body store.APIKey
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	body.ID = id
	updated, ok := s.store.UpdateAPIKey(tenant, &body)
	if !ok || updated == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "api key not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenant, UserID: caller.ID, Action: "apikey_update", Target: id, Detail: sanitizeAuditDetail("name=" + updated.Name),
	})
	writeJSON(w, http.StatusOK, updated)
}

// handleDeleteAPIKey 处理 DELETE /api/v1/apikeys/{id}。
func (s *Server) handleDeleteAPIKey(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "apikey:write")
	if !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	if !s.store.DeleteAPIKey(tenant, id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "api key not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenant, UserID: caller.ID, Action: "apikey_delete", Target: id, Detail: "",
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleEnableAPIKey 处理 POST /api/v1/apikeys/{id}/enable。
func (s *Server) handleEnableAPIKey(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "apikey:write")
	if !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	k, ok := s.store.GetAPIKey(tenant, id)
	if !ok || k == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "api key not found"})
		return
	}
	k.Enabled = true
	updated, ok := s.store.UpdateAPIKey(tenant, k)
	if !ok || updated == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "enable api key failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenant, UserID: caller.ID, Action: "apikey_enable", Target: id, Detail: "",
	})
	writeJSON(w, http.StatusOK, updated)
}

// handleDisableAPIKey 处理 POST /api/v1/apikeys/{id}/disable。
func (s *Server) handleDisableAPIKey(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "apikey:write")
	if !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	k, ok := s.store.GetAPIKey(tenant, id)
	if !ok || k == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "api key not found"})
		return
	}
	k.Enabled = false
	updated, ok := s.store.UpdateAPIKey(tenant, k)
	if !ok || updated == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "disable api key failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenant, UserID: caller.ID, Action: "apikey_disable", Target: id, Detail: "",
	})
	writeJSON(w, http.StatusOK, updated)
}