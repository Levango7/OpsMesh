package aiops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestHTTPClientChatWithMockServer 使用 httptest 模拟 LLM 服务器，验证请求/响应格式。
func TestHTTPClientChatWithMockServer(t *testing.T) {
	// 模拟 LLM 服务器：验证请求体，返回固定响应
	var capturedReq ChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if err := json.Unmarshal(body, &capturedReq); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		// 验证请求头
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Authorization") != "Bearer test-key-123" {
			t.Errorf("Authorization = %q, want Bearer test-key-123", r.Header.Get("Authorization"))
		}

		// 返回 OpenAI 兼容响应
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": `{"category":"软件","component":"nginx","description":"nginx 进程异常退出","confidence":0.9,"evidence":["进程不存在"]}`,
					},
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     120,
				"completion_tokens": 80,
				"total_tokens":      200,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfg := Config{
		Enabled:     true,
		Provider:    ProviderKilo,
		Model:       "qvac/qwen3.5-0.8b",
		BaseURL:     srv.URL,
		APIKey:      "test-key-123",
		Temperature: 0.3,
		MaxTokens:   4096,
		Timeout:     30,
	}
	client := NewHTTPClient(cfg)

	resp, err := client.Chat(context.Background(), ChatRequest{
		Model:       cfg.Model,
		Messages:    []Message{{Role: "user", Content: "分析告警"}},
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxTokens,
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	// 验证请求被正确转发
	if capturedReq.Model != cfg.Model {
		t.Errorf("request Model = %q, want %q", capturedReq.Model, cfg.Model)
	}
	if len(capturedReq.Messages) != 1 || capturedReq.Messages[0].Content != "分析告警" {
		t.Errorf("request Messages = %+v, want [{user 分析告警}]", capturedReq.Messages)
	}
	if capturedReq.Temperature != cfg.Temperature {
		t.Errorf("request Temperature = %v, want %v", capturedReq.Temperature, cfg.Temperature)
	}
	if capturedReq.MaxTokens != cfg.MaxTokens {
		t.Errorf("request MaxTokens = %d, want %d", capturedReq.MaxTokens, cfg.MaxTokens)
	}

	// 验证响应被正确解析
	if resp.Content == "" {
		t.Errorf("response Content should not be empty")
	}
	if resp.Usage.TotalTokens != 200 {
		t.Errorf("response Usage.TotalTokens = %d, want 200", resp.Usage.TotalTokens)
	}
}

// TestHTTPClientChatWithMockServerError 处理 LLM 返回非 200 的情况。
func TestHTTPClientChatWithMockServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid API key","type":"auth_error"}}`))
	}))
	defer srv.Close()

	cfg := Config{
		Enabled:  true,
		Provider: ProviderKilo,
		Model:    "qwen",
		BaseURL:  srv.URL,
		APIKey:   "bad-key",
		Timeout:  30,
	}
	client := NewHTTPClient(cfg)

	_, err := client.Chat(context.Background(), ChatRequest{
		Model:    cfg.Model,
		Messages: []Message{{Role: "user", Content: "test"}},
	})
	if err == nil {
		t.Errorf("expected error for 401 response")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should contain 401, got: %v", err)
	}
}

// TestHTTPClientReal 使用真实 API Key 进行端到端验证。
// 需要设置环境变量 OPSMESH_AIOPS_API_KEY 和 OPSMESH_AIOPS_BASE_URL 才会运行；
// 否则跳过（Skip）。
func TestHTTPClientReal(t *testing.T) {
	apiKey := os.Getenv("OPSMESH_AIOPS_API_KEY")
	baseURL := os.Getenv("OPSMESH_AIOPS_BASE_URL")
	model := os.Getenv("OPSMESH_AIOPS_MODEL")
	if model == "" {
		model = "qvac/qwen3.5-0.8b"
	}

	if apiKey == "" || baseURL == "" {
		t.Skip("skipping real LLM test: set OPSMESH_AIOPS_API_KEY and OPSMESH_AIOPS_BASE_URL to enable")
	}

	cfg := Config{
		Enabled:     true,
		Provider:    ProviderKilo,
		Model:       model,
		BaseURL:     baseURL,
		APIKey:      apiKey,
		Temperature: 0.3,
		MaxTokens:   1024,
		Timeout:     30,
	}
	client := NewHTTPClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Timeout)*time.Second)
	defer cancel()

	resp, err := client.Chat(ctx, ChatRequest{
		Model:       model,
		Messages:    []Message{{Role: "user", Content: "你是谁？请用一句话回答。"}},
		Temperature: 0.3,
		MaxTokens:   1024,
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content == "" {
		t.Errorf("response Content should not be empty")
	}
	fmt.Printf("LLM response: %s\n", resp.Content)
	fmt.Printf("token usage: prompt=%d completion=%d total=%d\n",
		resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
}
