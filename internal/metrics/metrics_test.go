package metrics

import (
	"fmt"
	"strconv"
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

// TestHTTPMetrics 验证 HTTP 请求计数器与延迟直方图输出格式。
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

// TestRuntimeMetrics 验证 Go runtime 指标输出。
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

// TestAuditChainMetrics 验证审计链自检指标的暴露与取值（P1-3）。
// 区分「校验失败」与「无法校验」是告警表达式的前提（supported=0 时 ok=0 不应报警）。
func TestAuditChainMetrics(t *testing.T) {
	m := New()
	// 初始：未校验 → ok=0 / supported=0（非 leader 副本不应触发告警）。
	out := m.Render()
	for _, want := range []string{
		"# TYPE opsmesh_audit_chain_ok gauge",
		"# TYPE opsmesh_audit_chain_supported gauge",
		"# TYPE opsmesh_audit_chain_checked_rows gauge",
		"# TYPE opsmesh_audit_chain_checks_total counter",
		"opsmesh_audit_chain_ok 0\n",
		"opsmesh_audit_chain_supported 0\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("初始状态缺少 %q\n---%s", want, out)
		}
	}
	// 校验通过：ok=1 / supported=1 / checked_rows=42 / checks_total=1。
	m.SetAuditChainStatus(true, 42, true)
	out = m.Render()
	for _, want := range []string{
		"opsmesh_audit_chain_ok 1\n",
		"opsmesh_audit_chain_supported 1\n",
		"opsmesh_audit_chain_checked_rows 42\n",
		"opsmesh_audit_chain_checks_total 1\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("校验通过后缺少 %q\n---%s", want, out)
		}
	}
	// 检出不一致：ok=0 / supported 仍为 1（可告警）。
	m.SetAuditChainStatus(false, 42, true)
	out = m.Render()
	if !strings.Contains(out, "opsmesh_audit_chain_ok 0\n") || !strings.Contains(out, "opsmesh_audit_chain_supported 1\n") {
		t.Fatalf("检出不一致时 ok/supported 取值错误\n---%s", out)
	}
	if !strings.Contains(out, "opsmesh_audit_chain_checks_total 2\n") {
		t.Fatalf("checks_total 应累加\n---%s", out)
	}
	// 后端不支持：supported=0（区别于「不一致」，避免误告警）。
	m.SetAuditChainStatus(false, 0, false)
	out = m.Render()
	if !strings.Contains(out, "opsmesh_audit_chain_supported 0\n") {
		t.Fatalf("不支持的后端 supported 应为 0\n---%s", out)
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

// TestHTTPMetricsSeriesCap 验证 HTTP 指标基数熔断（P1-5）。
// 场景：扫描器遍历大量随机路径（历史上每条路径都会成为独立时序，内存无限增长）。
// 期望：时序数被硬上限封顶；超限路径折叠到 path=":other"；既有真实路径计数不中断。
func TestHTTPMetricsSeriesCap(t *testing.T) {
	m := New()
	// 先登记一条真实路径，用于验证"已登记键在达到上限后仍继续累积"。
	m.RecordHTTP("GET", "/api/v1/devices", "200", 0.01)

	// 遍历 maxHTTPSeries+overflow 条互不相同的路径（模拟扫描器）。
	const overflow = 50
	for i := 0; i < maxHTTPSeries+overflow; i++ {
		m.RecordHTTP("GET", "/probe/"+strconv.Itoa(i), "404", 0.01)
	}
	// 时序数：上限内 maxHTTPSeries 个 + 折叠键 1 个，不再随请求数增长。
	if got := len(m.httpSeries); got != maxHTTPSeries+1 {
		t.Fatalf("时序数未封顶：got %d, want %d", got, maxHTTPSeries+1)
	}
	// 前 maxHTTPSeries-1 条 probe 路径占满剩余额度（已有 1 条真实路径），其余全部折叠。
	wantDropped := uint64(overflow + 1)
	if m.httpSeriesDropped != wantDropped {
		t.Fatalf("折叠计数 = %d, want %d", m.httpSeriesDropped, wantDropped)
	}
	if len(m.httpHist) != maxHTTPSeries+1 {
		t.Fatalf("直方图时序数未封顶：got %d", len(m.httpHist))
	}

	// 既有真实路径仍可精确查询且计数连续；随后新路径继续折叠（计数 +1）。
	m.RecordHTTP("GET", "/api/v1/devices", "200", 0.01)
	m.RecordHTTP("GET", "/probe/new-one", "404", 0.01)
	if m.httpSeriesDropped != wantDropped+1 {
		t.Fatalf("超限后新路径应折叠：dropped = %d, want %d", m.httpSeriesDropped, wantDropped+1)
	}
	out := m.Render()
	if !strings.Contains(out, `opsmesh_http_requests_total{method="GET",path="/api/v1/devices",status="200"} 2`) {
		t.Fatalf("既有路径计数不连续\n---%s", out)
	}
	// 折叠键可见且计数正确（= 被折叠的请求数），便于告警识别"正在被扫描"。
	folded := fmt.Sprintf(`opsmesh_http_requests_total{method="GET",path=":other",status="404"} %d`, wantDropped+1)
	if !strings.Contains(out, folded) {
		t.Fatalf("折叠键计数错误，want %q\n---%s", folded, out)
	}
	if !strings.Contains(out, fmt.Sprintf("opsmesh_http_metrics_series_dropped_total %d\n", wantDropped+1)) {
		t.Fatalf("折叠计数器缺失\n---%s", out)
	}
	if !strings.Contains(out, fmt.Sprintf("opsmesh_http_metrics_series %d\n", maxHTTPSeries+1)) {
		t.Fatalf("时序数指标缺失\n---%s", out)
	}
	// Prometheus 文本格式合法性：直方图折叠键也必须在 +Inf 桶上等于 count。
	if !strings.Contains(out, fmt.Sprintf(`opsmesh_http_request_duration_seconds_count{method="GET",path=":other",status="404"} %d`, wantDropped+1)) {
		t.Fatalf("折叠键直方图 count 缺失\n---%s", out)
	}
}

// TestHTTPMetricsMethodCollapse 验证非标准 HTTP 方法收敛（P1-5）：
// method 由请求方自选，若原样入标签，折叠键空间也会被撑大。
func TestHTTPMetricsMethodCollapse(t *testing.T) {
	cases := map[string]string{
		"GET": "GET", "POST": "POST", "PUT": "PUT", "DELETE": "DELETE",
		"PATCH": "PATCH", "HEAD": "HEAD", "OPTIONS": "OPTIONS",
		"TRACE": ":other", "CONNECT": ":other", "evil-custom": ":other", "": ":other",
	}
	for in, want := range cases {
		if got := collapseHTTPMethod(in); got != want {
			t.Fatalf("collapseHTTPMethod(%q) = %q, want %q", in, got, want)
		}
	}
}
