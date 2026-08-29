package k8smetrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInitAndPredefinedMetrics(t *testing.T) {
	Init()

	// Verify all four pre-defined metrics are registered.
	expected := []string{
		"opsmesh_gpu_queue_depth",
		"opsmesh_task_pending_count",
		"opsmesh_device_heartbeat_lag",
		"opsmesh_alert_firing_count",
	}
	out := defaultRegistry.render()
	for _, name := range expected {
		if !strings.Contains(out, "# HELP "+name) {
			t.Fatalf("missing HELP for %q\n---%s", name, out)
		}
		if !strings.Contains(out, "# TYPE "+name+" gauge") {
			t.Fatalf("missing TYPE for %q\n---%s", name, out)
		}
	}
}

func TestRegisterAndSetCustomMetric(t *testing.T) {
	Init()
	RegisterCustomMetric("opsmesh_custom_test", "A test metric.")
	SetCustomMetric("opsmesh_custom_test", 42.5, map[string]string{"region": "us-east"})

	out := defaultRegistry.render()
	if !strings.Contains(out, `opsmesh_custom_test{region="us-east"} 42.5`) {
		t.Fatalf("custom metric not found\n---%s", out)
	}
}

func TestSetCustomMetricUnregistered(t *testing.T) {
	Init()
	// Setting an unregistered metric should be a no-op.
	SetCustomMetric("opsmesh_nonexistent", 1.0, nil)

	out := defaultRegistry.render()
	if strings.Contains(out, "opsmesh_nonexistent") {
		t.Fatalf("unregistered metric should not appear\n---%s", out)
	}
}

func TestSetCustomMetricNoLabels(t *testing.T) {
	Init()
	SetCustomMetric("opsmesh_gpu_queue_depth", 7.0, nil)

	out := defaultRegistry.render()
	if !strings.Contains(out, "opsmesh_gpu_queue_depth 7") {
		t.Fatalf("metric without labels not found\n---%s", out)
	}
}

func TestGetMetricsHandler(t *testing.T) {
	Init()
	SetCustomMetric("opsmesh_gpu_queue_depth", 3.0, nil)

	handler := GetMetricsHandler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "opsmesh_gpu_queue_depth 3") {
		t.Fatalf("handler body missing metric\n---%s", body)
	}
}

func TestGetMetricsHandlerNotFound(t *testing.T) {
	Init()
	handler := GetMetricsHandler()
	req := httptest.NewRequest(http.MethodGet, "/other", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestGetMetricsHandlerNotInitialized(t *testing.T) {
	defaultRegistry = nil
	handler := GetMetricsHandler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestMetricLabelsSorted(t *testing.T) {
	Init()
	SetCustomMetric("opsmesh_task_pending_count", 10, map[string]string{
		"zone":   "us-east-1a",
		"tenant": "t1",
		"region": "us-east",
	})

	out := defaultRegistry.render()
	// Labels should be sorted alphabetically: region, tenant, zone.
	expected := `opsmesh_task_pending_count{region="us-east",tenant="t1",zone="us-east-1a"} 10`
	if !strings.Contains(out, expected) {
		t.Fatalf("labels not sorted correctly\nwant: %s\n---%s", expected, out)
	}
}

func TestParseLine(t *testing.T) {
	cases := []struct {
		line      string
		wantName  string
		wantValue float64
	}{
		{"opsmesh_gpu_queue_depth 5", "opsmesh_gpu_queue_depth", 5},
		{"opsmesh_task_pending_count{region=\"us-east\"} 12", "opsmesh_task_pending_count", 12},
		{"http_requests_total{method=\"GET\",path=\"/api\"} 100", "http_requests_total", 100},
		{"active_connections 0", "active_connections", 0},
	}
	for _, tc := range cases {
		name, value, err := parseLine(tc.line)
		if err != nil {
			t.Fatalf("parseLine(%q) error: %v", tc.line, err)
		}
		if name != tc.wantName || value != tc.wantValue {
			t.Fatalf("parseLine(%q) = (%s, %v), want (%s, %v)", tc.line, name, value, tc.wantName, tc.wantValue)
		}
	}
}

func TestParseMetricValue(t *testing.T) {
	body := []byte(`# HELP opsmesh_gpu_queue_depth GPU workload queue depth.
# TYPE opsmesh_gpu_queue_depth gauge
opsmesh_gpu_queue_depth 8
opsmesh_task_pending_count 15
opsmesh_task_pending_count{region="us-east"} 7
`)
	value, err := parseMetricValue(body, "opsmesh_task_pending_count")
	if err != nil {
		t.Fatalf("parseMetricValue error: %v", err)
	}
	// Should sum both instances: 15 + 7 = 22.
	if value != 22 {
		t.Fatalf("parseMetricValue = %v, want 22", value)
	}
}

func TestNilRegistryNoPanic(t *testing.T) {
	defaultRegistry = nil
	// Should not panic.
	RegisterCustomMetric("x", "y")
	SetCustomMetric("x", 1, nil)
}

func TestMultipleMetricInstances(t *testing.T) {
	Init()
	SetCustomMetric("opsmesh_alert_firing_count", 3, map[string]string{"tenant": "t1"})
	SetCustomMetric("opsmesh_alert_firing_count", 5, map[string]string{"tenant": "t2"})

	out := defaultRegistry.render()
	if !strings.Contains(out, `opsmesh_alert_firing_count{tenant="t1"} 3`) {
		t.Fatalf("missing t1 instance\n---%s", out)
	}
	if !strings.Contains(out, `opsmesh_alert_firing_count{tenant="t2"} 5`) {
		t.Fatalf("missing t2 instance\n---%s", out)
	}
}

func TestInitOverwrite(t *testing.T) {
	Init()
	SetCustomMetric("opsmesh_gpu_queue_depth", 100, nil)
	// Re-init should clear all state.
	Init()
	out := defaultRegistry.render()
	if strings.Contains(out, "100") {
		t.Fatalf("re-init should clear old metric values\n---%s", out)
	}
}
