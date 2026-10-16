package aiops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Provider LLM 提供商类型。
type Provider string

const (
	ProviderKilo     Provider = "kilo"      // Kilo Gateway (默认)
	ProviderOpenAI   Provider = "openai"    // OpenAI 兼容 API
	ProviderAnthropic Provider = "anthropic" // Anthropic 兼容 API (未来扩展)
)

// Config AIOps 配置（来自 --aiops-* flags 或 OPSMESH_AIOPS_* env）。
type Config struct {
	Enabled     bool     `json:"enabled"`
	Provider    Provider `json:"provider"`
	Model       string   `json:"model"`      // 模型 ID，如 qvac/qwen3.5-0.8b
	BaseURL     string   `json:"baseURL"`    // API 端点
	APIKey      string   `json:"-"`          // 密钥（不序列化）
	Temperature float64  `json:"temperature"` // 默认 0.3
	MaxTokens   int      `json:"maxTokens"`   // 默认 4096
	Timeout     int      `json:"timeout"`     // 秒，默认 30
}

// DefaultConfig 返回默认 AIOps 配置。
func DefaultConfig() Config {
	return Config{
		Enabled:     false,
		Provider:    ProviderKilo,
		Model:       "qvac/qwen3.5-0.8b",
		BaseURL:     "https://api.kilo.ai/v1",
		Temperature: 0.3,
		MaxTokens:   4096,
		Timeout:     30,
	}
}

// Validate 校验配置合法性。
func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.Model == "" {
		return fmt.Errorf("aiops: model is required when enabled")
	}
	if c.BaseURL == "" {
		return fmt.Errorf("aiops: base_url is required when enabled")
	}
	if c.Temperature < 0 || c.Temperature > 2 {
		return fmt.Errorf("aiops: temperature must be between 0 and 2, got %f", c.Temperature)
	}
	if c.MaxTokens > 32768 {
		return fmt.Errorf("aiops: maxTokens must not exceed 32768, got %d", c.MaxTokens)
	}
	if c.Timeout < 0 {
		return fmt.Errorf("aiops: timeout must not be negative, got %d", c.Timeout)
	}
	return nil
}

// Message 聊天消息（OpenAI 兼容格式）。
type Message struct {
	Role    string `json:"role"`    // system / user / assistant
	Content string `json:"content"` // plain text
}

// LLMResponse LLM 返回结果。
type LLMResponse struct {
	Content string `json:"content"`
	Usage   Usage  `json:"usage,omitempty"`
}

// Usage token 使用量。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatRequest 发送给 LLM 的聊天请求（OpenAI 兼容）。
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens"`
	Stream      bool      `json:"stream,omitempty"`
}

// LLM 客户端接口 —— 可 mock 用于测试。
type LLM interface {
	// Chat 发送聊天请求，返回 LLM 响应。
	Chat(ctx context.Context, req ChatRequest) (*LLMResponse, error)
}

// HTTPClient LLM HTTP 客户端（OpenAI 兼容 API）。
type HTTPClient struct {
	cfg    Config
	client *http.Client
}

// NewHTTPClient 构造 LLM HTTP 客户端。
func NewHTTPClient(cfg Config) *HTTPClient {
	return &HTTPClient{
		cfg: cfg,
		client: &http.Client{
			Timeout: time.Duration(cfg.Timeout) * time.Second,
		},
	}
}

// Chat 实现 LLM 接口：调用 OpenAI 兼容聊天接口，带重试。
func (c *HTTPClient) Chat(ctx context.Context, req ChatRequest) (*LLMResponse, error) {
	var lastErr error
	const maxRetries = 2

	for attempt := 0; attempt <= maxRetries; attempt++ {
		resp, err := c.doChat(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err

		// 只对可重试错误（网络、5xx、429）重试
		if !isRetryable(err) {
			return nil, err
		}
		if attempt < maxRetries {
			backoff := time.Duration(1<<attempt) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}
	}
	return nil, fmt.Errorf("aiops: LLM call failed after %d retries: %w", maxRetries, lastErr)
}

// doChat 执行单次 LLM 调用。
func (c *HTTPClient) doChat(ctx context.Context, req ChatRequest) (*LLMResponse, error) {
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("aiops: marshal request: %w", err)
	}

	url := strings.TrimSuffix(c.cfg.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("aiops: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("aiops: call LLM: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("aiops: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("aiops: LLM returned %d: %s", resp.StatusCode, string(body))
	}

	// 解析 OpenAI 兼容响应
	var raw struct {
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage Usage `json:"usage"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("aiops: decode response: %w", err)
	}

	if len(raw.Choices) == 0 {
		return nil, fmt.Errorf("aiops: empty response from LLM")
	}

	content := strings.TrimSpace(raw.Choices[0].Message.Content)
	if content == "" {
		return nil, fmt.Errorf("aiops: LLM returned empty content")
	}

	return &LLMResponse{
		Content: content,
		Usage:   raw.Usage,
	}, nil
}

// isRetryable 判断错误是否可重试（网络错误、429、5xx）。
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// 网络连接错误
	if strings.Contains(msg, "connection") ||
		strings.Contains(msg, "EOF") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "i/o timeout") {
		return true
	}
	// 429 Too Many Requests / 5xx
	for _, code := range []string{"429", "500", "502", "503", "504"} {
		if strings.Contains(msg, code) {
			return true
		}
	}
	return false
}

// BuildSystemMessages 构造带系统角色的消息列表。
// system 提示词固定角色定义，user 是具体任务描述。
func BuildSystemMessages(systemPrompt, userPrompt string) []Message {
	msgs := []Message{}
	if systemPrompt != "" {
		msgs = append(msgs, Message{Role: "system", Content: systemPrompt})
	}
	msgs = append(msgs, Message{Role: "user", Content: userPrompt})
	return msgs
}

// SanitizeJSON 从 LLM 返回中提取 JSON（去除 Markdown 代码块包装）。
func SanitizeJSON(content string) string {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```JSON")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	return strings.TrimSpace(content)
}
