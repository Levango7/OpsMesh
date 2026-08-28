package ollama

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestClient(url string) *Client {
	return NewClient(url, 5*time.Second)
}

func TestIsAvailableReachable(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(ollamaTagsResponse{Models: []ollamaModel{}})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	c := newTestClient(ts.URL)
	if !c.IsAvailable() {
		t.Error("expected IsAvailable to return true for reachable server")
	}
}

func TestIsAvailableUnreachable(t *testing.T) {
	c := newTestClient("http://invalid-host:9999")
	c.httpClient.Timeout = 100 * time.Millisecond
	if c.IsAvailable() {
		t.Error("expected IsAvailable to return false for unreachable server")
	}
}

func TestListModelsReal(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(ollamaTagsResponse{
			Models: []ollamaModel{
				{
					Name:       "llama3:8b",
					Size:       4661224676,
					Digest:     "abc123",
					ModifiedAt: time.Now(),
				},
				{
					Name:       "mistral:7b",
					Size:       4108917376,
					Digest:     "def456",
					ModifiedAt: time.Now(),
				},
			},
		})
	}))
	defer ts.Close()

	c := newTestClient(ts.URL)
	models, err := c.ListModels()
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}
	if models[0].Name != "llama3:8b" {
		t.Errorf("expected llama3:8b, got %s", models[0].Name)
	}
	if models[0].SizeBytes != 4661224676 {
		t.Errorf("expected size 4661224676, got %d", models[0].SizeBytes)
	}
}

func TestListModelsServerError(t *testing.T) {
	requestCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(ollamaTagsResponse{Models: []ollamaModel{}})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	c := newTestClient(ts.URL)
	_, err := c.ListModels()
	if err == nil {
		t.Fatal("expected error for server error response")
	}
}

func TestListModelsInvalidJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "invalid json")
	}))
	defer ts.Close()

	c := newTestClient(ts.URL)
	_, err := c.ListModels()
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestPullModelReal(t *testing.T) {
	pullReceived := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(ollamaTagsResponse{Models: []ollamaModel{}})
			return
		}
		if r.URL.Path == "/api/pull" {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			var req ollamaPullRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("failed to decode request: %v", err)
			}
			if req.Name != "llama3:8b" {
				t.Errorf("expected name llama3:8b, got %s", req.Name)
			}
			if req.Stream {
				t.Error("expected stream=false")
			}
			pullReceived = true
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	c := newTestClient(ts.URL)
	if err := c.PullModel("llama3:8b"); err != nil {
		t.Fatalf("PullModel failed: %v", err)
	}
	if !pullReceived {
		t.Error("expected pull request to be received by server")
	}
}

func TestPullModelEmptyName(t *testing.T) {
	c := newTestClient("http://localhost:11434")
	err := c.PullModel("")
	if err == nil {
		t.Fatal("expected error for empty model name")
	}
}

func TestPullModelServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(ollamaTagsResponse{Models: []ollamaModel{}})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "model not found")
	}))
	defer ts.Close()

	c := newTestClient(ts.URL)
	err := c.PullModel("nonexistent")
	if err == nil {
		t.Fatal("expected error for server error response")
	}
}

func TestRemoveModelReal(t *testing.T) {
	deleteReceived := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(ollamaTagsResponse{Models: []ollamaModel{}})
			return
		}
		if r.URL.Path == "/api/delete" {
			if r.Method != http.MethodDelete {
				t.Errorf("expected DELETE, got %s", r.Method)
			}
			var req ollamaDeleteRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("failed to decode request: %v", err)
			}
			if req.Name != "llama3:8b" {
				t.Errorf("expected name llama3:8b, got %s", req.Name)
			}
			deleteReceived = true
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	c := newTestClient(ts.URL)
	if err := c.RemoveModel("llama3:8b"); err != nil {
		t.Fatalf("RemoveModel failed: %v", err)
	}
	if !deleteReceived {
		t.Error("expected delete request to be received by server")
	}
}

func TestRemoveModelNotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(ollamaTagsResponse{Models: []ollamaModel{}})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "model not found")
	}))
	defer ts.Close()

	c := newTestClient(ts.URL)
	err := c.RemoveModel("nonexistent")
	if err == nil {
		t.Fatal("expected error for model not found")
	}
}

func TestServeModelReal(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(ollamaTagsResponse{
			Models: []ollamaModel{
				{Name: "llama3:8b", Size: 4661224676},
			},
		})
	}))
	defer ts.Close()

	c := newTestClient(ts.URL)
	if err := c.ServeModel("llama3:8b", 8080); err != nil {
		t.Fatalf("ServeModel failed: %v", err)
	}
}

func TestServeModelNotFoundReal(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(ollamaTagsResponse{Models: []ollamaModel{}})
	}))
	defer ts.Close()

	c := newTestClient(ts.URL)
	err := c.ServeModel("nonexistent", 8080)
	if err != ErrModelNotFound {
		t.Fatalf("expected ErrModelNotFound, got: %v", err)
	}
}

func TestStopModelReal(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(ollamaTagsResponse{Models: []ollamaModel{}})
	}))
	defer ts.Close()

	c := newTestClient(ts.URL)
	if err := c.StopModel("llama3:8b"); err != nil {
		t.Fatalf("StopModel failed: %v", err)
	}
}

func TestGetModelStatusReal(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(ollamaTagsResponse{
			Models: []ollamaModel{
				{Name: "llama3:8b", Size: 4661224676},
			},
		})
	}))
	defer ts.Close()

	c := newTestClient(ts.URL)
	status, err := c.GetModelStatus("llama3:8b")
	if err != nil {
		t.Fatalf("GetModelStatus failed: %v", err)
	}
	if status.Name != "llama3:8b" {
		t.Errorf("expected name llama3:8b, got %s", status.Name)
	}
	if !status.Serving {
		t.Error("expected model to be serving")
	}
}

func TestGetModelStatusNotFoundReal(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(ollamaTagsResponse{Models: []ollamaModel{}})
	}))
	defer ts.Close()

	c := newTestClient(ts.URL)
	_, err := c.GetModelStatus("nonexistent")
	if err != ErrModelNotFound {
		t.Fatalf("expected ErrModelNotFound, got: %v", err)
	}
}

func TestChatReal(t *testing.T) {
	chatReceived := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(ollamaTagsResponse{Models: []ollamaModel{}})
			return
		}
		if r.URL.Path == "/api/chat" {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			var req ollamaChatRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("failed to decode request: %v", err)
			}
			if req.Model != "llama3:8b" {
				t.Errorf("expected model llama3:8b, got %s", req.Model)
			}
			if len(req.Messages) != 1 || req.Messages[0].Role != "user" {
				t.Error("expected single user message")
			}
			if req.Messages[0].Content != "Hello" {
				t.Errorf("expected message 'Hello', got %q", req.Messages[0].Content)
			}
			chatReceived = true
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(ollamaChatResponse{
				Message: ollamaMessage{Role: "assistant", Content: "Hi there!"},
				Done:    true,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	c := newTestClient(ts.URL)
	resp, err := c.Chat("llama3:8b", "Hello")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if !chatReceived {
		t.Error("expected chat request to be received by server")
	}
	if resp != "Hi there!" {
		t.Errorf("expected 'Hi there!', got %q", resp)
	}
}

func TestChatEmptyModel(t *testing.T) {
	c := newTestClient("http://localhost:11434")
	_, err := c.Chat("", "Hello")
	if err == nil {
		t.Fatal("expected error for empty model name")
	}
}

func TestChatEmptyMessage(t *testing.T) {
	c := newTestClient("http://localhost:11434")
	_, err := c.Chat("llama3", "")
	if err == nil {
		t.Fatal("expected error for empty message")
	}
}

func TestChatServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(ollamaTagsResponse{Models: []ollamaModel{}})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	c := newTestClient(ts.URL)
	_, err := c.Chat("llama3:8b", "Hello")
	if err == nil {
		t.Fatal("expected error for server error")
	}
}

func TestCheckHealthDeprecated(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(ollamaTagsResponse{Models: []ollamaModel{}})
	}))
	defer ts.Close()

	c := newTestClient(ts.URL)
	if err := c.CheckHealth(); err != nil {
		t.Fatalf("CheckHealth failed: %v", err)
	}
}

func TestCheckHealthUnreachable(t *testing.T) {
	c := newTestClient("http://invalid-host:9999")
	c.httpClient.Timeout = 100 * time.Millisecond
	err := c.CheckHealth()
	if err != ErrOllamaUnavailable {
		t.Fatalf("expected ErrOllamaUnavailable, got: %v", err)
	}
}

func TestModeDetection(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(ollamaTagsResponse{Models: []ollamaModel{}})
	}))
	defer ts.Close()

	c := newTestClient(ts.URL)
	mode := c.Mode()
	if mode != modeReal {
		t.Errorf("expected mode %s, got %s", modeReal, mode)
	}
}

func TestModeDetectionSimulated(t *testing.T) {
	c := newTestClient("http://invalid-host:9999")
	c.httpClient.Timeout = 100 * time.Millisecond
	mode := c.Mode()
	if mode != modeSimulated {
		t.Errorf("expected mode %s, got %s", modeSimulated, mode)
	}
}

func TestListModelsSimulated(t *testing.T) {
	c := newTestClient("http://invalid-host:9999")
	c.httpClient.Timeout = 100 * time.Millisecond

	_ = c.PullModel("llama3:8b")
	_ = c.PullModel("mistral:7b")

	models, err := c.ListModels()
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}
}

func TestPullModelIdempotentSimulated(t *testing.T) {
	c := newTestClient("http://invalid-host:9999")
	c.httpClient.Timeout = 100 * time.Millisecond

	if err := c.PullModel("llama3:8b"); err != nil {
		t.Fatalf("first PullModel failed: %v", err)
	}
	if err := c.PullModel("llama3:8b"); err != nil {
		t.Fatalf("second PullModel failed: %v", err)
	}

	models, _ := c.ListModels()
	if len(models) != 1 {
		t.Errorf("expected 1 model after idempotent pull, got %d", len(models))
	}
}

func TestRemoveModelSimulated(t *testing.T) {
	c := newTestClient("http://invalid-host:9999")
	c.httpClient.Timeout = 100 * time.Millisecond

	_ = c.PullModel("test-model")
	if err := c.RemoveModel("test-model"); err != nil {
		t.Fatalf("RemoveModel failed: %v", err)
	}

	models, _ := c.ListModels()
	if len(models) != 0 {
		t.Errorf("expected 0 models after remove, got %d", len(models))
	}
}

func TestRemoveModelNotFoundSimulated(t *testing.T) {
	c := newTestClient("http://invalid-host:9999")
	c.httpClient.Timeout = 100 * time.Millisecond

	err := c.RemoveModel("nonexistent")
	if err != ErrModelNotFound {
		t.Fatalf("expected ErrModelNotFound, got: %v", err)
	}
}

func TestServeModelSimulated(t *testing.T) {
	c := newTestClient("http://invalid-host:9999")
	c.httpClient.Timeout = 100 * time.Millisecond

	_ = c.PullModel("llama3:8b")
	if err := c.ServeModel("llama3:8b", 8080); err != nil {
		t.Fatalf("ServeModel failed: %v", err)
	}

	status, err := c.GetModelStatus("llama3:8b")
	if err != nil {
		t.Fatalf("GetModelStatus failed: %v", err)
	}
	if !status.Serving {
		t.Error("expected model to be serving")
	}
}

func TestServeModelNotFoundSimulated(t *testing.T) {
	c := newTestClient("http://invalid-host:9999")
	c.httpClient.Timeout = 100 * time.Millisecond

	err := c.ServeModel("nonexistent", 8080)
	if err != ErrModelNotFound {
		t.Fatalf("expected ErrModelNotFound, got: %v", err)
	}
}

func TestStopModelSimulated(t *testing.T) {
	c := newTestClient("http://invalid-host:9999")
	c.httpClient.Timeout = 100 * time.Millisecond

	_ = c.PullModel("llama3:8b")
	_ = c.ServeModel("llama3:8b", 8080)
	if err := c.StopModel("llama3:8b"); err != nil {
		t.Fatalf("StopModel failed: %v", err)
	}

	status, _ := c.GetModelStatus("llama3:8b")
	if status.Serving {
		t.Error("expected model to be stopped")
	}
}

func TestChatSimulated(t *testing.T) {
	c := newTestClient("http://invalid-host:9999")
	c.httpClient.Timeout = 100 * time.Millisecond

	_ = c.PullModel("llama3:8b")
	resp, err := c.Chat("llama3:8b", "Hello")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if !strings.Contains(resp, "simulated response") {
		t.Errorf("expected simulated response, got %q", resp)
	}
}

func TestChatModelNotFoundSimulated(t *testing.T) {
	c := newTestClient("http://invalid-host:9999")
	c.httpClient.Timeout = 100 * time.Millisecond

	_, err := c.Chat("nonexistent", "Hello")
	if err != ErrModelNotFound {
		t.Fatalf("expected ErrModelNotFound, got: %v", err)
	}
}

func TestEstimateModelSizeExported(t *testing.T) {
	tests := []struct {
		name    string
		minSize int64
	}{
		{"llama3.1:70b", 30e9},
		{"llama3.1:8b", 3e9},
		{"mistral:13b", 5e9},
		{"phi3:3b", 1e9},
	}
	for _, tt := range tests {
		size := EstimateModelSize(tt.name)
		if size < tt.minSize {
			t.Errorf("expected %s size >= %d, got %d", tt.name, tt.minSize, size)
		}
	}
}

func TestEstimateParamsExported(t *testing.T) {
	params := EstimateParams("llama3.1:70b")
	if params != "70B" {
		t.Errorf("expected 70B, got %s", params)
	}
}

func TestContains(t *testing.T) {
	if !contains("llama3:7b", "7b") {
		t.Error("expected contains '7b'")
	}
	if contains("llama3:8b", "70b") {
		t.Error("expected not contains '70b'")
	}
}

func TestNewClientDefaultTimeout(t *testing.T) {
	c := NewClient("http://localhost:11434", 0)
	if c.httpClient.Timeout != defaultTimeout {
		t.Errorf("expected default timeout %v, got %v", defaultTimeout, c.httpClient.Timeout)
	}
}
