package prometheus

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newMockPrometheus(t *testing.T) (*httptest.Server, *Client) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/-/healthy":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Prometheus is Healthy."))
		case "/api/v1/query":
			handleQuery(w, r)
		case "/api/v1/query_range":
			handleQueryRange(w, r)
		case "/api/v1/labels":
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]interface{}{"status": "success", "data": []string{"__name__", "instance", "job"}}
			_ = json.NewEncoder(w).Encode(resp)
		case "/api/v1/series":
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]interface{}{
				"status": "success",
				"data": []map[string]string{
					{"__name__": "node_cpu_seconds_total", "instance": "node-1"},
					{"__name__": "node_memory_MemTotal_bytes", "instance": "node-1"},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	client := NewClient(srv.URL, 5*time.Second)
	return srv, client
}

func handleQuery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	query := r.URL.Query().Get("query")
	result := map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"resultType": "vector",
			"result":     queryResultFor(query),
		},
	}
	_ = json.NewEncoder(w).Encode(result)
}

func handleQueryRange(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	query := r.URL.Query().Get("query")
	values := [][]interface{}{
		{time.Now().Add(-5 * time.Minute).Unix(), "45.2"},
		{time.Now().Add(-4 * time.Minute).Unix(), "62.8"},
		{time.Now().Add(-3 * time.Minute).Unix(), "33.1"},
	}
	_ = query
	result := map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"resultType": "matrix",
			"result": []map[string]interface{}{
				{
					"metric": map[string]string{"instance": "node-1"},
					"values": values,
				},
			},
		},
	}
	_ = json.NewEncoder(w).Encode(result)
}

func queryResultFor(query string) []map[string]interface{} {
	switch {
	case len(query) > 0 && query[0] == '1':
		return []map[string]interface{}{
			{
				"metric": map[string]string{"instance": "node-1", "cpu": "0"},
				"value":  []interface{}{time.Now().Unix(), "45.2"},
			},
		}
	case len(query) > 12 && query[:12] == "node_memory_":
		return []map[string]interface{}{
			{
				"metric": map[string]string{"instance": "node-1"},
				"value":  []interface{}{time.Now().Unix(), "8589934592"},
			},
		}
	case len(query) > 11 && query[:11] == "node_filesy":
		return []map[string]interface{}{
			{
				"metric": map[string]string{"instance": "node-1", "mountpoint": "/"},
				"value":  []interface{}{time.Now().Unix(), "107374182400"},
			},
		}
	case len(query) > 7 && query[:7] == "nvidia_":
		return []map[string]interface{}{
			{
				"metric": map[string]string{"gpu": "0", "model": "A100"},
				"value":  []interface{}{time.Now().Unix(), "78.3"},
			},
			{
				"metric": map[string]string{"gpu": "1", "model": "A100"},
				"value":  []interface{}{time.Now().Unix(), "42.1"},
			},
		}
	}
	return []map[string]interface{}{}
}

func TestNewClient_EmptyURL_SimulatedMode(t *testing.T) {
	c := NewClient("", 0)
	if c.Available() {
		t.Error("client with empty URL should not be available")
	}
}

func TestNewClient_WithURL_ChecksAvailability(t *testing.T) {
	srv, client := newMockPrometheus(t)
	defer srv.Close()

	if !client.Available() {
		t.Error("client should be available with healthy server")
	}
}

func TestIsAvailable_Healthy(t *testing.T) {
	srv, client := newMockPrometheus(t)
	defer srv.Close()

	if !client.IsAvailable() {
		t.Error("expected IsAvailable to return true")
	}
}

func TestIsAvailable_Unhealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	if c.IsAvailable() {
		t.Error("expected IsAvailable to return false for unhealthy server")
	}
}

func TestIsAvailable_ConnectionRefused(t *testing.T) {
	c := NewClient("http://127.0.0.1:1", 1*time.Second)
	if c.IsAvailable() {
		t.Error("expected IsAvailable to return false when connection refused")
	}
}

func TestQuery_Success(t *testing.T) {
	srv, client := newMockPrometheus(t)
	defer srv.Close()

	result, err := client.Query(`100 - avg(irate(node_cpu_seconds_total{mode="idle"}[5m])) * 100`, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "success" {
		t.Errorf("expected status 'success', got '%s'", result.Status)
	}
	if len(result.Data.Result) == 0 {
		t.Error("expected at least one result")
	}
}

func TestQueryRange_Success(t *testing.T) {
	srv, client := newMockPrometheus(t)
	defer srv.Close()

	now := time.Now()
	result, err := client.QueryRange(
		`node_cpu_seconds_total`,
		formatTS(now.Add(-5*time.Minute)),
		formatTS(now),
		"60s",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "success" {
		t.Errorf("expected status 'success', got '%s'", result.Status)
	}
}

func TestGetMetricNames_Success(t *testing.T) {
	srv, client := newMockPrometheus(t)
	defer srv.Close()

	names, err := client.GetMetricNames()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(names) == 0 {
		t.Error("expected non-empty label names")
	}
	found := false
	for _, n := range names {
		if n == "__name__" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected __name__ in label names")
	}
}

func TestGetSeries_Success(t *testing.T) {
	srv, client := newMockPrometheus(t)
	defer srv.Close()

	series, err := client.GetSeries(`{instance="node-1"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(series) == 0 {
		t.Error("expected non-empty series")
	}
}

func TestGetCPUUsage_Success(t *testing.T) {
	srv, client := newMockPrometheus(t)
	defer srv.Close()

	samples, err := client.GetCPUUsage("node-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(samples) == 0 {
		t.Error("expected CPU samples")
	}
	for _, s := range samples {
		if s.Value < 0 || s.Value > 100 {
			t.Errorf("CPU value out of range: %f", s.Value)
		}
	}
}

func TestGetMemoryUsage_Success(t *testing.T) {
	srv, client := newMockPrometheus(t)
	defer srv.Close()

	samples, err := client.GetMemoryUsage("node-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(samples) == 0 {
		t.Error("expected memory samples")
	}
}

func TestGetDiskUsage_Success(t *testing.T) {
	srv, client := newMockPrometheus(t)
	defer srv.Close()

	samples, err := client.GetDiskUsage("node-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(samples) == 0 {
		t.Error("expected disk samples")
	}
}

func TestGetGPUUtilization_Success(t *testing.T) {
	srv, client := newMockPrometheus(t)
	defer srv.Close()

	samples, err := client.GetGPUUtilization()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(samples) == 0 {
		t.Error("expected GPU samples")
	}
}

func TestSimulatedMode_FullFallback(t *testing.T) {
	c := NewClient("", 0)

	cpuSamples, err := c.GetCPUUsage("any-node")
	if err != nil {
		t.Fatalf("simulated CPU should not error: %v", err)
	}
	if len(cpuSamples) == 0 {
		t.Error("expected simulated CPU samples")
	}

	memSamples, err := c.GetMemoryUsage("any-node")
	if err != nil {
		t.Fatalf("simulated memory should not error: %v", err)
	}
	if len(memSamples) == 0 {
		t.Error("expected simulated memory samples")
	}

	diskSamples, err := c.GetDiskUsage("any-node")
	if err != nil {
		t.Fatalf("simulated disk should not error: %v", err)
	}
	if len(diskSamples) == 0 {
		t.Error("expected simulated disk samples")
	}

	gpuSamples, err := c.GetGPUUtilization()
	if err != nil {
		t.Fatalf("simulated GPU should not error: %v", err)
	}
	if len(gpuSamples) == 0 {
		t.Error("expected simulated GPU samples")
	}
}

func TestQuery_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	_, err := c.Query(`up`, time.Now())
	if err == nil {
		t.Error("expected error from server error response")
	}
}

func TestGetMetricNames_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	_, err := c.GetMetricNames()
	if err == nil {
		t.Error("expected error from bad response")
	}
}

func formatTS(t time.Time) string {
	return t.Format(time.RFC3339)
}
