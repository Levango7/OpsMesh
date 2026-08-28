package metrics

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestRegistryRecordAndRender(t *testing.T) {
	Init("test-svc")
	RecordHTTPRequest("GET", "/api/v1/devices", "200", 0.01)
	RecordHTTPRequest("GET", "/api/v1/devices", "200", 0.05)
	RecordHTTPRequest("POST", "/api/v1/tasks", "201", 0.1)
	RecordActiveConnections(5)
	RecordActiveConnections(-2)
	RecordQueueDepth(10)
	RecordBusinessMetric("alerts_firing", 3, map[string]string{"tenant_id": "default"})

	out := defaultRegistry.render()

	for _, want := range []string{
		`http_requests_total{method="GET",path="/api/v1/devices",status="200"} 2`,
		`http_requests_total{method="POST",path="/api/v1/tasks",status="201"} 1`,
		`http_request_duration_seconds_bucket{method="GET",path="/api/v1/devices",le="0.01"} 1`,
		`http_request_duration_seconds_bucket{method="GET",path="/api/v1/devices",le="0.05"} 2`,
		`http_request_duration_seconds_bucket{method="GET",path="/api/v1/devices",le="+Inf"} 2`,
		`active_connections 3`,
		`queue_depth 10`,
		`business_metrics{name="alerts_firing",tenant_id="default"} 3`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q\n---%s", want, out)
		}
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
	RecordQueueDepth(1)
	RecordBusinessMetric("x", 1, nil)
}

func TestBusinessMetricLabels(t *testing.T) {
	Init("test-svc")
	RecordBusinessMetric("tasks_running", 5, map[string]string{"tenant_id": "t1", "region": "us-east"})
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
	RecordBusinessMetric("alerts_firing", 5, map[string]string{"tenant_id": "t1"})
	RecordBusinessMetric("alerts_firing", 10, map[string]string{"tenant_id": "t1"})
	out := defaultRegistry.render()
	if !strings.Contains(out, `business_metrics{name="alerts_firing",tenant_id="t1"} 10`) {
		t.Fatalf("expected updated value 10\n---%s", out)
	}
}

func TestMultipleBusinessMetrics(t *testing.T) {
	Init("test-svc")
	RecordBusinessMetric("metric_a", 1, nil)
	RecordBusinessMetric("metric_b", 2, nil)
	RecordBusinessMetric("metric_c", 3, map[string]string{"env": "prod"})
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

func TestQueueDepthUpdate(t *testing.T) {
	Init("test-svc")
	RecordQueueDepth(5)
	RecordQueueDepth(15)
	RecordQueueDepth(0)
	out := defaultRegistry.render()
	if !strings.Contains(out, "queue_depth 0") {
		t.Fatalf("expected queue_depth 0\n---%s", out)
	}
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
		RecordBusinessMetric("m"+strconv.Itoa(i), float64(i), map[string]string{"t": "1"})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = defaultRegistry.render()
	}
}
