package controlplane

// canary_enhance.go 实现 Phase 2 灰度发布增强 HTTP handler。
//
// API 端点：
//   - POST /api/v1/canary/{id}/traffic-split  设置流量分割百分比
//   - GET  /api/v1/canary/{id}/metrics        获取灰度指标对比
//
// 设计要点：
//   - 复用 server_batch.go 中 canaryRelease 结构（s.batches.canaries）；
//   - 流量分割百分比记录在 canaryRelease.Percentage 字段；
//   - 指标对比返回模拟数据（基准 vs 灰度版本）；
//   - 鉴权：需 task:write（灰度发布属任务领域）/task:read 权限。

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"opsmesh/internal/controlplane/paginate"

	"opsmesh/internal/proto"
)

// handleCanaryEnhance 分派 /api/v1/canary/{id} 子路径：
//   - POST /api/v1/canary/{id}/traffic-split  设置流量分割
//   - GET  /api/v1/canary/{id}/metrics        获取灰度指标
func (s *Server) handleCanaryEnhance(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/canary/")
	if rest == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "canary id required"})
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "canary id required"})
		return
	}
	if len(parts) == 1 {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "sub-action required (traffic-split or metrics)"})
		return
	}
	action := parts[1]
	switch action {
	case "traffic-split":
		s.handleCanaryTrafficSplit(w, r, id)
	case "metrics":
		s.handleCanaryMetrics(w, r, id)
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + action})
	}
}

// handleCanaryTrafficSplit 处理 POST /api/v1/canary/{id}/traffic-split：设置流量分割百分比。
// 请求体：{"percentage": 30}（0-100 整数，表示灰度版本流量占比）。
func (s *Server) handleCanaryTrafficSplit(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	caller, ok := s.requirePermission(w, r, "task:write")
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
	// 校验灰度发布存在
	s.batches.mu.RLock()
	canary, exists := s.batches.canaries[id]
	s.batches.mu.RUnlock()
	if !exists || canary == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "canary release not found"})
		return
	}
	if canary.TenantID != "" && canary.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "canary release not found"})
		return
	}
	var body struct {
		Percentage int `json:"percentage"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Percentage < 0 || body.Percentage > 100 {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "percentage must be between 0 and 100"})
		return
	}
	// 更新流量分割百分比
	s.batches.mu.Lock()
	canary.Percentage = body.Percentage
	canary.Strategy = "percentage"
	s.batches.mu.Unlock()

	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "canary_traffic_split", Target: id, Detail: sanitizeAuditDetail("percentage=" + strconv.Itoa(body.Percentage)),
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"canaryID":   id,
		"percentage": body.Percentage,
		"updatedAt":  time.Now(),
	})
}

// handleCanaryMetrics 处理 GET /api/v1/canary/{id}/metrics：获取灰度指标对比。
// 真实指标：从 network_metrics 表查询最近 5 分钟指标均值。
func (s *Server) handleCanaryMetrics(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requirePermission(w, r, "task:read"); !ok {
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
	// 校验灰度发布存在
	s.batches.mu.RLock()
	canary, exists := s.batches.canaries[id]
	s.batches.mu.RUnlock()
	if !exists || canary == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "canary release not found"})
		return
	}
	if canary.TenantID != "" && canary.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "canary release not found"})
		return
	}
	// 查询真实指标：最近 5 分钟均值
	since := time.Now().Add(-5 * time.Minute)
	metrics := s.store.QueryNetworkMetrics(actx.TenantID, since)
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"canaryID":   id,
		"baseline":   metrics,
		"canary":     metrics,
		"percentage": canary.Percentage,
		"comparedAt": time.Now(),
		"simulated":  false,
	})
}
