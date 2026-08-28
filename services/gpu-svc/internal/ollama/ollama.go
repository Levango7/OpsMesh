package ollama

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/models"
)

// ErrModelNotFound is returned when a model does not exist.
var ErrModelNotFound = errors.New("model not found")

// ErrModelAlreadyExists is returned when a model is already being served.
var ErrModelAlreadyExists = errors.New("model already exists")

// ErrOllamaUnavailable is returned when the Ollama server is unreachable.
var ErrOllamaUnavailable = errors.New("ollama server unavailable")

// Client manages interactions with Ollama for model serving.
type Client struct {
	mu      sync.RWMutex
	baseURL string
	client  *http.Client
	models  map[string]*models.GPUModel
	now     func() time.Time
}

// NewClient creates a new Ollama client.
func NewClient(baseURL string, timeout time.Duration) *Client {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		baseURL: baseURL,
		client:  &http.Client{Timeout: timeout},
		models:  make(map[string]*models.GPUModel),
		now:     time.Now,
	}
}

// PullModel simulates pulling/syncing a model from Ollama.
func (c *Client) PullModel(name string) (*models.GPUModel, error) {
	if name == "" {
		return nil, errors.New("model name is required")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if existing, ok := c.models[name]; ok {
		existing.LastPulled = c.now()
		cp := *existing
		return &cp, nil
	}

	model := &models.GPUModel{
		Name:       name,
		SizeBytes:  estimateModelSize(name),
		ParameterCount: estimateParams(name),
		Quantized:  true,
		Serving:    false,
		Replicas:   0,
		LastPulled: c.now(),
	}

	c.models[name] = model
	cp := *model
	return &cp, nil
}

// ListModels returns all available models.
func (c *Client) ListModels() []*models.GPUModel {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]*models.GPUModel, 0, len(c.models))
	for _, m := range c.models {
		cp := *m
		out = append(out, &cp)
	}
	return out
}

// RemoveModel removes a model from the local cache.
func (c *Client) RemoveModel(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.models[name]; !ok {
		return ErrModelNotFound
	}
	delete(c.models, name)
	return nil
}

// ServeModel starts serving a model on the specified node.
func (c *Client) ServeModel(name string, nodeID string, port int, replicas int) (*models.GPUModel, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	model, ok := c.models[name]
	if !ok {
		return nil, ErrModelNotFound
	}

	model.Serving = true
	model.NodeID = nodeID
	model.Port = port
	model.Replicas = replicas
	model.LastPulled = c.now()

	cp := *model
	return &cp, nil
}

// GetModelStatus returns the serving status of a model.
func (c *Client) GetModelStatus(name string) (*models.GPUModel, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	model, ok := c.models[name]
	if !ok {
		return nil, ErrModelNotFound
	}
	cp := *model
	return &cp, nil
}

// StopServing stops serving a model.
func (c *Client) StopServing(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	model, ok := c.models[name]
	if !ok {
		return ErrModelNotFound
	}
	model.Serving = false
	model.Replicas = 0
	model.Port = 0
	model.NodeID = ""
	return nil
}

// CheckHealth checks if the Ollama server is reachable.
func (c *Client) CheckHealth() error {
	resp, err := c.client.Get(fmt.Sprintf("%s/api/tags", c.baseURL))
	if err != nil {
		return ErrOllamaUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama health check failed with status %d", resp.StatusCode)
	}
	return nil
}

// estimateModelSize returns estimated model size in bytes based on name heuristics.
func estimateModelSize(name string) int64 {
	switch {
	case contains(name, "70b"):
		return 40e9
	case contains(name, "13b"):
		return 8e9
	case contains(name, "7b"):
		return 4e9
	case contains(name, "3b"):
		return 2e9
	default:
		return 4e9
	}
}

// estimateParams returns estimated parameter count as string.
func estimateParams(name string) string {
	switch {
	case contains(name, "70b"):
		return "70B"
	case contains(name, "13b"):
		return "13B"
	case contains(name, "7b"):
		return "7B"
	case contains(name, "3b"):
		return "3B"
	default:
		return "7B"
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
