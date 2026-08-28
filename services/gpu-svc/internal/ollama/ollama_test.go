package ollama

import (
	"testing"
	"time"
)

func TestPullModel(t *testing.T) {
	c := NewClient("http://localhost:11434", 5*time.Second)
	model, err := c.PullModel("llama3.1:8b")
	if err != nil {
		t.Fatalf("PullModel failed: %v", err)
	}
	if model.Name != "llama3.1:8b" {
		t.Errorf("expected name llama3.1:8b, got %s", model.Name)
	}
	if model.SizeBytes == 0 {
		t.Error("expected non-zero size")
	}
}

func TestPullModelEmptyName(t *testing.T) {
	c := NewClient("http://localhost:11434", 5*time.Second)
	_, err := c.PullModel("")
	if err == nil {
		t.Fatal("expected error for empty model name")
	}
}

func TestPullModelIdempotent(t *testing.T) {
	c := NewClient("http://localhost:11434", 5*time.Second)
	m1, _ := c.PullModel("mistral:7b")
	m2, _ := c.PullModel("mistral:7b")
	if m1.Name != m2.Name {
		t.Error("expected same model name on duplicate pull")
	}
}

func TestListModels(t *testing.T) {
	c := NewClient("http://localhost:11434", 5*time.Second)
	c.PullModel("model-a")
	c.PullModel("model-b")

	models := c.ListModels()
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}
}

func TestRemoveModel(t *testing.T) {
	c := NewClient("http://localhost:11434", 5*time.Second)
	c.PullModel("test-model")

	if err := c.RemoveModel("test-model"); err != nil {
		t.Fatalf("RemoveModel failed: %v", err)
	}

	models := c.ListModels()
	if len(models) != 0 {
		t.Errorf("expected 0 models after remove, got %d", len(models))
	}
}

func TestRemoveModelNotFound(t *testing.T) {
	c := NewClient("http://localhost:11434", 5*time.Second)
	err := c.RemoveModel("nonexistent")
	if err != ErrModelNotFound {
		t.Fatalf("expected ErrModelNotFound, got: %v", err)
	}
}

func TestServeModel(t *testing.T) {
	c := NewClient("http://localhost:11434", 5*time.Second)
	c.PullModel("llama3:8b")

	model, err := c.ServeModel("llama3:8b", "node-1", 8080, 2)
	if err != nil {
		t.Fatalf("ServeModel failed: %v", err)
	}
	if !model.Serving {
		t.Error("expected model to be serving")
	}
	if model.NodeID != "node-1" {
		t.Errorf("expected node-1, got %s", model.NodeID)
	}
	if model.Port != 8080 {
		t.Errorf("expected port 8080, got %d", model.Port)
	}
	if model.Replicas != 2 {
		t.Errorf("expected 2 replicas, got %d", model.Replicas)
	}
}

func TestServeModelNotFound(t *testing.T) {
	c := NewClient("http://localhost:11434", 5*time.Second)
	_, err := c.ServeModel("nonexistent", "node-1", 8080, 1)
	if err != ErrModelNotFound {
		t.Fatalf("expected ErrModelNotFound, got: %v", err)
	}
}

func TestGetModelStatus(t *testing.T) {
	c := NewClient("http://localhost:11434", 5*time.Second)
	c.PullModel("phi3:3.8b")
	c.ServeModel("phi3:3.8b", "node-1", 9090, 1)

	status, err := c.GetModelStatus("phi3:3.8b")
	if err != nil {
		t.Fatalf("GetModelStatus failed: %v", err)
	}
	if !status.Serving {
		t.Error("expected model to be serving")
	}
}

func TestGetModelStatusNotFound(t *testing.T) {
	c := NewClient("http://localhost:11434", 5*time.Second)
	_, err := c.GetModelStatus("nonexistent")
	if err != ErrModelNotFound {
		t.Fatalf("expected ErrModelNotFound, got: %v", err)
	}
}

func TestStopServing(t *testing.T) {
	c := NewClient("http://localhost:11434", 5*time.Second)
	c.PullModel("qwen2:7b")
	c.ServeModel("qwen2:7b", "node-1", 8080, 1)

	if err := c.StopServing("qwen2:7b"); err != nil {
		t.Fatalf("StopServing failed: %v", err)
	}

	status, _ := c.GetModelStatus("qwen2:7b")
	if status.Serving {
		t.Error("expected model to be stopped")
	}
}

func TestEstimateModelSize(t *testing.T) {
	tests := []struct {
		name     string
		minSize  int64
	}{
		{"llama3.1:70b", 30e9},
		{"llama3.1:8b", 3e9},
		{"mistral:13b", 5e9},
		{"phi3:3b", 1e9},
	}
	for _, tt := range tests {
		size := estimateModelSize(tt.name)
		if size < tt.minSize {
			t.Errorf("expected %s size >= %d, got %d", tt.name, tt.minSize, size)
		}
	}
}

func TestEstimateParams(t *testing.T) {
	params := estimateParams("llama3.1:70b")
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

func TestCheckHealth(t *testing.T) {
	c := NewClient("http://invalid-host:9999", 1*time.Millisecond)
	err := c.CheckHealth()
	if err == nil {
		t.Error("expected error for unreachable Ollama")
	}
}
