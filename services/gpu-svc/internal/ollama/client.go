package ollama

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

const (
	modeReal       = "real"
	modeSimulated  = "simulated"
	defaultTimeout = 30 * time.Second
)

// ollamaTagsResponse represents the Ollama /api/tags response.
type ollamaTagsResponse struct {
	Models []ollamaModel `json:"models"`
}

// ollamaModel represents a model in the Ollama API response.
type ollamaModel struct {
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	Digest     string    `json:"digest"`
	ModifiedAt time.Time `json:"modified_at"`
	Details    struct {
		Format            string   `json:"format"`
		Family            string   `json:"family"`
		Families          []string `json:"families"`
		ParameterSize     string   `json:"parameter_size"`
		QuantizationLevel string   `json:"quantization_level"`
	} `json:"details"`
}

// ollamaPullRequest represents a pull request to the Ollama API.
type ollamaPullRequest struct {
	Name   string `json:"name"`
	Stream bool   `json:"stream"`
}

// ollamaDeleteRequest represents a delete request to the Ollama API.
type ollamaDeleteRequest struct {
	Name string `json:"name"`
}

// ollamaChatRequest represents a chat request to the Ollama API.
type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
}

// ollamaMessage represents a chat message.
type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ollamaChatResponse represents a chat response from the Ollama API.
type ollamaChatResponse struct {
	Message ollamaMessage `json:"message"`
	Done    bool          `json:"done"`
}

// Client manages interactions with Ollama for model serving.
// It automatically detects whether an Ollama server is available and
// falls back to simulated mode when it is not reachable.
type Client struct {
	mu         sync.RWMutex
	baseURL    string
	httpClient *http.Client
	mode       string
	simulated  map[string]*models.GPUModel
	now        func() time.Time
}

// NewClient creates a new Ollama client. It auto-detects whether
// the Ollama server is reachable and selects the appropriate mode.
func NewClient(baseURL string, timeout time.Duration) *Client {
	if timeout == 0 {
		timeout = defaultTimeout
	}
	c := &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: timeout},
		mode:       "",
		simulated:  make(map[string]*models.GPUModel),
		now:        time.Now,
	}
	return c
}

// detectMode determines whether to use real or simulated mode.
func (c *Client) detectMode() {
	if c.mode != "" {
		return
	}
	if c.isReachable() {
		c.mode = modeReal
	} else {
		c.mode = modeSimulated
	}
}

// isReachable checks if the Ollama server is reachable.
func (c *Client) isReachable() bool {
	resp, err := c.httpClient.Get(fmt.Sprintf("%s/api/tags", c.baseURL))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// IsAvailable returns true if the Ollama server is reachable.
func (c *Client) IsAvailable() bool {
	return c.isReachable()
}

// Mode returns the current operating mode ("real" or "simulated").
func (c *Client) Mode() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	c.detectMode()
	return c.mode
}

// ListModels returns all available models.
func (c *Client) ListModels() ([]Model, error) {
	c.mu.Lock()
	c.detectMode()
	c.mu.Unlock()

	if c.mode == modeReal {
		return c.listModelsReal()
	}
	return c.listModelsSimulated()
}

// listModelsReal fetches models from the Ollama API.
func (c *Client) listModelsReal() ([]Model, error) {
	resp, err := c.httpClient.Get(fmt.Sprintf("%s/api/tags", c.baseURL))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOllamaUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama /api/tags returned status %d", resp.StatusCode)
	}

	var result ollamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode /api/tags response: %w", err)
	}

	models := make([]Model, 0, len(result.Models))
	for _, m := range result.Models {
		models = append(models, Model{
			Name:           m.Name,
			SizeBytes:      m.Size,
			ParameterCount: m.Details.ParameterSize,
			Quantized:      m.Details.QuantizationLevel != "",
			LastPulled:     m.ModifiedAt,
		})
	}
	return models, nil
}

// listModelsSimulated returns models from the simulated cache.
func (c *Client) listModelsSimulated() ([]Model, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	models := make([]Model, 0, len(c.simulated))
	for _, m := range c.simulated {
		models = append(models, Model{
			Name:           m.Name,
			SizeBytes:      m.SizeBytes,
			ParameterCount: m.ParameterCount,
			Quantized:      m.Quantized,
			Serving:        m.Serving,
			Port:           m.Port,
			NodeID:         m.NodeID,
			Replicas:       m.Replicas,
			LastPulled:     m.LastPulled,
		})
	}
	return models, nil
}

// PullModel pulls a model, making it available for serving.
func (c *Client) PullModel(name string) error {
	if name == "" {
		return errors.New("model name is required")
	}

	c.mu.Lock()
	c.detectMode()
	c.mu.Unlock()

	if c.mode == modeReal {
		return c.pullModelReal(name)
	}
	return c.pullModelSimulated(name)
}

// pullModelReal sends a pull request to the Ollama API.
func (c *Client) pullModelReal(name string) error {
	body := ollamaPullRequest{Name: name, Stream: false}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal pull request: %w", err)
	}

	resp, err := c.httpClient.Post(
		fmt.Sprintf("%s/api/pull", c.baseURL),
		"application/json",
		bytes.NewReader(payload),
	)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrOllamaUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ollama /api/pull returned status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// pullModelSimulated adds a model to the simulated cache.
func (c *Client) pullModelSimulated(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.simulated[name]; ok {
		c.simulated[name].LastPulled = c.now()
		return nil
	}

	c.simulated[name] = &models.GPUModel{
		Name:           name,
		SizeBytes:      EstimateModelSize(name),
		ParameterCount: EstimateParams(name),
		Quantized:      true,
		Serving:        false,
		Replicas:       0,
		LastPulled:     c.now(),
	}
	return nil
}

// RemoveModel removes a model.
func (c *Client) RemoveModel(name string) error {
	c.mu.Lock()
	c.detectMode()
	c.mu.Unlock()

	if c.mode == modeReal {
		return c.removeModelReal(name)
	}
	return c.removeModelSimulated(name)
}

// removeModelReal sends a delete request to the Ollama API.
func (c *Client) removeModelReal(name string) error {
	body := ollamaDeleteRequest{Name: name}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal delete request: %w", err)
	}

	req, err := http.NewRequest(http.MethodDelete,
		fmt.Sprintf("%s/api/delete", c.baseURL),
		bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create delete request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrOllamaUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ollama /api/delete returned status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// removeModelSimulated removes a model from the simulated cache.
func (c *Client) removeModelSimulated(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.simulated[name]; !ok {
		return ErrModelNotFound
	}
	delete(c.simulated, name)
	return nil
}

// ServeModel marks a model as being served on a specific port.
func (c *Client) ServeModel(name string, port int) error {
	if name == "" {
		return errors.New("model name is required")
	}

	c.mu.Lock()
	c.detectMode()
	c.mu.Unlock()

	if c.mode == modeReal {
		return c.serveModelReal(name, port)
	}
	return c.serveModelSimulated(name, port)
}

// serveModelReal verifies the model exists in Ollama.
func (c *Client) serveModelReal(name string, port int) error {
	models, err := c.listModelsReal()
	if err != nil {
		return err
	}
	for _, m := range models {
		if m.Name == name {
			return nil
		}
	}
	return ErrModelNotFound
}

// serveModelSimulated marks a model as serving in the simulated cache.
func (c *Client) serveModelSimulated(name string, port int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	model, ok := c.simulated[name]
	if !ok {
		return ErrModelNotFound
	}

	model.Serving = true
	model.Port = port
	model.LastPulled = c.now()
	return nil
}

// StopModel stops serving a model.
func (c *Client) StopModel(name string) error {
	c.mu.Lock()
	c.detectMode()
	c.mu.Unlock()

	if c.mode == modeReal {
		return nil
	}
	return c.stopModelSimulated(name)
}

// stopModelSimulated marks a model as not serving in the simulated cache.
func (c *Client) stopModelSimulated(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	model, ok := c.simulated[name]
	if !ok {
		return ErrModelNotFound
	}
	model.Serving = false
	model.Replicas = 0
	model.Port = 0
	model.NodeID = ""
	return nil
}

// GetModelStatus returns the current status of a model.
func (c *Client) GetModelStatus(name string) (ModelStatus, error) {
	c.mu.Lock()
	c.detectMode()
	c.mu.Unlock()

	if c.mode == modeReal {
		return c.getModelStatusReal(name)
	}
	return c.getModelStatusSimulated(name)
}

// getModelStatusReal checks if a model exists in Ollama.
func (c *Client) getModelStatusReal(name string) (ModelStatus, error) {
	models, err := c.listModelsReal()
	if err != nil {
		return ModelStatus{}, err
	}
	for _, m := range models {
		if m.Name == name {
			return ModelStatus{Name: name, Serving: true}, nil
		}
	}
	return ModelStatus{}, ErrModelNotFound
}

// getModelStatusSimulated returns the status from the simulated cache.
func (c *Client) getModelStatusSimulated(name string) (ModelStatus, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	model, ok := c.simulated[name]
	if !ok {
		return ModelStatus{}, ErrModelNotFound
	}
	return ModelStatus{Name: name, Serving: model.Serving}, nil
}

// Chat sends a chat message to the specified model and returns the response.
func (c *Client) Chat(model, message string) (string, error) {
	if model == "" {
		return "", errors.New("model name is required")
	}
	if message == "" {
		return "", errors.New("message is required")
	}

	c.mu.Lock()
	c.detectMode()
	c.mu.Unlock()

	if c.mode == modeReal {
		return c.chatReal(model, message)
	}
	return c.chatSimulated(model, message)
}

// chatReal sends a chat request to the Ollama API.
func (c *Client) chatReal(model, message string) (string, error) {
	body := ollamaChatRequest{
		Model: model,
		Messages: []ollamaMessage{
			{Role: "user", Content: message},
		},
		Stream: false,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("failed to marshal chat request: %w", err)
	}

	resp, err := c.httpClient.Post(
		fmt.Sprintf("%s/api/chat", c.baseURL),
		"application/json",
		bytes.NewReader(payload),
	)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrOllamaUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama /api/chat returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result ollamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode chat response: %w", err)
	}
	return result.Message.Content, nil
}

// chatSimulated returns a simulated chat response.
func (c *Client) chatSimulated(model, message string) (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if _, ok := c.simulated[model]; !ok {
		return "", ErrModelNotFound
	}
	return fmt.Sprintf("[simulated response from %s]: acknowledged %q", model, message), nil
}

// CheckHealth checks if the Ollama server is reachable.
// Deprecated: use IsAvailable instead.
func (c *Client) CheckHealth() error {
	if !c.IsAvailable() {
		return ErrOllamaUnavailable
	}
	return nil
}

// Model represents an Ollama model with its metadata.
type Model struct {
	Name           string    `json:"name"`
	SizeBytes      int64     `json:"size_bytes"`
	ParameterCount string    `json:"parameter_count"`
	Quantized      bool      `json:"quantized"`
	Serving        bool      `json:"serving"`
	Port           int       `json:"port,omitempty"`
	NodeID         string    `json:"node_id,omitempty"`
	Replicas       int       `json:"replicas"`
	LastPulled     time.Time `json:"last_pulled"`
}

// ModelStatus represents the serving status of a model.
type ModelStatus struct {
	Name    string `json:"name"`
	Serving bool   `json:"serving"`
}

// EstimateModelSize returns estimated model size in bytes based on name heuristics.
func EstimateModelSize(name string) int64 {
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

// EstimateParams returns estimated parameter count as string.
func EstimateParams(name string) string {
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
