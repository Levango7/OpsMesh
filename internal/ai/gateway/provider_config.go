package aigateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
)

// ParseProviderConfigs 解析 OPSMESH_AI_GATEWAY_PROVIDERS 的 JSON 数组。
//
// 空串表示不配置 Provider，保持接线前的显式「no provider available」行为。
// 非空配置必须完整有效：未知类型、重复名称、缺少密钥/模型或非法 base URL
// 都返回错误，由控制面启动时 fail-fast；不能静默回退成另一个厂商端点。
func ParseProviderConfigs(raw string) ([]ProviderConfig, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var configs []ProviderConfig
	if err := dec.Decode(&configs); err != nil {
		return nil, fmt.Errorf("invalid AI gateway providers JSON: %w", err)
	}
	if err := ensureJSONEOF(dec); err != nil {
		return nil, fmt.Errorf("invalid AI gateway providers JSON: %w", err)
	}
	if len(configs) == 0 {
		return nil, fmt.Errorf("AI gateway providers must not be an empty array; use an empty value to disable providers")
	}

	seen := make(map[string]struct{}, len(configs))
	for i := range configs {
		cfg := &configs[i]
		cfg.Type = strings.ToLower(strings.TrimSpace(cfg.Type))
		cfg.Name = strings.TrimSpace(cfg.Name)
		if cfg.Name == "" {
			cfg.Name = cfg.Type
		}
		if cfg.Name == "" {
			return nil, fmt.Errorf("AI gateway provider %d: type is required", i)
		}
		if _, exists := seen[cfg.Name]; exists {
			return nil, fmt.Errorf("AI gateway provider %q: duplicate name", cfg.Name)
		}
		seen[cfg.Name] = struct{}{}

		switch cfg.Type {
		case "openai", "anthropic", "dashscope":
			if strings.TrimSpace(cfg.APIKey) == "" {
				return nil, fmt.Errorf("AI gateway provider %q: apiKey is required for %s", cfg.Name, cfg.Type)
			}
		case "ollama":
			// 本地/私网推理通常没有 API key。
		default:
			return nil, fmt.Errorf("AI gateway provider %q: unsupported type %q", cfg.Name, cfg.Type)
		}
		if err := validateProviderBaseURL(cfg.BaseURL); err != nil {
			return nil, fmt.Errorf("AI gateway provider %q: %w", cfg.Name, err)
		}

		models := make([]string, 0, len(cfg.Models))
		for _, model := range cfg.Models {
			if model = strings.TrimSpace(model); model != "" {
				models = append(models, model)
			}
		}
		if len(models) == 0 {
			return nil, fmt.Errorf("AI gateway provider %q: models must not be empty", cfg.Name)
		}
		cfg.Models = models
	}
	return configs, nil
}

func ensureJSONEOF(dec *json.Decoder) error {
	var extra interface{}
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected value after providers array")
		}
		return err
	}
	return nil
}

// validateProviderBaseURL 与各 Provider 构造器的 SSRF 入口约束一致：
// 仅允许绝对 http/https URL 且不得携带 userinfo。构造器保留默认值兜底，
// 运维显式配置则在这里严格校验，避免拼写错误被静默换成默认厂商。
func validateProviderBaseURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return fmt.Errorf("baseUrl must be an absolute http/https URL without userinfo")
	}
	return nil
}

// NewProvider 按配置构造真实 Provider。此前四种适配器只有测试直接构造，
// 生产路径没有工厂与注册调用，chat/embed/RAG 因零 Provider 恒不可用。
func NewProvider(cfg ProviderConfig) (LLMProvider, error) {
	switch cfg.Type {
	case "openai":
		return NewOpenAIProvider(cfg.Name, cfg.APIKey, cfg.BaseURL, cfg.Models), nil
	case "anthropic":
		return NewAnthropicProvider(cfg.Name, cfg.APIKey, cfg.BaseURL, cfg.Models), nil
	case "ollama":
		return NewOllamaProvider(cfg.Name, cfg.BaseURL, cfg.Models), nil
	case "dashscope":
		return NewDashScopeProvider(cfg.Name, cfg.APIKey, cfg.BaseURL, cfg.Models), nil
	default:
		return nil, fmt.Errorf("unsupported AI gateway provider type %q", cfg.Type)
	}
}
