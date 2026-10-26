package controlplane

// canary_enhance.go 实现 Phase 2 灰度发布增强 HTTP handler。
//
// API 端点：
//   - POST /api/v1/canary/{id}/traffic-split  设置流量分割百分比
//   - GET  /api/v1/canary/{id}/metrics        获取灰度指标对比
//   - 同名子路径也挂在 /api/v1/tasks/canary/ 前缀下（灰度本体所在的任务域）。
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

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// handleCanaryEnhance 分派灰度增强子路径：
//   - POST /api/v1/canary/{id}/traffic-split  设置流量分割
//   - GET  /api/v1/canary/{id}/metrics        获取灰度指标
//   - POST /api/v1/tasks/canary/{id}/traffic-split（同上，任务域前缀）
//   - GET  /api/v1/tasks/canary/{id}/metrics（同上，任务域前缀）
//
// 前缀兼容说明（2026-10-21）：本 handler 原本只认 "/api/v1/canary/" 前缀，
// 而灰度发布本体在 "/api/v1/tasks/canary/"（见 server_batch.go 的 handleCanaryRouting）。
// 两种前缀都真实存在调用方（前端 src/api/canary.js 用 /canary/…，
// 任务域路由用 /tasks/canary/…），故按前缀逐个剥离，解析出 {id} 与子动作。
func (s *Server) handleCanaryEnhance(w http.ResponseWriter, r *http.Request) {
	rest := r.URL.Path
	trimmed := false
	for _, prefix := range []string{"/api/v1/tasks/canary/", "/api/v1/canary/"} {
		if strings.HasPrefix(rest, prefix) {
			rest = strings.TrimPrefix(rest, prefix)
			trimmed = true
			break
		}
	}
	if !trimmed || rest == "" {
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
	// 鉴权对齐（2026-10-21）：改用 requireProd（与 handleCanaryCreate/Status/Advance 同闸）。
	// 此前用 requirePermission——它只认 JWT/Cookie，没有 demo 模式与网关注入（X-User-Roles）
	// 路径，导致同一批灰度接口在 demo 部署下「创建/查询能过、改分流/看指标恒 401」，
	// 与 FIX 1「让 traffic-split / metrics 真正可达」的目标相悖。权限值不变（task:write），
	// 生产态鉴权强度不变（无身份仍 401）。身份不落地为变量：审计 UserID 统一取 actx.UserID。
	if _, ok := s.requireProd(w, r, "task:write"); !ok {
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

	// UserID 取 actx.UserID（与 handleCanaryCreate/Status/Advance 的审计口径一致）。
	// 注：不能直接用 caller.ID——requireProd 的 demo 分支返回 (nil, true)，
	// 无 JWT 的 demo 部署下 caller 为 nil，caller.ID 会 panic（此路径在
	// 鉴权改 requireProd 前因恒 401 而不可达，属一并修掉的潜在崩溃点）。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "canary_traffic_split", Target: id, Detail: sanitizeAuditDetail("percentage=" + strconv.Itoa(body.Percentage)),
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
	// 鉴权对齐（2026-10-21）：同 handleCanaryTrafficSplit ——改 requireProd 兼容
	// demo 模式与网关注入身份（权限值仍为 task:read；无身份生产态仍 401）。
	if _, ok := s.requireProd(w, r, "task:read"); !ok {
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
