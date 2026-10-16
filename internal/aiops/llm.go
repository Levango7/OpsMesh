// Package aiops 实现 AIOps 引擎：故障根因分析 + 智能修复生成 + 自然语言运维。
//
// 设计原则：
//   - 零外部 AI SDK 依赖：通过标准 net/http 调用 OpenAI 兼容接口，支持 Kilo Gateway。
//   - 纯领域模型 + 可注入 LLM 客户端，便于单元测试（mock LLM 即可全覆盖）。
//   - 复用 internal/proto 的 Task/Device/Alert/Metric 类型，与控制面 Store 无反向依赖。
//
// 架构：
//
//	LLM ← Analyzer (alert+log+metric+topology → root cause) → Remediation (root cause → task/DAG)
//	LLM ← NLParser (natural language → structured task)
package aiops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Provider LLM 提供商类型。
type Provider string

const (
	ProviderKilo      Provider = "kilo"      // Kilo Gateway (默认)
	ProviderOpenAI    Provider = "openai"    // OpenAI 兼容 API
	ProviderAnthropic Provider = "anthropic" // Anthropic 兼容 API (未来扩展)
)

// Config AIOps 配置（来自 --aiops-* flags 或 OPSMESH_AIOPS_* env）。
type Config struct {
	Enabled     bool     `json:"enabled"`
	Provider    Provider `json:"provider"`
	Model       string   `json:"model"`       // 模型 ID，如 qvac/qwen3.5-0.8b
	BaseURL     string   `json:"baseURL"`     // API 端点
	APIKey      string   `json:"-"`           // 密钥（不序列化）
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
	if c.Timeout <= 0 {
		c.Timeout = 30
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

// Chat 实现 LLM 接口：调用 OpenAI 兼容聊天接口。
func (c *HTTPClient) Chat(ctx context.Context, req ChatRequest) (*LLMResponse, error) {
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("aiops: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/chat/completions", c.cfg.BaseURL)
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

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("aiops: LLM returned %d: %s", resp.StatusCode, string(body))
	}

	// 解析 OpenAI 兼容响应
	var raw struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage Usage `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("aiops: decode response: %w", err)
	}

	if len(raw.Choices) == 0 {
		return nil, fmt.Errorf("aiops: empty response from LLM")
	}

	return &LLMResponse{
		Content: raw.Choices[0].Message.Content,
		Usage:   raw.Usage,
	}, nil
}
