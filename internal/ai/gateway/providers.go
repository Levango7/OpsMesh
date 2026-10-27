package aigateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OpenAIProvider OpenAI LLM 提供商适配器
type OpenAIProvider struct {
	name    string
	apiKey  string
	baseURL string
	models  []string
	client  *http.Client
}

func NewOpenAIProvider(name, apiKey, baseURL string, models []string) *OpenAIProvider {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &OpenAIProvider{
		name:    name,
		apiKey:  apiKey,
		baseURL: baseURL,
		models:  models,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *OpenAIProvider) Name() string { return p.name }

func (p *OpenAIProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("openai: HTTP %d: %s", resp.StatusCode, string(data))
	}

	var result CompletionResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *OpenAIProvider) Embed(ctx context.Context, text string) ([]float64, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"input": text,
		"model": "text-embedding-3-small",
	})
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai embed: read body: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("openai embed: HTTP %d: %s", resp.StatusCode, string(data))
	}

	var result struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("openai embed: no data returned")
	}
	return result.Data[0].Embedding, nil
}

func (p *OpenAIProvider) ListModels() []string { return p.models }

func (p *OpenAIProvider) HealthCheck(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/models", nil)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("openai health: HTTP %d", resp.StatusCode)
	}
	return nil
}

// AnthropicProvider Anthropic LLM 提供商适配器
type AnthropicProvider struct {
	name    string
	apiKey  string
	baseURL string
	models  []string
	client  *http.Client
}

func NewAnthropicProvider(name, apiKey, baseURL string, models []string) *AnthropicProvider {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	return &AnthropicProvider{
		name:    name,
		apiKey:  apiKey,
		baseURL: baseURL,
		models:  models,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *AnthropicProvider) Name() string { return p.name }

func (p *AnthropicProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	// Anthropic API 格式转换
	anthropicReq := map[string]interface{}{
		"model":      req.Model,
		"max_tokens": req.MaxTokens,
		"messages":   req.Messages,
	}
	if req.Temperature > 0 {
		anthropicReq["temperature"] = req.Temperature
	}
	if req.TopP > 0 {
		anthropicReq["top_p"] = req.TopP
	}

	body, _ := json.Marshal(anthropicReq)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: read body: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("anthropic: HTTP %d: %s", resp.StatusCode, string(data))
	}

	// 转换 Anthropic 响应为统一格式
	var anthropicResp struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		StopReason string `json:"stop_reason"`
	}
	if err := json.Unmarshal(data, &anthropicResp); err != nil {
		return nil, err
	}

	resp2 := &CompletionResponse{
		ID:    anthropicResp.ID,
		Model: anthropicResp.Model,
	}
	resp2.Choices = append(resp2.Choices, struct {
		Index        int        `json:"index"`
		Message      Message    `json:"message"`
		ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
		FinishReason string     `json:"finish_reason"`
	}{
		Index: 0,
		Message: Message{
			Role: RoleAssistant,
			Content: func() string {
				var text string
				for _, c := range anthropicResp.Content {
					if c.Type == "text" {
						text += c.Text
					}
				}
				return text
			}(),
		},
		FinishReason: anthropicResp.StopReason,
	})
	resp2.Usage.PromptTokens = anthropicResp.Usage.InputTokens
	resp2.Usage.CompletionTokens = anthropicResp.Usage.OutputTokens
	resp2.Usage.TotalTokens = anthropicResp.Usage.InputTokens + anthropicResp.Usage.OutputTokens

	return resp2, nil
}

func (p *AnthropicProvider) Embed(ctx context.Context, text string) ([]float64, error) {
	// Anthropic 不提供 embedding API，返回错误
	return nil, fmt.Errorf("anthropic: embedding not supported")
}

func (p *AnthropicProvider) ListModels() []string { return p.models }

func (p *AnthropicProvider) HealthCheck(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/models", nil)
	if err != nil {
		return err
	}
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("anthropic health: HTTP %d", resp.StatusCode)
	}
	return nil
}

// OllamaProvider Ollama 本地 LLM 提供商适配器
type OllamaProvider struct {
	name    string
	baseURL string
	models  []string
	client  *http.Client
}

func NewOllamaProvider(name, baseURL string, models []string) *OllamaProvider {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &OllamaProvider{
		name:    name,
		baseURL: baseURL,
		models:  models,
		client:  &http.Client{Timeout: 300 * time.Second}, // 本地推理可能较慢
	}
}

func (p *OllamaProvider) Name() string { return p.name }

func (p *OllamaProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	ollamaReq := map[string]interface{}{
		"model":    req.Model,
		"messages": req.Messages,
		"stream":   false,
	}
	if req.MaxTokens > 0 {
		ollamaReq["options"] = map[string]interface{}{
			"num_predict": req.MaxTokens,
		}
	}
	if req.Temperature > 0 {
		if opts, ok := ollamaReq["options"].(map[string]interface{}); ok {
			opts["temperature"] = req.Temperature
		} else {
			ollamaReq["options"] = map[string]interface{}{"temperature": req.Temperature}
		}
	}

	body, _ := json.Marshal(ollamaReq)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ollama: read body: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("ollama: HTTP %d: %s", resp.StatusCode, string(data))
	}

	var ollamaResp struct {
		Model   string `json:"model"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		Done            bool `json:"done"`
		PromptEvalCount int  `json:"prompt_eval_count"`
		EvalCount       int  `json:"eval_count"`
	}
	if err := json.Unmarshal(data, &ollamaResp); err != nil {
		return nil, err
	}

	resp2 := &CompletionResponse{
		Model: ollamaResp.Model,
	}
	resp2.Choices = append(resp2.Choices, struct {
		Index        int        `json:"index"`
		Message      Message    `json:"message"`
		ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
		FinishReason string     `json:"finish_reason"`
	}{
		Index: 0,
		Message: Message{
			Role:    RoleAssistant,
			Content: ollamaResp.Message.Content,
		},
		FinishReason: "stop",
	})
	resp2.Usage.PromptTokens = ollamaResp.PromptEvalCount
	resp2.Usage.CompletionTokens = ollamaResp.EvalCount
	resp2.Usage.TotalTokens = ollamaResp.PromptEvalCount + ollamaResp.EvalCount

	return resp2, nil
}

func (p *OllamaProvider) Embed(ctx context.Context, text string) ([]float64, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"model":  "nomic-embed-text",
		"prompt": text,
	})
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: read body: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("ollama embed: HTTP %d: %s", resp.StatusCode, string(data))
	}

	var result struct {
		Embedding []float64 `json:"embedding"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result.Embedding, nil
}

func (p *OllamaProvider) ListModels() []string { return p.models }

func (p *OllamaProvider) HealthCheck(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/api/tags", nil)
	if err != nil {
		return err
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("ollama health: HTTP %d", resp.StatusCode)
	}
	return nil
}

// DashScopeProvider 阿里云 DashScope (通义千问) 提供商适配器
type DashScopeProvider struct {
	name    string
	apiKey  string
	baseURL string
	models  []string
	client  *http.Client
}

func NewDashScopeProvider(name, apiKey, baseURL string, models []string) *DashScopeProvider {
	if baseURL == "" {
		baseURL = "https://dashscope.aliyuncs.com/api/v1"
	}
	return &DashScopeProvider{
		name:    name,
		apiKey:  apiKey,
		baseURL: baseURL,
		models:  models,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *DashScopeProvider) Name() string { return p.name }

func (p *DashScopeProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	dashReq := map[string]interface{}{
		"model": req.Model,
		"input": map[string]interface{}{
			"messages": req.Messages,
		},
		"parameters": map[string]interface{}{
			"max_tokens":    req.MaxTokens,
			"temperature":   req.Temperature,
			"top_p":         req.TopP,
			"result_format": "message",
		},
	}

	body, _ := json.Marshal(dashReq)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/services/aigc/text-generation/generation", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("dashscope: read body: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("dashscope: HTTP %d: %s", resp.StatusCode, string(data))
	}

	var dashResp struct {
		Output struct {
			Choices []struct {
				Message struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		} `json:"output"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(data, &dashResp); err != nil {
		return nil, err
	}

	resp2 := &CompletionResponse{
		ID:    dashResp.RequestID,
		Model: req.Model,
	}
	for i, c := range dashResp.Output.Choices {
		resp2.Choices = append(resp2.Choices, struct {
			Index        int        `json:"index"`
			Message      Message    `json:"message"`
			ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
			FinishReason string     `json:"finish_reason"`
		}{
			Index: i,
			Message: Message{
				Role:    RoleAssistant,
				Content: c.Message.Content,
			},
			FinishReason: c.FinishReason,
		})
	}
	resp2.Usage.PromptTokens = dashResp.Usage.InputTokens
	resp2.Usage.CompletionTokens = dashResp.Usage.OutputTokens
	resp2.Usage.TotalTokens = dashResp.Usage.TotalTokens

	return resp2, nil
}

func (p *DashScopeProvider) Embed(ctx context.Context, text string) ([]float64, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"model": "text-embedding-v3",
		"input": map[string]interface{}{
			"texts": []string{text},
		},
	})
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/services/embeddings/text-embedding/text-embedding", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("dashscope embed: read body: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("dashscope embed: HTTP %d: %s", resp.StatusCode, string(data))
	}

	var result struct {
		Output struct {
			Embeddings []struct {
				Embedding []float64 `json:"embedding"`
			} `json:"embeddings"`
		} `json:"output"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if len(result.Output.Embeddings) == 0 {
		return nil, fmt.Errorf("dashscope embed: no data returned")
	}
	return result.Output.Embeddings[0].Embedding, nil
}

func (p *DashScopeProvider) ListModels() []string { return p.models }

func (p *DashScopeProvider) HealthCheck(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/models", nil)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("dashscope health: HTTP %d", resp.StatusCode)
	}
	return nil
}
