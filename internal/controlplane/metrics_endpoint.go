
// metrics_endpoint.go 实现 Prometheus metrics 端点（GET /metrics）。
//
// 输出 Prometheus text exposition format，包含以下指标：
//   - opsmesh_devices_total：注册设备总数（gauge）
//   - opsmesh_tasks_total：任务总数（gauge）
//   - opsmesh_alerts_active：活跃告警数（gauge）
//   - opsmesh_tickets_open：开放工单数（gauge）
//
// 设计要点：
//   - 从 s.store 获取数据（Snapshot/AllTasks/Alerts/ListTickets）；
//   - 不需要鉴权（Prometheus 抓取通常经网络策略限制访问）；
//   - 输出 Prometheus text exposition format（Content-Type: text/plain; version=0.0.4）。
package controlplane

import (
	"fmt"
	"net/http"

	"opsmesh/internal/store"
)

// handlePrometheusMetrics 处理 GET /metrics：输出 Prometheus text exposition format。
func (s *Server) handlePrometheusMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	// 从 store 获取数据（空租户=全部租户）。
	devices := s.store.Snapshot("")
	var deviceCount int
	for _, devs := range devices {
		deviceCount += len(devs)
	}
	tasks := s.store.AllTasks("")
	taskCount := len(tasks)
	alerts := s.store.Alerts("")
	alertCount := len(alerts)
	tickets := s.store.ListTickets("", store.TicketFilter{Status: "open"})
	ticketOpenCount := len(tickets)

	// 输出 Prometheus text exposition format。
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "# HELP opsmesh_devices_total Total number of registered devices\n")
	fmt.Fprintf(w, "# TYPE opsmesh_devices_total gauge\n")
	fmt.Fprintf(w, "opsmesh_devices_total %d\n", deviceCount)
	fmt.Fprintf(w, "# HELP opsmesh_tasks_total Total number of tasks\n")
	fmt.Fprintf(w, "# TYPE opsmesh_tasks_total gauge\n")
	fmt.Fprintf(w, "opsmesh_tasks_total %d\n", taskCount)
	fmt.Fprintf(w, "# HELP opsmesh_alerts_active Active alerts count\n")
	fmt.Fprintf(w, "# TYPE opsmesh_alerts_active gauge\n")
	fmt.Fprintf(w, "opsmesh_alerts_active %d\n", alertCount)
	fmt.Fprintf(w, "# HELP opsmesh_tickets_open Open tickets count\n")
	fmt.Fprintf(w, "# TYPE opsmesh_tickets_open gauge\n")
	fmt.Fprintf(w, "opsmesh_tickets_open %d\n", ticketOpenCount)
}