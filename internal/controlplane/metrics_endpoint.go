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
//   - 访问控制（P1-5）：本端点位于对外 Web 端口（8080）且无鉴权
//     且无鉴权，故必须经 metricsAllowed（CIDR 白名单）准入；生产模式未配置白名单时 fail-closed
//     返回 403。Prometheus 抓取应指向独立 metrics 端口 9091。
//   - 计数缓存（P1-6）：应用级计数经 appMetricsCounts 走 TTL 缓存（--metrics-cache-ttl，
//     默认 1s，0=关闭）。此前每次抓取会对 store 做 4 次（8080）+1 次（9091 SetAgents）
//     全量扫描——注意 9091 并非旧注释所称的"只做 O(1) 渲染"。
//   - 输出 Prometheus text exposition format（Content-Type: text/plain; version=0.0.4）。
package controlplane

import (
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/store"
)

// appMetricsCounts 返回 /metrics 用到的应用级计数（TTL 内复用，见 appCountsCache）。
//
// 这 5 个计数原先每次抓取各扫一遍 store（Snapshot/AllTasks/Alerts/ListTickets/Agents）。
// 8080 与 9091 两个端口都渲染指标，等于同一份数据被全量扫两遍；缓存把它们收敛成一次。
func (s *Server) appMetricsCounts() appCounts {
	return s.metricsCounts.resolve(func() appCounts {
		var c appCounts
		for _, devs := range s.store.Snapshot("") {
			c.devices += len(devs)
		}
		c.tasks = len(s.store.AllTasks(""))
		c.alerts = len(s.store.Alerts(""))
		c.ticketsOpen = len(s.store.ListTickets("", store.TicketFilter{Status: "open"}))
		c.agents = len(s.store.Agents(""))
		return c
	})
}

// handlePrometheusMetrics 处理 GET /metrics：输出 Prometheus text exposition format。
func (s *Server) handlePrometheusMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	// 准入控制：本端点无鉴权且做全量 store 扫描，未授权来源可直接放大为控制面 CPU/DB 压力。
	if !s.metricsAllowed(r.RemoteAddr) {
		logx.Warn(r.Context(), "metrics(8080) 访问被拒",
			"remote", r.RemoteAddr, "hint", "配置 --metrics-allow-cidr 或改用独立 metrics 端口 9091")
		paginate.WriteJSON(w, http.StatusForbidden,
			map[string]string{"error": "metrics access denied", "hint": "配置 --metrics-allow-cidr 白名单（生产模式必填）；监控抓取建议使用 9091 端口"})
		return
	}
	cnt := s.appMetricsCounts()
	deviceCount, taskCount, alertCount, ticketOpenCount := cnt.devices, cnt.tasks, cnt.alerts, cnt.ticketsOpen

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
