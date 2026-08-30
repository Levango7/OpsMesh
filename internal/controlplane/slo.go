// slo.go 实现 Phase 1 SLO 管理 HTTP handler。
//
// API 端点：
//   - GET    /api/v1/slos           列出 SLO
//   - POST   /api/v1/slos           创建 SLO {name, description, serviceName, target, window, slis}
//   - GET    /api/v1/slos/{id}      获取 SLO 详情
//   - PUT    /api/v1/slos/{id}      更新 SLO
//   - DELETE /api/v1/slos/{id}      删除 SLO
//   - GET    /api/v1/slos/{id}/status 获取 SLI 状态
//
// 设计要点（与 ticket.go 风格一致）：
//   - 用 s.requireTenantContext(w, r) 提取租户（复用现有方法，统一租户隔离行为）；
//   - 错误响应统一 {"error": "message"} 格式，HTTP 状态码 400/404/500；
//   - 用 decodeJSONBody 解析请求体（防 DoS 限制大小）；
//   - 鉴权：需 slo:read/slo:write/slo:delete 权限（与现有 RBAC 一致）。
package controlplane

import (
	"net/http"
	"strings"

	"opsmesh/internal/controlplane/paginate"

	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// handleSLOs 统一处理 /api/v1/slos：
//   - GET：列出 SLO
//   - POST：创建 SLO
func (s *Server) handleSLOs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListSLOs(w, r)
	case http.MethodPost:
		s.handleCreateSLO(w, r)
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListSLOs 处理 GET /api/v1/slos：列出 SLO。
func (s *Server) handleListSLOs(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "slo:read"); !ok {
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
	slos := s.store.ListSLOs(actx.TenantID)
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"slos": slos})
}

// handleCreateSLO 处理 POST /api/v1/slos：创建 SLO。
// 请求体：{name, description, serviceName, target, window, slis}；name 必填。
func (s *Server) handleCreateSLO(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "slo:write")
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
	var body struct {
		Name        string      `json:"name"`
		Description string      `json:"description"`
		ServiceName string      `json:"serviceName"`
		Target      float64     `json:"target"`
		Window      string      `json:"window"`
		SLIs        []store.SLI `json:"slis"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Name == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	slo := &store.SLO{
		Name:        body.Name,
		Description: body.Description,
		ServiceName: body.ServiceName,
		Target:      body.Target,
		Window:      body.Window,
		SLIs:        body.SLIs,
	}
	created := s.store.CreateSLO(actx.TenantID, slo)
	if created == nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "create SLO failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "slo_create", Target: created.ID, Detail: sanitizeAuditDetail("name=" + created.Name),
	})
	paginate.WriteJSON(w, http.StatusCreated, created)
}

// handleSLORouting 分派 /api/v1/slos/{id} 子路径：
//   - GET    /api/v1/slos/{id}        获取 SLO 详情
//   - PUT    /api/v1/slos/{id}        更新 SLO
//   - DELETE /api/v1/slos/{id}        删除 SLO
//   - GET    /api/v1/slos/{id}/status 获取 SLI 状态
func (s *Server) handleSLORouting(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/slos/")
	if rest == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "slo id required"})
		return
	}
	// 按 / 切分：[id] / [id, action]。
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "slo id required"})
		return
	}
	// 仅 /{id}：SLO 本身管理（GET/PUT/DELETE）。
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			s.handleGetSLO(w, r, id)
		case http.MethodPut:
			s.handleUpdateSLO(w, r, id)
		case http.MethodDelete:
			s.handleDeleteSLO(w, r, id)
		default:
			paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}
	// /{id}/{action}。
	action := parts[1]
	switch action {
	case "status":
		s.handleSLOStatus(w, r, id)
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + action})
	}
}

// handleGetSLO 处理 GET /api/v1/slos/{id}：获取 SLO 详情。
func (s *Server) handleGetSLO(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "slo:read"); !ok {
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
	slo, ok := s.store.GetSLO(actx.TenantID, id)
	if !ok || slo == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "slo not found"})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, slo)
}

// handleUpdateSLO 处理 PUT /api/v1/slos/{id}：更新 SLO。
// 请求体：{name, description, serviceName, target, window, slis}。
func (s *Server) handleUpdateSLO(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "slo:write")
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
	var body struct {
		Name        string      `json:"name"`
		Description string      `json:"description"`
		ServiceName string      `json:"serviceName"`
		Target      float64     `json:"target"`
		Window      string      `json:"window"`
		SLIs        []store.SLI `json:"slis"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	slo := &store.SLO{
		ID:          id,
		Name:        body.Name,
		Description: body.Description,
		ServiceName: body.ServiceName,
		Target:      body.Target,
		Window:      body.Window,
		SLIs:        body.SLIs,
	}
	updated, ok := s.store.UpdateSLO(actx.TenantID, slo)
	if !ok || updated == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "slo not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "slo_update", Target: id, Detail: sanitizeAuditDetail("name=" + updated.Name),
	})
	paginate.WriteJSON(w, http.StatusOK, updated)
}

// handleDeleteSLO 处理 DELETE /api/v1/slos/{id}：删除 SLO（返回 204）。
func (s *Server) handleDeleteSLO(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "slo:delete")
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
	if !s.store.DeleteSLO(actx.TenantID, id) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "slo not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "slo_delete", Target: id,
	})
	w.WriteHeader(http.StatusNoContent)
}

// handleSLOStatus 处理 GET /api/v1/slos/{id}/status：获取 SLI 状态。
func (s *Server) handleSLOStatus(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requirePermission(w, r, "slo:read"); !ok {
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
	// 先校验 SLO 存在（不存在返回 404，与 ticket close 风格一致）。
	slo, ok := s.store.GetSLO(actx.TenantID, id)
	if !ok || slo == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "slo not found"})
		return
	}
	statuses := s.store.SLIStatus(actx.TenantID, id)
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"statuses": statuses})
}
