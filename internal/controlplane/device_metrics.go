// device_metrics.go 处理 GET /api/v1/devices/{id}/metrics：返回设备监控指标。
//
// 监控指标由 agent 端采集（internal/agent/metrics_collect.go），经心跳上报到控制面，
// 控制面环形缓冲保留最近 2h 历史快照（store.metricsRing）。
//
// 查询模式：
//   - 不带 range 参数：返回最新值（向后兼容现有行为）。
//   - ?range=2h：返回历史时序数据（proto.MetricsSeries），支持 15m/1h/2h/6h/24h。
//
// 历史时序数据由控制面环形缓冲提供（最近 2h/240 条），更长历史请查 Prometheus。
package controlplane

import (
	"net/http"
	"opsmesh/internal/controlplane/paginate"
	"strings"
	"time"

	"opsmesh/internal/proto"
)

// handleDeviceMetrics 处理 GET /api/v1/devices/{id}/metrics：返回设备监控指标。
// 租户隔离：requireAuth 时仅返回本租户设备的指标（经 Device 归属校验）。
// 无数据时返回 404（agent 未上报过指标，可能是刚注册尚未到首个 30s 采集周期）。
//
// 查询参数：
//   - 无 range：返回最新值（proto.DeviceMetrics），向后兼容。
//   - ?range=2h：返回历史时序（proto.MetricsSeries），range 支持 15m/1h/2h/6h/24h。
func (s *Server) handleDeviceMetrics(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "device:read"); !ok {
		return
	}
	// 先校验设备存在 + 租户归属，避免泄露他租户设备指标。
	dev := s.store.Device(id)
	if dev == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}
	if actx.TenantID != "" && dev.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}

	rangeStr := strings.TrimSpace(r.URL.Query().Get("range"))
	if rangeStr == "" {
		// 不带 range：保持现有行为，返回最新值。
		metrics := s.store.DeviceMetrics(id)
		if metrics == nil {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "no metrics yet (agent may not have reported)"})
			return
		}
		paginate.WriteJSON(w, http.StatusOK, metrics)
		return
	}

	// 带 range：返回历史时序数据。
	since, ok := parseMetricsRange(rangeStr)
	if !ok {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid range, supported: 15m, 1h, 2h, 6h, 24h",
		})
		return
	}
	samples := s.store.DeviceMetricsHistory(id, since)
	if len(samples) == 0 {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{
			"error": "no metrics history in range " + rangeStr + " (agent may not have reported or history expired)",
		})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, proto.MetricsSeries{
		DeviceID: id,
		Range:    rangeStr,
		Samples:  samples,
	})
}

// parseMetricsRange 解析 range 参数为查询起始时间（since = now - duration）。
// 支持 15m/1h/2h/6h/24h；不区分大小写。非法值返回 (zero, false)。
func parseMetricsRange(s string) (time.Time, bool) {
	now := time.Now()
	switch strings.ToLower(s) {
	case "15m":
		return now.Add(-15 * time.Minute), true
	case "1h":
		return now.Add(-1 * time.Hour), true
	case "2h":
		return now.Add(-2 * time.Hour), true
	case "6h":
		return now.Add(-6 * time.Hour), true
	case "24h":
		return now.Add(-24 * time.Hour), true
	}
	return time.Time{}, false
}
