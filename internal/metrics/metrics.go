// Package metrics 提供零依赖的可观测指标（计数器/直方图/仪表盘），
// 以 Prometheus 文本格式暴露于控制面 metrics 端口。
// 刻意不引入 prometheus 客户端，避免 go.sum 负担（沙箱无 Go，默认构建须干净）。
//
// 扩充：
//   - HTTP 请求延迟直方图（opsmesh_http_request_duration_seconds_bucket/sum/count）
//   - HTTP 请求计数器（opsmesh_http_requests_total）
//   - Go runtime 指标（go_goroutines / go_memstats_* / go_gc_duration_seconds / process_start_time_seconds）
//
// 所有新增指标沿用零依赖手写风格，桶边界与 prometheus.DefBuckets 一致，
// runtime 指标在 Render 时实时采集 runtime.MemStats，无需外部 collector 注册。
package metrics

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// defBuckets 与 prometheus.DefBuckets 一致（秒）。
// 用于 HTTP 请求延迟直方图桶边界。
var defBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// processStartTime 进程启动时间（Unix 秒），用于 process_start_time_seconds 指标。
// 包初始化时采集一次，等价于 prometheus process collector 的 start_time。
var processStartTime = float64(time.Now().UnixNano()) / 1e9

// httpHistStats 单个 (method,path,status) 维度的直方图统计。
// bucketCounts[i] 表示落在 (defBuckets[i-1], defBuckets[i]] 区间内的观测数；
// bucketCounts[len(defBuckets)] 为 +Inf 桶（> 最大有限桶上界）。
// 渲染时按 Prometheus 累积桶语义输出（bucket{le=bi} = sum(counts[0..i])）。
type httpHistStats struct {
	bucketCounts []uint64 // len == len(defBuckets)+1，末位为 +Inf 桶
	sum          float64
	count        uint64
}

// M 是线程安全的指标注册表。
type M struct {
	mu     sync.Mutex
	agents int64
	tasks  map[string]int64 // status -> count
	depth  int64            // 待执行队列深度（pending 任务数）
	durN   int64
	durSum float64
	durMax float64

	// HTTP 指标。
	// httpReqs: (method|path|status) -> count；httpHist: 同 key -> 直方图统计。
	// key 形如 "GET|/api/v1/devices|200"，避免高基数（路径已归一化）。
	httpReqs map[string]uint64
	httpHist map[string]*httpHistStats
}

// New 构造空指标注册表。
func New() *M {
	return &M{
		tasks:    make(map[string]int64),
		httpReqs: make(map[string]uint64),
		httpHist: make(map[string]*httpHistStats),
	}
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

// httpKey 构造 HTTP 指标维度键（method|path|status）。
func httpKey(method, path, status string) string {
	return method + "|" + path + "|" + status
}

// IncHTTPRequest 累加 HTTP 请求计数。
// method/path/status 由中间件归一化后传入（路径已替换数字 ID 为 :id）。
func (m *M) IncHTTPRequest(method, path, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.httpReqs[httpKey(method, path, status)]++
}

// ObserveHTTPRequestDuration 记录一次 HTTP 请求耗时（秒），更新直方图桶。
// 桶边界与 prometheus.DefBuckets 一致；耗时 > 最大桶上界落入 +Inf 桶。
func (m *M) ObserveHTTPRequestDuration(method, path, status string, seconds float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := httpKey(method, path, status)
	h, ok := m.httpHist[k]
	if !ok {
		h = &httpHistStats{bucketCounts: make([]uint64, len(defBuckets)+1)}
		m.httpHist[k] = h
	}
	// 找到第一个 bucket >= seconds，落入该桶（累积语义在 Render 时展开）。
	idx := len(defBuckets) // 默认 +Inf 桶
	for i, b := range defBuckets {
		if seconds <= b {
			idx = i
			break
		}
	}
	h.bucketCounts[idx]++
	h.sum += seconds
	h.count++
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

	b = m.appendHTTPMetrics(b)
	b = m.appendRuntimeMetrics(b)
	return string(b)
}

// appendHTTPMetrics 输出 HTTP 请求计数器与延迟直方图。
// 调用方已持锁，无需再锁。
func (m *M) appendHTTPMetrics(b []byte) []byte {
	// 1. HTTP 请求计数器（按 key 排序保证输出稳定，便于测试断言）。
	b = append(b, "# HELP opsmesh_http_requests_total HTTP 请求总数（按方法/路径/状态）\n"...)
	b = append(b, "# TYPE opsmesh_http_requests_total counter\n"...)
	reqKeys := make([]string, 0, len(m.httpReqs))
	for k := range m.httpReqs {
		reqKeys = append(reqKeys, k)
	}
	sort.Strings(reqKeys)
	for _, k := range reqKeys {
		method, path, status := splitHTTPKey(k)
		b = append(b, fmt.Sprintf("opsmesh_http_requests_total{method=%q,path=%q,status=%q} %d\n",
			method, path, status, m.httpReqs[k])...)
	}

	// 2. HTTP 请求延迟直方图（累积桶语义）。
	b = append(b, "# HELP opsmesh_http_request_duration_seconds HTTP 请求延迟（秒）\n"...)
	b = append(b, "# TYPE opsmesh_http_request_duration_seconds histogram\n"...)
	histKeys := make([]string, 0, len(m.httpHist))
	for k := range m.httpHist {
		histKeys = append(histKeys, k)
	}
	sort.Strings(histKeys)
	for _, k := range histKeys {
		method, path, status := splitHTTPKey(k)
		h := m.httpHist[k]
		// 累积桶：bucket{le=bi} = sum(bucketCounts[0..i])，最后 +Inf 桶 = count。
		var cumulative uint64
		for i, le := range defBuckets {
			cumulative += h.bucketCounts[i]
			b = append(b, fmt.Sprintf("opsmesh_http_request_duration_seconds_bucket{method=%q,path=%q,status=%q,le=%q} %d\n",
				method, path, status, formatBucketLabel(le), cumulative)...)
		}
		// +Inf 桶（必须输出，Prometheus histogram 协议要求）。
		cumulative += h.bucketCounts[len(defBuckets)]
		b = append(b, fmt.Sprintf("opsmesh_http_request_duration_seconds_bucket{method=%q,path=%q,status=%q,le=\"+Inf\"} %d\n",
			method, path, status, cumulative)...)
		b = append(b, fmt.Sprintf("opsmesh_http_request_duration_seconds_sum{method=%q,path=%q,status=%q} %f\n",
			method, path, status, h.sum)...)
		b = append(b, fmt.Sprintf("opsmesh_http_request_duration_seconds_count{method=%q,path=%q,status=%q} %d\n",
			method, path, status, h.count)...)
	}
	return b
}

// splitHTTPKey 拆分 "method|path|status" 键回三元组。
// path 自身可能含 '|' 极罕见（归一化后不会），此处按最后两个 '|' 切分以稳健处理。
func splitHTTPKey(k string) (method, path, status string) {
	// 格式固定为 method|path|status，path 不含 '|'（归一化后为 /api/v1/.../:id）。
	first := strings.IndexByte(k, '|')
	if first < 0 {
		return k, "", ""
	}
	last := strings.LastIndexByte(k, '|')
	if last == first {
		return k[:first], k[first+1:], ""
	}
	return k[:first], k[first+1 : last], k[last+1:]
}

// formatBucketLabel 格式化桶上界为 Prometheus 标签值（去尾零，与 client_golang 一致）。
func formatBucketLabel(le float64) string {
	return strconv.FormatFloat(le, 'g', -1, 64)
}

// appendRuntimeMetrics 输出 Go runtime 与 process 指标。
// 等价于 prometheus.NewGoCollector() + NewProcessCollector()，但零依赖：
// 实时采集 runtime.MemStats + runtime.NumGoroutine + os.Getpid。
// 调用方已持锁，无需再锁。
func (m *M) appendRuntimeMetrics(b []byte) []byte {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	nGo := runtime.NumGoroutine()

	// --- go_* 指标（等价 prometheus GoCollector 默认集） ---
	b = append(b, "# HELP go_goroutines 当前 goroutine 数\n"...)
	b = append(b, "# TYPE go_goroutines gauge\n"...)
	b = append(b, fmt.Sprintf("go_goroutines %d\n", nGo)...)

	b = append(b, "# HELP go_memstats_alloc_bytes 已分配且仍在使用的字节数\n"...)
	b = append(b, "# TYPE go_memstats_alloc_bytes gauge\n"...)
	b = append(b, fmt.Sprintf("go_memstats_alloc_bytes %d\n", ms.Alloc)...)

	b = append(b, "# HELP go_memstats_sys_bytes 从 OS 获取的总内存（字节）\n"...)
	b = append(b, "# TYPE go_memstats_sys_bytes gauge\n"...)
	b = append(b, fmt.Sprintf("go_memstats_sys_bytes %d\n", ms.Sys)...)

	b = append(b, "# HELP go_memstats_heap_alloc_bytes 堆已分配字节数\n"...)
	b = append(b, "# TYPE go_memstats_heap_alloc_bytes gauge\n"...)
	b = append(b, fmt.Sprintf("go_memstats_heap_alloc_bytes %d\n", ms.HeapAlloc)...)

	b = append(b, "# HELP go_memstats_heap_inuse_bytes 堆在用字节数\n"...)
	b = append(b, "# TYPE go_memstats_heap_inuse_bytes gauge\n"...)
	b = append(b, fmt.Sprintf("go_memstats_heap_inuse_bytes %d\n", ms.HeapInuse)...)

	b = append(b, "# HELP go_memstats_next_gc_bytes 下次 GC 目标堆大小（字节）\n"...)
	b = append(b, "# TYPE go_memstats_next_gc_bytes gauge\n"...)
	b = append(b, fmt.Sprintf("go_memstats_next_gc_bytes %d\n", ms.NextGC)...)

	b = append(b, "# HELP go_memstats_gc_cpu_fraction GC 占 CPU 时间比例\n"...)
	b = append(b, "# TYPE go_memstats_gc_cpu_fraction gauge\n"...)
	b = append(b, fmt.Sprintf("go_memstats_gc_cpu_fraction %f\n", ms.GCCPUFraction)...)

	// GC 耗时概要（sum/count/max，等价 prometheus go_gc_duration_seconds summary）。
	b = append(b, "# HELP go_gc_duration_seconds GC 耗时（秒）\n"...)
	b = append(b, "# TYPE go_gc_duration_seconds summary\n"...)
	b = append(b, fmt.Sprintf("go_gc_duration_seconds_sum %f\n", float64(ms.PauseTotalNs)/1e9)...)
	b = append(b, fmt.Sprintf("go_gc_duration_seconds_count %d\n", ms.NumGC)...)

	// --- process_* 指标（等价 prometheus ProcessCollector 子集） ---
	b = append(b, "# HELP process_start_time_seconds 进程启动时间（Unix 秒）\n"...)
	b = append(b, "# TYPE process_start_time_seconds gauge\n"...)
	b = append(b, fmt.Sprintf("process_start_time_seconds %f\n", processStartTime)...)

	// 进程 RSS/VMS 通过 runtime.MemStats 近似（Go 无 libc getrusage 零依赖封装）。
	// Sys 等价 VMS（向 OS 申请总量），HeapInuse 近似 RSS（实际驻留堆）。
	// 注：真实 process_resident_memory_bytes 需读 /proc/self/status（Linux）或 GetProcessMemoryInfo（Win），
	// 此处用 HeapInuse 近似以保持零依赖跨平台；如需精确值可在 Render 调用方覆盖。
	b = append(b, "# HELP process_resident_memory_bytes 进程驻留内存近似值（字节，用 heap_inuse 近似）\n"...)
	b = append(b, "# TYPE process_resident_memory_bytes gauge\n"...)
	b = append(b, fmt.Sprintf("process_resident_memory_bytes %d\n", ms.HeapInuse)...)

	b = append(b, "# HELP process_virtual_memory_bytes 进程虚拟内存近似值（字节，用 sys 近似）\n"...)
	b = append(b, "# TYPE process_virtual_memory_bytes gauge\n"...)
	b = append(b, fmt.Sprintf("process_virtual_memory_bytes %d\n", ms.Sys)...)

	// 进程 PID（便于多实例区分）。
	b = append(b, "# HELP process_pid 进程 PID\n"...)
	b = append(b, "# TYPE process_pid gauge\n"...)
	b = append(b, fmt.Sprintf("process_pid %d\n", os.Getpid())...)

	return b
}
