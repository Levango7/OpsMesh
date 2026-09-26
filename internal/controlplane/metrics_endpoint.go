// metrics_endpoint.go 实现 GET /metrics 的**应用侧数据装配**（Web 端口 8080 与 metrics 端口 9091 共用）。
//
// 输出内容 = internal/metrics 注册表的渲染结果，含三类：
//   - 运行时/HTTP/审计链/agent 验签等既有序列；
//   - 应用级仪表值（opsmesh_devices_total、opsmesh_device_status{status}、
//     opsmesh_alerts_active、opsmesh_tickets_open —— 见 internal/metrics/app_gauges.go）；
//   - 任务侧 opsmesh_tasks_total{status}（counter，由 IncTask 累加）与 opsmesh_task_queue_depth。
//
// 设计要点：
//   - **两个端口一份渲染**（2026-09-26）：以前 8080 自己手写 4 个 gauge、9091 走注册表，
//     而 prometheus.yml 只抓 9091 ⇒ 成批出厂告警/面板引用的序列在抓取面上不存在，且
//     opsmesh_tasks_total 同名在不同端口类型不同（gauge vs counter）。见 writeMetricsBody 注释。
//   - 数据来源仍是 store（Snapshot/AllTasks/Alerts/ListTickets/Agents）。
//   - 访问控制（P1-5）：8080 上该端点无鉴权且做全量扫描，故必须过 metricsAllowed（CIDR 白名单）；
//     生产模式未配白名单时 fail-closed 返回 403。抓取请指向 9091。
//   - 计数缓存（P1-6）：应用级计数走 TTL 缓存（--metrics-cache-ttl，默认 1s，0=关闭）。
//     此前每次抓取对 store 做 5 次全表读（8080 四次 + 9091 一次 Agents），
//     注意 9091 并非旧注释所称的"只做 O(1) 渲染"。
package controlplane

import (
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/metrics"
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
			for _, d := range devs {
				c.devices++
				// 只按显式状态计数：discovered（网段发现的候选）与 provisioning（推送中）
				// 既不算在线也不算离线——把它们算成离线会让 DeviceOffline 告警在纳管过程中误报。
				switch d.State {
				case "online":
					c.devicesOnline++
				case "offline":
					c.devicesOffline++
				}
			}
		}
		c.tasks = len(s.store.AllTasks(""))
		c.alerts = len(s.store.Alerts(""))
		c.ticketsOpen = len(s.store.ListTickets("", store.TicketFilter{Status: "open"}))
		c.agents = len(s.store.Agents(""))
		return c
	})
}

// writeMetricsBody 是 8080 与 9091 **共用的唯一渲染路径**。
//
// 为什么要唯一：此前两个端口各写一套（8080 手写 4 个 gauge、9091 走注册表），
// 而 prometheus.yml 只抓 9091 ⇒ 出厂规则与面板引用的一批序列在抓取面上不存在。
// 收敛成一个渲染器后，"两个端口指标集合不同"这类问题在结构上不再可能。
// 访问控制仍由各 handler 自己负责（8080 走 CIDR 准入，9091 独立监听）。
func (s *Server) writeMetricsBody(w http.ResponseWriter) {
	cnt := s.appMetricsCounts()
	reg := s.metrics
	if reg == nil {
		// 白盒测试里手工装配的 Server 没有注册表（生产路径由 New() 建好）。
		// 这里用**局部**注册表而不是给 s.metrics 赋值：后者会在并发抓取下构成数据竞争，
		// 而"为了测试能跑而在被测代码里埋一个 race"是更坏的取舍。
		reg = metrics.New()
	}
	reg.SetAgents(cnt.agents)
	reg.SetAppGauges(cnt.devices, cnt.devicesOnline, cnt.devicesOffline, cnt.alerts, cnt.ticketsOpen)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, reg.Render())
}

// handlePrometheusMetrics 处理 GET /metrics（Web 端口 8080）：输出与 9091 同一份注册表内容。
//
// 刻意不再自己手写 4 个 gauge：那正是"同名不同类型 / 抓取面上缺序列"的成因（见 writeMetricsBody）。
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
	s.writeMetricsBody(w)
}
