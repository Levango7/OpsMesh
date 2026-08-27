package controlplane

// traffic.go 实现 Phase 2 流量治理 HTTP handler。
//
// API 端点：
//   - GET    /api/v1/traffic/policies           列出流量策略
//   - POST   /api/v1/traffic/policies           创建流量策略
//   - GET    /api/v1/traffic/policies/{id}      获取策略详情
//   - PUT    /api/v1/traffic/policies/{id}      更新策略
//   - DELETE /api/v1/traffic/policies/{id}      删除策略
//   - POST   /api/v1/traffic/policies/{id}/enable  启用策略
//   - POST   /api/v1/traffic/policies/{id}/disable 禁用策略
//
// 设计要点（与 ticket.go 风格一致）：
//   - 用 s.requireTenantContext(w, r) 提取租户；
//   - 错误响应统一 {"error": "message"} 格式；
//   - 用 decodeJSONBody 解析请求体；
//   - 鉴权：需 traffic:read/traffic:write 权限。

import (
	"opsmesh/internal/controlplane/paginate"
	"net/http"
	"strings"

	"opsmesh/internal/store"
)

// handleTrafficPolicies 统一处理 /api/v1/traffic/policies：
//   - GET：列出流量策略
//   - POST：创建流量策略
func (s *Server) handleTrafficPolicies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListTrafficPolicies(w, r)
	case http.MethodPost:
		s.handleCreateTrafficPolicy(w, r)
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListTrafficPolicies 处理 GET /api/v1/traffic/policies：列出策略。
func (s *Server) handleListTrafficPolicies(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "traffic:read"); !ok {
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
	policies := s.store.ListPolicies(actx.TenantID)
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"policies": policies})
}

// handleCreateTrafficPolicy 处理 POST /api/v1/traffic/policies：创建策略。
func (s *Server) handleCreateTrafficPolicy(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "traffic:write")
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
	var body store.TrafficPolicy
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Name == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	created := s.store.CreatePolicy(actx.TenantID, &body)
	if created == nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "create policy failed"})
		return
	}
	// 审计：记录创建人
	_ = caller
	paginate.WriteJSON(w, http.StatusCreated, created)
}

// handleTrafficPolicyRouting 分派 /api/v1/traffic/policies/{id} 子路径：
//   - GET    /api/v1/traffic/policies/{id}        获取策略详情
//   - PUT    /api/v1/traffic/policies/{id}        更新策略
//   - DELETE /api/v1/traffic/policies/{id}        删除策略
//   - POST   /api/v1/traffic/policies/{id}/enable  启用
//   - POST   /api/v1/traffic/policies/{id}/disable 禁用
func (s *Server) handleTrafficPolicyRouting(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/traffic/policies/")
	if rest == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "policy id required"})
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "policy id required"})
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			s.handleGetTrafficPolicy(w, r, id)
		case http.MethodPut:
			s.handleUpdateTrafficPolicy(w, r, id)
		case http.MethodDelete:
			s.handleDeleteTrafficPolicy(w, r, id)
		default:
			paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}
	action := parts[1]
	switch action {
	case "enable":
		s.handleEnableTrafficPolicy(w, r, id)
	case "disable":
		s.handleDisableTrafficPolicy(w, r, id)
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + action})
	}
}

// handleGetTrafficPolicy 处理 GET /api/v1/traffic/policies/{id}：获取详情。
func (s *Server) handleGetTrafficPolicy(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "traffic:read"); !ok {
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
	p, ok := s.store.GetPolicy(actx.TenantID, id)
	if !ok || p == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "policy not found"})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, p)
}

// handleUpdateTrafficPolicy 处理 PUT /api/v1/traffic/policies/{id}：更新策略。
func (s *Server) handleUpdateTrafficPolicy(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "traffic:write"); !ok {
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
	var body store.TrafficPolicy
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	body.ID = id
	updated, ok := s.store.UpdatePolicy(actx.TenantID, &body)
	if !ok || updated == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "policy not found"})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, updated)
}

// handleDeleteTrafficPolicy 处理 DELETE /api/v1/traffic/policies/{id}：删除策略。
func (s *Server) handleDeleteTrafficPolicy(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "traffic:write"); !ok {
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
	if !s.store.DeletePolicy(actx.TenantID, id) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "policy not found"})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleEnableTrafficPolicy 处理 POST /api/v1/traffic/policies/{id}/enable：启用策略。
func (s *Server) handleEnableTrafficPolicy(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requirePermission(w, r, "traffic:write"); !ok {
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
	p, ok := s.store.EnablePolicy(actx.TenantID, id)
	if !ok || p == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "policy not found"})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, p)
}

// handleDisableTrafficPolicy 处理 POST /api/v1/traffic/policies/{id}/disable：禁用策略。
func (s *Server) handleDisableTrafficPolicy(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requirePermission(w, r, "traffic:write"); !ok {
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
	p, ok := s.store.DisablePolicy(actx.TenantID, id)
	if !ok || p == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "policy not found"})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, p)
}

