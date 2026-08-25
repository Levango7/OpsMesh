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

	"opsmesh/internal/proto"
)

// handleCanaryEnhance 分派 /api/v1/canary/{id} 子路径：
//   - POST /api/v1/canary/{id}/traffic-split  设置流量分割
//   - GET  /api/v1/canary/{id}/metrics        获取灰度指标
func (s *Server) handleCanaryEnhance(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/canary/")
	if rest == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "canary id required"})
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "canary id required"})
		return
	}
	if len(parts) == 1 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "sub-action required (traffic-split or metrics)"})
		return
	}
	action := parts[1]
	switch action {
	case "traffic-split":
		s.handleCanaryTrafficSplit(w, r, id)
	case "metrics":
		s.handleCanaryMetrics(w, r, id)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + action})
	}
}

// handleCanaryTrafficSplit 处理 POST /api/v1/canary/{id}/traffic-split：设置流量分割百分比。
// 请求体：{"percentage": 30}（0-100 整数，表示灰度版本流量占比）。
func (s *Server) handleCanaryTrafficSplit(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	caller, ok := s.requirePermission(w, r, "task:write")
	if !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	// 校验灰度发布存在
	s.batches.mu.RLock()
	canary, exists := s.batches.canaries[id]
	s.batches.mu.RUnlock()
	if !exists || canary == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "canary release not found"})
		return
	}
	if canary.TenantID != "" && canary.TenantID != tenant {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "canary release not found"})
		return
	}
	var body struct {
		Percentage int `json:"percentage"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Percentage < 0 || body.Percentage > 100 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "percentage must be between 0 and 100"})
		return
	}
	// 更新流量分割百分比
	s.batches.mu.Lock()
	canary.Percentage = body.Percentage
	canary.Strategy = "percentage"
	s.batches.mu.Unlock()

	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenant, UserID: caller.ID, Action: "canary_traffic_split", Target: id, Detail: sanitizeAuditDetail("percentage=" + strconv.Itoa(body.Percentage)),
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"canaryID":   id,
		"percentage": body.Percentage,
		"updatedAt":  time.Now(),
	})
}

// handleCanaryMetrics 处理 GET /api/v1/canary/{id}/metrics：获取灰度指标对比。
// 返回模拟指标数据（基准版本 vs 灰度版本）。
func (s *Server) handleCanaryMetrics(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requirePermission(w, r, "task:read"); !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	// 校验灰度发布存在
	s.batches.mu.RLock()
	canary, exists := s.batches.canaries[id]
	s.batches.mu.RUnlock()
	if !exists || canary == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "canary release not found"})
		return
	}
	if canary.TenantID != "" && canary.TenantID != tenant {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "canary release not found"})
		return
	}
	// 返回模拟指标对比数据
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"canaryID": id,
		"baseline": map[string]float64{
			"errorRate":    0.5,
			"p99LatencyMs": 120.0,
			"qps":          1000.0,
			"successRate":  99.5,
		},
		"canary": map[string]float64{
			"errorRate":    0.3,
			"p99LatencyMs": 115.0,
			"qps":          300.0,
			"successRate":  99.7,
		},
		"percentage": canary.Percentage,
		"comparedAt": time.Now(),
	})
}
