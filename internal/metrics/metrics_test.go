package metrics

import (
	"strings"
	"testing"
)

func TestMetricsRender(t *testing.T) {
	m := New()
	m.SetAgents(3)
	m.IncTask("done")
	m.IncTask("done")
	m.IncTask("failed")
	m.SetQueueDepth(2)
	m.ObserveDuration(1.5)
	m.ObserveDuration(0.5)

	out := m.Render()
	for _, want := range []string{
		"opsmesh_agents_total 3",
		"opsmesh_tasks_total{status=\"done\"} 2",
		"opsmesh_tasks_total{status=\"failed\"} 1",
		"opsmesh_task_queue_depth 2",
		"opsmesh_task_duration_seconds_count 2",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("Render 缺少 %q\n---%s", want, out)
		}
	}
}

// TestHTTPMetrics 验证 P1-C3 HTTP 请求计数器与延迟直方图输出格式（P1-C3）。
func TestHTTPMetrics(t *testing.T) {
	m := New()
	// 模拟 3 次 GET /api/v1/devices 200，耗时分别 0.001s/0.05s/2s
	m.IncHTTPRequest("GET", "/api/v1/devices", "200")
	m.IncHTTPRequest("GET", "/api/v1/devices", "200")
	m.IncHTTPRequest("GET", "/api/v1/devices", "200")
	m.ObserveHTTPRequestDuration("GET", "/api/v1/devices", "200", 0.001)
	m.ObserveHTTPRequestDuration("GET", "/api/v1/devices", "200", 0.05)
	m.ObserveHTTPRequestDuration("GET", "/api/v1/devices", "200", 2.0)
	// 1 次 POST /api/v1/tasks 500
	m.IncHTTPRequest("POST", "/api/v1/tasks", "500")
	m.ObserveHTTPRequestDuration("POST", "/api/v1/tasks", "500", 0.3)

	out := m.Render()
	// 计数器
	if !strings.Contains(out, `opsmesh_http_requests_total{method="GET",path="/api/v1/devices",status="200"} 3`) {
		t.Fatalf("HTTP 计数器 GET/200 缺失\n---%s", out)
	}
	if !strings.Contains(out, `opsmesh_http_requests_total{method="POST",path="/api/v1/tasks",status="500"} 1`) {
		t.Fatalf("HTTP 计数器 POST/500 缺失\n---%s", out)
	}
	// 直方图 count/sum
	if !strings.Contains(out, `opsmesh_http_request_duration_seconds_count{method="GET",path="/api/v1/devices",status="200"} 3`) {
		t.Fatalf("HTTP 直方图 count 缺失\n---%s", out)
	}
	// +Inf 桶应等于 count
	if !strings.Contains(out, `opsmesh_http_request_duration_seconds_bucket{method="GET",path="/api/v1/devices",status="200",le="+Inf"} 3`) {
		t.Fatalf("HTTP 直方图 +Inf 桶缺失\n---%s", out)
	}
	// 桶上界标签存在（0.005）
	if !strings.Contains(out, `opsmesh_http_request_duration_seconds_bucket{method="GET",path="/api/v1/devices",status="200",le="0.005"}`) {
		t.Fatalf("HTTP 直方图 le=0.005 桶缺失\n---%s", out)
	}
}

// TestRuntimeMetrics 验证 P1-C3 Go runtime 指标输出（P1-C3）。
func TestRuntimeMetrics(t *testing.T) {
	m := New()
	out := m.Render()
	for _, want := range []string{
		"# TYPE go_goroutines gauge",
		"# TYPE go_memstats_alloc_bytes gauge",
		"# TYPE go_memstats_sys_bytes gauge",
		"# TYPE go_memstats_heap_inuse_bytes gauge",
		"# TYPE go_gc_duration_seconds summary",
		"# TYPE process_start_time_seconds gauge",
		"# TYPE process_resident_memory_bytes gauge",
		"# TYPE process_virtual_memory_bytes gauge",
		"# TYPE process_pid gauge",
		"go_goroutines ",
		"process_start_time_seconds ",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("runtime 指标缺失 %q\n---%s", want, out)
		}
	}
}

// TestSplitHTTPKey 验证 method|path|status 键拆分正确性。
func TestSplitHTTPKey(t *testing.T) {
	method, path, status := splitHTTPKey("GET|/api/v1/devices|200")
	if method != "GET" || path != "/api/v1/devices" || status != "200" {
		t.Fatalf("splitHTTPKey = %q/%q/%q, want GET//api/v1/devices/200", method, path, status)
	}
	// 归一化路径（含 :id）不破坏拆分
	method, path, status = splitHTTPKey("DELETE|/api/v1/devices/:id|204")
	if method != "DELETE" || path != "/api/v1/devices/:id" || status != "204" {
		t.Fatalf("splitHTTPKey 归一化路径拆分错误 = %q/%q/%q", method, path, status)
	}
}
