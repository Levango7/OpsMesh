// device_metrics.go 处理 GET /api/v1/devices/{id}/metrics：返回设备最新监控指标。
//
// 监控指标由 agent 端采集（internal/agent/metrics_collect.go），经心跳上报到控制面，
// 控制面缓存最新值（store.StoreDeviceMetrics），此端点对外暴露。
// 历史时序数据由 Prometheus 负责，这里只返回最近一次采集结果。
package controlplane

import (
	"net/http"

)

// handleDeviceMetrics 处理 GET /api/v1/devices/{id}/metrics：返回设备最新监控指标。
// 租户隔离：requireAuth 时仅返回本租户设备的指标（经 Device 归属校验）。
// 无数据时返回 404（agent 未上报过指标，可能是刚注册尚未到首个 30s 采集周期）。
func (s *Server) handleDeviceMetrics(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	// 先校验设备存在 + 租户归属，避免泄露他租户设备指标。
	dev := s.store.Device(id)
	if dev == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}
	if actx.TenantID != "" && dev.TenantID != actx.TenantID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}
	metrics := s.store.DeviceMetrics(id)
	if metrics == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no metrics yet (agent may not have reported)"})
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}
