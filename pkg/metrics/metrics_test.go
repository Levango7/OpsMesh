package metrics

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRegistryRecordAndRender(t *testing.T) {
	Init("test-svc")
	RecordHTTPRequest("GET", "/api/v1/devices", "200", 0.01)
	RecordHTTPRequest("GET", "/api/v1/devices", "200", 0.05)
	RecordHTTPRequest("POST", "/api/v1/tasks", "201", 0.1)
	RecordActiveConnections(5)
	RecordActiveConnections(-2)
	SetBusinessMetric("alerts_firing", 3, map[string]string{"tenant_id": "default"})
	// counter 家族名已带 _total，指标名不再重复该后缀（迁移到本 API 时要去掉后缀）。
	AddBusinessMetric("alerts_delivered", 1, map[string]string{"tenant_id": "default"})
	AddBusinessMetric("alerts_delivered", 1, map[string]string{"tenant_id": "default"})

	out := defaultRegistry.render()

	for _, want := range []string{
		`http_requests_total{method="GET",path="/api/v1/devices",status="200"} 2`,
		`http_requests_total{method="POST",path="/api/v1/tasks",status="201"} 1`,
		`http_request_duration_seconds_bucket{method="GET",path="/api/v1/devices",le="0.01"} 1`,
		`http_request_duration_seconds_bucket{method="GET",path="/api/v1/devices",le="0.05"} 2`,
		`http_request_duration_seconds_bucket{method="GET",path="/api/v1/devices",le="+Inf"} 2`,
		`active_connections 3`,
		`service_info{service="test-svc"} 1`,
		`business_metrics{name="alerts_firing",tenant_id="default"} 3`,
		// counter 家族必须**累加**，不是覆盖：两次 +1 得到 2 而不是 1。
		`business_metrics_total{name="alerts_delivered",tenant_id="default"} 2`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q\n---%s", want, out)
		}
	}
	// queue_depth 已删除：它以前恒输出 0（全仓零调用方），是"声明了但从不喂"的假仪表。
	if strings.Contains(out, "queue_depth") {
		t.Fatalf("queue_depth 应已随 RecordQueueDepth 一起删除\n---%s", out)
	}
}

func TestGetHandler(t *testing.T) {
	Init("test-svc")
	RecordHTTPRequest("GET", "/health", "200", 0.001)

	handler := GetHandler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `http_requests_total{method="GET",path="/health",status="200"} 1`) {
		t.Fatalf("handler body missing metric\n---%s", body)
	}
}

func TestGetHandlerNotFound(t *testing.T) {
	Init("test-svc")
	handler := GetHandler()
	req := httptest.NewRequest(http.MethodGet, "/other", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestHTTPMiddleware(t *testing.T) {
	Init("test-svc")
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	})
	handler := HTTPMiddleware(next)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}
	out := defaultRegistry.render()
	if !strings.Contains(out, `http_requests_total{method="POST",path="/api/v1/tasks",status="201"} 1`) {
		t.Fatalf("middleware did not record metric\n---%s", out)
	}
}

func TestHistogramBuckets(t *testing.T) {
	Init("test-svc")
	// Record values across different buckets
	RecordHTTPRequest("GET", "/api/v1/test", "200", 0.001) // le=0.005
	RecordHTTPRequest("GET", "/api/v1/test", "200", 0.03)  // le=0.05
	RecordHTTPRequest("GET", "/api/v1/test", "200", 0.3)   // le=0.5
	RecordHTTPRequest("GET", "/api/v1/test", "200", 15.0)  // +Inf

	out := defaultRegistry.render()
	// Cumulative: le=0.005 -> 1, le=0.05 -> 2, le=0.5 -> 3, +Inf -> 4
	for _, want := range []string{
		`http_request_duration_seconds_bucket{method="GET",path="/api/v1/test",le="0.005"} 1`,
		`http_request_duration_seconds_bucket{method="GET",path="/api/v1/test",le="0.05"} 2`,
		`http_request_duration_seconds_bucket{method="GET",path="/api/v1/test",le="0.5"} 3`,
		`http_request_duration_seconds_bucket{method="GET",path="/api/v1/test",le="+Inf"} 4`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("bucket missing %q\n---%s", want, out)
		}
	}
}

func TestNilRegistry(t *testing.T) {
	defaultRegistry = nil
	// Should not panic when registry is nil.
	RecordHTTPRequest("GET", "/x", "200", 0.1)
	RecordActiveConnections(1)
	SetBusinessMetric("x", 1, nil)
	AddBusinessMetric("x", 1, nil)
}

func TestBusinessMetricLabels(t *testing.T) {
	Init("test-svc")
	SetBusinessMetric("tasks_running", 5, map[string]string{"tenant_id": "t1", "region": "us-east"})
	out := defaultRegistry.render()
	// Labels should be sorted alphabetically.
	if !strings.Contains(out, `business_metrics{name="tasks_running",region="us-east",tenant_id="t1"} 5`) {
		t.Fatalf("business metric labels not sorted\n---%s", out)
	}
}

func TestActiveConnectionsDelta(t *testing.T) {
	Init("test-svc")
	RecordActiveConnections(10)
	RecordActiveConnections(-3)
	RecordActiveConnections(5)
	out := defaultRegistry.render()
	if !strings.Contains(out, "active_connections 12") {
		t.Fatalf("expected 12 connections\n---%s", out)
	}
}

func TestSplitReqKey(t *testing.T) {
	m, p, s := splitReqKey("GET|/api/v1/devices/:id|200")
	if m != "GET" || p != "/api/v1/devices/:id" || s != "200" {
		t.Fatalf("split = %q/%q/%q", m, p, s)
	}
}

func TestSplitHistKey(t *testing.T) {
	m, p := splitHistKey("GET|/api/v1/devices")
	if m != "GET" || p != "/api/v1/devices" {
		t.Fatalf("split = %q/%q", m, p)
	}
}

func TestFormatBucket(t *testing.T) {
	cases := map[float64]string{
		0.005: "0.005",
		0.01:  "0.01",
		0.1:   "0.1",
		1:     "1",
		10:    "10",
	}
	for in, want := range cases {
		got := formatBucket(in)
		if got != want {
			t.Fatalf("formatBucket(%f) = %q, want %q", in, got, want)
		}
	}
}

func TestRecordHTTPRequestDurationTracking(t *testing.T) {
	Init("test-svc")
	for i := 0; i < 100; i++ {
		RecordHTTPRequest("GET", "/api/v1/test", "200", 0.01)
	}
	out := defaultRegistry.render()
	if !strings.Contains(out, `http_requests_total{method="GET",path="/api/v1/test",status="200"} 100`) {
		t.Fatalf("expected 100 requests\n---%s", out)
	}
	// Verify histogram count matches.
	if !strings.Contains(out, `http_request_duration_seconds_count{method="GET",path="/api/v1/test"} 100`) {
		t.Fatalf("histogram count mismatch\n---%s", out)
	}
}

func TestRenderDoesNotPanicOnEmptyRegistry(t *testing.T) {
	Init("test-svc")
	out := defaultRegistry.render()
	if !strings.Contains(out, "active_connections 0") {
		t.Fatalf("empty registry should show 0 connections\n---%s", out)
	}
}

func TestGetHandlerContentType(t *testing.T) {
	Init("test-svc")
	handler := GetHandler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain prefix", ct)
	}
}

func TestHTTPMiddlewareStatusCapture(t *testing.T) {
	Init("test-svc")
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	handler := HTTPMiddleware(next)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/fail", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	out := defaultRegistry.render()
	if !strings.Contains(out, `http_requests_total{method="GET",path="/api/v1/fail",status="500"} 1`) {
		t.Fatalf("middleware did not capture 500 status\n---%s", out)
	}
}

func TestBusinessMetricUpdateValue(t *testing.T) {
	Init("test-svc")
	SetBusinessMetric("alerts_firing", 5, map[string]string{"tenant_id": "t1"})
	SetBusinessMetric("alerts_firing", 10, map[string]string{"tenant_id": "t1"})
	out := defaultRegistry.render()
	if !strings.Contains(out, `business_metrics{name="alerts_firing",tenant_id="t1"} 10`) {
		t.Fatalf("expected updated value 10\n---%s", out)
	}
}

func TestMultipleBusinessMetrics(t *testing.T) {
	Init("test-svc")
	SetBusinessMetric("metric_a", 1, nil)
	SetBusinessMetric("metric_b", 2, nil)
	SetBusinessMetric("metric_c", 3, map[string]string{"env": "prod"})
	out := defaultRegistry.render()
	for _, want := range []string{
		`business_metrics{name="metric_a"} 1`,
		`business_metrics{name="metric_b"} 2`,
		`business_metrics{name="metric_c",env="prod"} 3`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q\n---%s", want, out)
		}
	}
}

func TestNormalizePath(t *testing.T) {
	cases := map[string]string{
		"":                                    "",
		"/":                                   "/",
		"/api/v1/devices":                     "/api/v1/devices",
		"/api/v1/devices/123":                 "/api/v1/devices/:id",
		"/api/v1/devices/10.0.0.1":            "/api/v1/devices/10.0.0.1",
		"/api/v1/users/u-abc-1":               "/api/v1/users/u-abc-1",
		"/api/v1/tasks/batch":                 "/api/v1/tasks/batch",
		"/api/v1/evil path/x":                 "/api/v1/:id/x",
		"/api/v1/d/123/alerts/456":            "/api/v1/d/:id/alerts/:id",
		"/api/v1/" + strings.Repeat("9", 60):  "/api/v1/:id",
		"/api/v1/" + strings.Repeat("a", 300): "/:overlong",
	}
	for in, want := range cases {
		if got := NormalizePath(in); got != want {
			t.Fatalf("NormalizePath(%q) = %q, want %q", in, got, want)
		}
	}
}

// 扫描器遍历随机路径曾可让 reqTotal/reqHist 无限增长直至 OOM——上限与折叠必须真实生效。
// 路径用"含字母的唯一段"：纯数字段会被 NormalizePath 折成 :id，那样根本到不了上限，
// 测的就不是熔断而是归一化了（第一版用例就踩在这个前提错误上）。
func TestHTTPSeriesCapFoldsToOther(t *testing.T) {
	Init("test-svc")
	// 次数与期望都写死，不引用 maxHTTPSeries：用例若用实现常量推导期望，把常量改大就同时改大了
	// 用例自己 ⇒ 变异杀不掉（实测存活过一轮，故钉死字面值）。
	for i := 0; i < 2500; i++ {
		RecordHTTPRequest("GET", "/api/v1/scan/item-"+strconv.Itoa(i), "200", 0.01)
	}
	out := defaultRegistry.render()
	// 2001 = 2000 个真实键 + 1 个折叠键（折叠键也占一个名额，这是"序列数"的诚实口径）。
	if !strings.Contains(out, "http_metrics_series 2001\n") {
		t.Fatalf("时序数未钉在 2000 上限（真实键 2000 + 折叠键 1 = 2001）\n---%s", lastLines(out, 12))
	}
	if !strings.Contains(out, `http_requests_total{method="GET",path=":other",status="200"} 500`) {
		t.Fatalf("超限请求未折叠到 :other\n---%s", lastLines(out, 12))
	}
	// 折叠必须可观测：丢弃计数 = 2500-2000 = 500。
	if !strings.Contains(out, "http_metrics_series_dropped_total 500") {
		t.Fatalf("丢弃计数不正确（折叠等于悄悄丢数据，必须看得见）\n---%s", lastLines(out, 12))
	}
	// 直方图键也必须受同一个上限约束：以前 histKey 用原始 method 拼，会绕开上限重新撑开。
	if n := strings.Count(out, "http_request_duration_seconds_count{"); n > 2001 {
		t.Fatalf("直方图键数 %d 超过上限 %d ⇒ histKey 没走折叠键", n, maxHTTPSeries+1)
	}
	if !strings.Contains(out, `http_request_duration_seconds_count{method="GET",path=":other"} 500`) {
		t.Fatalf("直方图没有折叠键（说明超限请求另开了新键）")
	}
}

// 非标准方法由请求方自选，不收敛会撑大折叠后的键空间。
func TestUnknownMethodCollapsed(t *testing.T) {
	Init("test-svc")
	for i := 0; i < maxHTTPSeries+50; i++ {
		RecordHTTPRequest("WEIRD"+strconv.Itoa(i), "/api/v1/x", "200", 0.01)
	}
	out := defaultRegistry.render()
	if strings.Contains(out, `method="WEIRD`) {
		t.Fatalf("非标准方法未被收敛 ⇒ 折叠键空间仍可无界增长")
	}
}

func TestGaugeAndCounterSameNameDoNotCollide(t *testing.T) {
	Init("test-svc")
	SetBusinessMetric("jobs", 7, nil)
	AddBusinessMetric("jobs", 3, nil)
	out := defaultRegistry.render()
	if !strings.Contains(out, `business_metrics{name="jobs"} 7`) {
		t.Fatalf("gauge 丢失\n---%s", out)
	}
	if !strings.Contains(out, `business_metrics_total{name="jobs"} 3`) {
		t.Fatalf("counter 丢失（两类共用一个 name 时必须各自成族）\n---%s", out)
	}
}

// TYPE 行必须与家族一致：counter 家族标成 gauge 会让 promtool 与 Prometheus 都判错。
func TestFamilyTypeLines(t *testing.T) {
	Init("test-svc")
	AddBusinessMetric("events", 1, nil)
	out := defaultRegistry.render()
	for _, want := range []string{
		"# TYPE http_requests_total counter",
		"# TYPE business_metrics gauge",
		"# TYPE business_metrics_total counter",
		"# TYPE http_metrics_series_dropped_total counter",
		"# TYPE active_connections gauge",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("缺少 %q\n---%s", want, out)
		}
	}
}

// 标签值来自调用方传入的 ID 时不设限 = 把基数交给客户端。
func TestLabelValueSanitized(t *testing.T) {
	Init("test-svc")
	SetBusinessMetric("m", 1, map[string]string{"q": "has space/and?slash"})
	SetBusinessMetric("n", 1, map[string]string{"q": strings.Repeat("x", 200)})
	out := defaultRegistry.render()
	if strings.Contains(out, `q="has space/and?slash"`) || strings.Contains(out, `q="`+strings.Repeat("x", 200)+`"`) {
		t.Fatalf("未 sanitise 的标签值直接进了exposition：\n---%s", out)
	}
	if !strings.Contains(out, `q=":other"`) {
		t.Fatalf("非法标签值应折叠成 :other\n---%s", out)
	}
}

func TestBusinessSeriesCapFolds(t *testing.T) {
	Init("test-svc")
	for i := 0; i < 2300; i++ {
		AddBusinessMetric("task_claims", 1, map[string]string{"agent_id": "agent-" + strconv.Itoa(i)})
	}
	out := defaultRegistry.render()
	if !strings.Contains(out, "business_metrics_series 2001") {
		t.Fatalf("业务时序未钉住（真实键 2000 + 折叠键 1 = 2001）")
	}
	if !strings.Contains(out, `business_metrics_series_dropped_total 300`) {
		t.Fatalf("业务折叠未计数")
	}
	if !strings.Contains(out, `business_metrics_total{name="task_claims",folded=":other"}`) {
		t.Fatalf("超限业务写入未折叠\n---%s", lastLines(out, 8))
	}
}

// 中间件必须把 active_connections 喂成真实在途数（以前全仓零调用方 ⇒ 恒 0）。
func TestMiddlewareFeedsActiveConnections(t *testing.T) {
	Init("test-svc")
	entered := make(chan struct{})
	release := make(chan struct{})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
	})
	h := HTTPMiddleware(next)
	go func() {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/slow", nil))
	}()
	<-entered
	if got := defaultRegistry.render(); !strings.Contains(got, "active_connections 1") {
		t.Fatalf("在途请求未计入 active_connections\n---%s", lastLines(got, 8))
	}
	close(release)
	// defer 递减发生在响应返回后；给一个同步点。
	for i := 0; i < 200; i++ {
		if strings.Contains(defaultRegistry.render(), "active_connections 0") {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("请求结束后 active_connections 未归零")
}

// 归一化是第一道防线：带数字 ID 的路径必须先并成 :id，否则上限会被"同一路由的无穷实例"吃光。
// 这条用例专门杀"只留上限、去掉归一化"的变异（TestHTTPSeriesCapFoldsToOther 杀不掉它，
// 因为那条用的是含字母的唯一段，去掉归一化后照样撞上限）。
func TestNumericIDSeriesCollapse(t *testing.T) {
	Init("test-svc")
	for i := 0; i < 3000; i++ {
		RecordHTTPRequest("GET", "/api/v1/devices/"+strconv.Itoa(i), "200", 0.01)
	}
	out := defaultRegistry.render()
	if !strings.Contains(out, "http_metrics_series 1\n") {
		t.Fatalf("3000 个数字 ID 路径应并成 1 条时序（去掉 NormalizePath 就会活成 2001 条）\n---%s", lastLines(out, 10))
	}
	if !strings.Contains(out, `http_requests_total{method="GET",path="/api/v1/devices/:id",status="200"} 3000`) {
		t.Fatalf("未按 :id 归并或计数不对\n---%s", lastLines(out, 10))
	}
}

func lastLines(out string, n int) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func TestInitOverwrite(t *testing.T) {
	Init("svc-a")
	RecordHTTPRequest("GET", "/a", "200", 0.1)
	Init("svc-b")
	// After re-init, old metrics should be gone.
	out := defaultRegistry.render()
	if strings.Contains(out, "/a") {
		t.Fatalf("re-init should clear old metrics\n---%s", out)
	}
}

func BenchmarkRecordHTTPRequest(b *testing.B) {
	Init("bench-svc")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		RecordHTTPRequest("GET", "/api/v1/devices", "200", 0.01)
	}
}

func BenchmarkRender(b *testing.B) {
	Init("bench-svc")
	for i := 0; i < 100; i++ {
		RecordHTTPRequest("GET", "/api/v1/devices", "200", 0.01)
		RecordHTTPRequest("POST", "/api/v1/tasks", "201", 0.1)
		SetBusinessMetric("m"+strconv.Itoa(i), float64(i), map[string]string{"t": "1"})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = defaultRegistry.render()
	}
}
