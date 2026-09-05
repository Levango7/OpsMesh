package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/metrics"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/node"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/ollama"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/quota"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/scheduler"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/workload"
)

// newTestHandler 构建基于内存实现的完整 handler（防回归锚专用）。
func newTestHandler() *Handler {
	svc := service.NewService(
		node.NewManager(nil),
		scheduler.NewScheduler(nil),
		workload.NewManager(nil),
		ollama.NewClient("http://localhost:11434", 0),
		quota.NewManager(nil),
		metrics.NewCollector(nil),
	)
	h := NewHandler(svc)
	return h
}

// TestMetricsQueryForm 防回归：前端契约 GET /api/v1/gpu/metrics?nodeId={id}&range={r}
// 必须命中精确路径（非尾斜杠子树），nodeId 取 query 参数，响应形状为 {metrics: [...]}。
func TestMetricsQueryForm(t *testing.T) {
	h := newTestHandler()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/gpu/metrics?nodeId=node-1&range=1h", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Metrics []struct {
			NodeID string `json:"node_id"`
		} `json:"metrics"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Metrics) != 1 {
		t.Fatalf("expected 1 metrics element, got %d", len(resp.Metrics))
	}
	if resp.Metrics[0].NodeID != "node-1" {
		t.Fatalf("expected node_id node-1, got %s", resp.Metrics[0].NodeID)
	}
}

// TestMetricsPathFormCompat 防回归：旧路径形态 /api/v1/gpu/metrics/{nodeID} 保留兼容。
func TestMetricsPathFormCompat(t *testing.T) {
	h := newTestHandler()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/gpu/metrics/node-legacy", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, ok := resp["metrics"]; !ok {
		t.Fatal("expected 'metrics' key in response")
	}
}

// TestCreateWorkloadCamelCase 防回归：前端契约 POST /api/v1/gpu/workloads
// 发 {name,type,model,gpuCount,nodeId}（camelCase），禁止零值 GPURequest 入库。
func TestCreateWorkloadCamelCase(t *testing.T) {
	h := newTestHandler()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"name":"llm-train","type":"training","model":"llama3:8b","gpuCount":2,"nodeId":"node-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gpu/workloads", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var created struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		ModelName  string `json:"model_name"`
		GPURequest struct {
			Count int `json:"count"`
		} `json:"gpu_request"`
		NodeIDs []string `json:"node_ids"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if created.GPURequest.Count != 2 {
		t.Fatalf("expected gpu_request.count 2, got %d", created.GPURequest.Count)
	}
	if created.ModelName != "llama3:8b" {
		t.Fatalf("expected model_name llama3:8b, got %s", created.ModelName)
	}
	if len(created.NodeIDs) != 1 || created.NodeIDs[0] != "node-1" {
		t.Fatalf("expected node_ids [node-1], got %v", created.NodeIDs)
	}
}

// TestCreateWorkloadSnakeCaseCompat 防回归：旧 snake_case 形态（含 gpu_request/tenant_id）仍可用。
func TestCreateWorkloadSnakeCaseCompat(t *testing.T) {
	h := newTestHandler()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"name":"train","tenant_id":"t1","gpu_request":{"count":1,"min_vram_mb":40960}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gpu/workloads", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		TenantID   string `json:"tenant_id"`
		GPURequest struct {
			Count     int `json:"count"`
			MinVRAMMB int `json:"min_vram_mb"`
		} `json:"gpu_request"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if created.TenantID != "t1" || created.GPURequest.Count != 1 || created.GPURequest.MinVRAMMB != 40960 {
		t.Fatalf("snake_case fields lost: %+v", created)
	}
}

// TestPullModelNodeIdKept 防回归：前端契约 POST /api/v1/gpu/models 发 {name,nodeId}，
// nodeId 必须带回返回体，禁止丢弃。
func TestPullModelNodeIdKept(t *testing.T) {
	h := newTestHandler()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"name":"llama3:8b","nodeId":"node-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gpu/models", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Name   string `json:"name"`
		NodeID string `json:"node_id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if created.NodeID != "node-1" {
		t.Fatalf("expected node_id node-1, got %q", created.NodeID)
	}
}
