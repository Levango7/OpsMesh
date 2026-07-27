// Package metrics 提供零依赖的可观测指标（计数器/直方图/仪表盘），
// 以 Prometheus 文本格式暴露于控制面 metrics 端口（P2-1）。
// 刻意不引入 prometheus 客户端，避免 go.sum 负担（沙箱无 Go，默认构建须干净）。
package metrics

import (
	"fmt"
	"sort"
	"sync"
)

// M 是线程安全的指标注册表。
type M struct {
	mu     sync.Mutex
	agents int64
	tasks  map[string]int64 // status -> count
	depth  int64            // 待执行队列深度（pending 任务数）
	durN   int64
	durSum float64
	durMax float64
}

// New 构造空指标注册表。
func New() *M {
	return &M{tasks: make(map[string]int64)}
}

// SetAgents 设置 agent 总数（仪表盘指标）。
func (m *M) SetAgents(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.agents = int64(n)
}

// IncTask 累加某状态任务数（done/failed/...）。
func (m *M) IncTask(status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[status]++
}

// SetQueueDepth 设置待执行队列深度（pending 任务数）。
func (m *M) SetQueueDepth(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.depth = int64(n)
}

// ObserveDuration 记录一次任务执行耗时（秒），更新概要（计数/总和/最大）。
func (m *M) ObserveDuration(seconds float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.durN++
	m.durSum += seconds
	if seconds > m.durMax {
		m.durMax = seconds
	}
}

// Render 以 Prometheus 文本格式输出所有指标。
func (m *M) Render() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var b []byte
	b = append(b, "# HELP opsmesh_agents_total 已注册 agent 数\n"...)
	b = append(b, "# TYPE opsmesh_agents_total gauge\n"...)
	b = append(b, fmt.Sprintf("opsmesh_agents_total %d\n", m.agents)...)

	b = append(b, "# HELP opsmesh_tasks_total 任务总数（按状态）\n"...)
	b = append(b, "# TYPE opsmesh_tasks_total counter\n"...)
	statuses := make([]string, 0, len(m.tasks))
	for s := range m.tasks {
		statuses = append(statuses, s)
	}
	sort.Strings(statuses)
	for _, s := range statuses {
		b = append(b, fmt.Sprintf("opsmesh_tasks_total{status=%q} %d\n", s, m.tasks[s])...)
	}

	b = append(b, "# HELP opsmesh_task_queue_depth 待执行任务深度\n"...)
	b = append(b, "# TYPE opsmesh_task_queue_depth gauge\n"...)
	b = append(b, fmt.Sprintf("opsmesh_task_queue_depth %d\n", m.depth)...)

	b = append(b, "# HELP opsmesh_task_duration_seconds_sum 任务执行耗时总和（秒）\n"...)
	b = append(b, "# TYPE opsmesh_task_duration_seconds_sum gauge\n"...)
	b = append(b, fmt.Sprintf("opsmesh_task_duration_seconds_sum %f\n", m.durSum)...)
	b = append(b, "# HELP opsmesh_task_duration_seconds_count 任务执行次数\n"...)
	b = append(b, "# TYPE opsmesh_task_duration_seconds_count gauge\n"...)
	b = append(b, fmt.Sprintf("opsmesh_task_duration_seconds_count %d\n", m.durN)...)
	b = append(b, "# HELP opsmesh_task_duration_seconds_max 任务执行最大耗时（秒）\n"...)
	b = append(b, "# TYPE opsmesh_task_duration_seconds_max gauge\n"...)
	b = append(b, fmt.Sprintf("opsmesh_task_duration_seconds_max %f\n", m.durMax)...)
	return string(b)
}
