// Package aigateway 提供统一的 LLM 网关层，支持多厂商适配器、模型路由、熔断、限流、计费、缓存。
package aigateway

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Role 消息角色
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message 对话消息
type Message struct {
	Role       Role   `json:"role"`
	Content    string `json:"content"`
	Name       string `json:"name,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// ToolCall 工具调用
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // function
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// Tool 工具定义
type Tool struct {
	Type     string `json:"type"` // function
	Function struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description"`
		Parameters  map[string]interface{} `json:"parameters"`
	} `json:"function"`
}

// CompletionRequest LLM 补全请求
type CompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []Tool    `json:"tools,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	TopP        float64   `json:"top_p,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

// CompletionResponse LLM 补全响应
type CompletionResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int      `json:"index"`
		Message      Message  `json:"message"`
		ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
		FinishReason string   `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// LLMProvider LLM 提供商接口
type LLMProvider interface {
	Name() string
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
	Embed(ctx context.Context, text string) ([]float64, error)
	ListModels() []string
	HealthCheck(ctx context.Context) error
}

// ProviderConfig 提供商配置
type ProviderConfig struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"` // openai/anthropic/ollama/dashscope/deepseek
	APIKey      string            `json:"apiKey"`
	BaseURL     string            `json:"baseUrl"`
	Models      []string          `json:"models"`
	MaxTokens   int               `json:"maxTokens"`
	RateLimit   int               `json:"rateLimit"` // RPM
	CostPer1K   map[string]float64 `json:"costPer1K"` // model -> $/1K tokens
}

// ModelRoute 模型路由规则
type ModelRoute struct {
	TaskType   string `json:"taskType"`   // classify/reason/chat/code
	Provider   string `json:"provider"`   // 提供商名称
	Model      string `json:"model"`      // 模型名称
	MaxTokens  int    `json:"maxTokens"`  // 最大 token
	Priority   int    `json:"priority"`   // 优先级
}

// Gateway 统一 LLM 网关
type Gateway struct {
	mu         sync.RWMutex
	providers  map[string]LLMProvider
	configs    map[string]ProviderConfig
	routes     []ModelRoute
	breakers   map[string]*CircuitBreaker
	rateLimits map[string]*RateLimiter
	tokenUsage *TokenUsageTracker
	cache      *SemanticCache
}

func NewGateway() *Gateway {
	return &Gateway{
		providers:  make(map[string]LLMProvider),
		configs:    make(map[string]ProviderConfig),
		breakers:   make(map[string]*CircuitBreaker),
		rateLimits: make(map[string]*RateLimiter),
		tokenUsage: NewTokenUsageTracker(),
		cache:      NewSemanticCache(1000),
	}
}

// RegisterProvider 注册 LLM 提供商
func (g *Gateway) RegisterProvider(cfg ProviderConfig, provider LLMProvider) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.providers[cfg.Name] = provider
	g.configs[cfg.Name] = cfg
	g.breakers[cfg.Name] = NewCircuitBreaker(5, 30*time.Second)
	g.rateLimits[cfg.Name] = NewRateLimiter(cfg.RateLimit)
}

// SetRoutes 设置模型路由规则
func (g *Gateway) SetRoutes(routes []ModelRoute) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.routes = routes
}

// Complete 执行 LLM 补全（带路由/熔断/限流/缓存/计费）
func (g *Gateway) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	// 1. 语义缓存检查
	if cached := g.cache.Get(req); cached != nil {
		return cached, nil
	}

	// 2. 模型路由
	provider, model, err := g.route(req.Model)
	if err != nil {
		return nil, err
	}
	req.Model = model

	// 3. 限流检查
	if err := g.rateLimits[provider].Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit exceeded: %w", err)
	}

	// 4. 熔断检查
	if !g.breakers[provider].Allow() {
		return nil, fmt.Errorf("circuit breaker open for provider %s", provider)
	}

	// 5. 执行请求
	resp, err := g.providers[provider].Complete(ctx, req)
	if err != nil {
		g.breakers[provider].RecordFailure()
		return nil, err
	}
	g.breakers[provider].RecordSuccess()

	// 6. 记录 Token 用量
	g.tokenUsage.Record(provider, model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)

	// 7. 缓存结果
	g.cache.Set(req, resp)

	return resp, nil
}

// Embed 生成文本嵌入向量
func (g *Gateway) Embed(ctx context.Context, provider string, text string) ([]float64, error) {
	p, ok := g.providers[provider]
	if !ok {
		return nil, fmt.Errorf("provider %s not found", provider)
	}
	return p.Embed(ctx, text)
}

// route 根据模型名或任务类型选择提供商
func (g *Gateway) route(model string) (string, string, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// 直接匹配模型名
	for name, cfg := range g.configs {
		for _, m := range cfg.Models {
			if m == model {
				return name, m, nil
			}
		}
	}

	// 路由规则匹配
	for _, route := range g.routes {
		if route.Model == model {
			return route.Provider, route.Model, nil
		}
	}

	// 默认：第一个可用提供商
	for name := range g.providers {
		return name, model, nil
	}

	return "", "", fmt.Errorf("no provider available for model %s", model)
}

// HealthCheck 检查所有提供商健康状态
func (g *Gateway) HealthCheck(ctx context.Context) map[string]bool {
	g.mu.RLock()
	defer g.mu.RUnlock()

	result := make(map[string]bool)
	for name, p := range g.providers {
		err := p.HealthCheck(ctx)
		result[name] = err == nil
	}
	return result
}

// GetTokenUsage 获取 Token 用量统计
func (g *Gateway) GetTokenUsage() *TokenUsageReport {
	return g.tokenUsage.Report()
}

// ListProviders 列出所有已注册提供商
func (g *Gateway) ListProviders() []ProviderConfig {
	g.mu.RLock()
	defer g.mu.RUnlock()

	out := make([]ProviderConfig, 0, len(g.configs))
	for _, cfg := range g.configs {
		out = append(out, cfg)
	}
	return out
}
