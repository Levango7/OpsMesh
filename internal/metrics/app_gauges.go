// app_gauges.go — 应用级仪表值（快照 gauge）的集中定义与渲染。
//
// 为什么单列一个文件并统一两个端口（2026-09-26 实测修复）：
// 此前 GET /metrics 有两套互不一致的输出——
//   - Web 端口 8080：手写的 4 个未带标签的 gauge（devices/tasks/alerts/tickets）；
//   - metrics 端口 9091：internal/metrics 注册表（go_*/process_*/opsmesh_http_*/*_audit_chain_*…）。
//
// 而 deploy/monitoring/prometheus.yml **只抓 9091**。后果是出厂监控资产里成批的规则与面板
// 引用了"抓取面上根本不存在的序列"，因此**永远不会触发/永远空白**，而 CI 与本地起栈都不会
// 报错——因为"没有数据"在 Prometheus 里是合法状态。实测（本机独立实例 + 真抓 9091）：
//
//	process_cpu_seconds_total            命中 0   ← HighCPUUsage 告警 + 仪表盘 CPU 面板
//	opsmesh_device_status                命中 0   ← DeviceOffline 告警
//	opsmesh_tasks_failed_total           命中 0   ← TaskExecutionFailed 告警（真名是 opsmesh_tasks_total{status="failed"}）
//	http_requests_total                  命中 0   ← HighErrorRate（真名带 opsmesh_ 前缀）
//	http_request_duration_seconds_bucket 命中 0   ← HighLatency（同上）
//
// 另有一处同名不同类型的隐患：opsmesh_tasks_total 在 8080 是"库内任务总数"的 gauge，
// 在 9091 是"按状态累加的 counter"。若两个端口都被抓进同一个 Prometheus，就是同名异构冲突。
//
// 处置原则：
//  1. **一个渲染器、两个端口**——8080 与 9091 输出同一份注册表内容（8080 仍受 CIDR 准入约束），
//     "两个端口返回不同集合"这一类问题从此结构上不可能再出现；
//  2. 规则/面板需要的序列**补进注册表**（本文件的设备状态细分、下面的 process CPU），
//     而不是把规则改成"能跑但没意义"的近似式；
//  3. 8080 原来那个未带标签的 opsmesh_tasks_total gauge 不再输出：它的信息已由
//     opsmesh_tasks_total{status=...}（counter）与 opsmesh_task_queue_depth 覆盖，
//     保留只会造成同名异构。
package metrics

import "fmt"

// 设备状态标签值：与 store 侧 Device.State 的取值集合一致（memory/sql 都只写这两个值）。
// 刻意在此固定枚举：Prometheus 的约定是"0 值也要输出"，否则告警在冷启动阶段无序列可比。
const (
	deviceStatusOnline  = "online"
	deviceStatusOffline = "offline"
)

// SetAppGauges 写入应用级仪表值。与 SetAgents 同性质：快照赋值，不是累加。
//
// 参数语义：devices 为已纳管设备总数，devicesOnline/Offline 为其按连接状态的细分
// （两者之和不必等于 devices——未知状态设备存在，此时差额只体现在 total 上，
// 这是刻意的：total 保持"总数"含义，不为了凑数把 unknown 塞进 offline 制造假告警）。
func (m *M) SetAppGauges(devices, devicesOnline, devicesOffline, alertsActive, ticketsOpen int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.devices = int64(devices)
	m.devicesOnline = int64(devicesOnline)
	m.devicesOffline = int64(devicesOffline)
	m.alertsActive = int64(alertsActive)
	m.ticketsOpen = int64(ticketsOpen)
}

// appendAppGaugeMetrics 输出应用级仪表值。调用方已持锁。
func (m *M) appendAppGaugeMetrics(b []byte) []byte {
	b = append(b, "# HELP opsmesh_devices_total 已纳管设备总数\n"...)
	b = append(b, "# TYPE opsmesh_devices_total gauge\n"...)
	b = append(b, fmt.Sprintf("opsmesh_devices_total %d\n", m.devices)...)

	// DeviceOffline 告警依赖带 status 标签的序列（此前全仓不存在该名字，告警恒不触发）。
	b = append(b, "# HELP opsmesh_device_status 设备数（按连接状态细分；0 值也输出，避免冷启动无序列）\n"...)
	b = append(b, "# TYPE opsmesh_device_status gauge\n"...)
	b = append(b, fmt.Sprintf("opsmesh_device_status{status=%q} %d\n", deviceStatusOnline, m.devicesOnline)...)
	b = append(b, fmt.Sprintf("opsmesh_device_status{status=%q} %d\n", deviceStatusOffline, m.devicesOffline)...)

	b = append(b, "# HELP opsmesh_alerts_active 活跃告警数\n"...)
	b = append(b, "# TYPE opsmesh_alerts_active gauge\n"...)
	b = append(b, fmt.Sprintf("opsmesh_alerts_active %d\n", m.alertsActive)...)

	b = append(b, "# HELP opsmesh_tickets_open 未关闭工单数\n"...)
	b = append(b, "# TYPE opsmesh_tickets_open gauge\n"...)
	b = append(b, fmt.Sprintf("opsmesh_tickets_open %d\n", m.ticketsOpen)...)
	return b
}
