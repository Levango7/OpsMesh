package aigateway

import (
	"strings"
	"testing"
)

func TestParseProviderConfigsEmptyIsBackwardCompatible(t *testing.T) {
	for _, raw := range []string{"", "  \n\t"} {
		configs, err := ParseProviderConfigs(raw)
		if err != nil || configs != nil {
			t.Fatalf("空配置应返回 (nil, nil): raw=%q got (%v, %v)", raw, configs, err)
		}
	}
}

func TestParseProviderConfigsNormalizesAndConstructs(t *testing.T) {
	raw := `[
		{"type":"openai","apiKey":"test-only","models":[" m1 ","","m2"]},
		{"name":"local","type":"ollama","baseUrl":"http://127.0.0.1:11434","models":["qwen"]}
	]`
	configs, err := ParseProviderConfigs(raw)
	if err != nil {
		t.Fatalf("ParseProviderConfigs: %v", err)
	}
	if len(configs) != 2 || configs[0].Name != "openai" || configs[1].Name != "local" {
		t.Fatalf("名称归一化错误: %+v", configs)
	}
	if len(configs[0].Models) != 2 || configs[0].Models[0] != "m1" {
		t.Fatalf("模型应 trim 并去空项: %+v", configs[0].Models)
	}
	for _, cfg := range configs {
		provider, err := NewProvider(cfg)
		if err != nil || provider == nil || provider.Name() != cfg.Name {
			t.Fatalf("NewProvider(%s) = (%v, %v)", cfg.Type, provider, err)
		}
	}
}

func TestNewProviderAllSupportedTypes(t *testing.T) {
	cases := []string{"openai", "anthropic", "ollama", "dashscope"}
	for _, typ := range cases {
		cfg := ProviderConfig{Name: "p-" + typ, Type: typ, APIKey: "test-only", Models: []string{"m"}}
		provider, err := NewProvider(cfg)
		if err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		switch typ {
		case "openai":
			if _, ok := provider.(*OpenAIProvider); !ok {
				t.Fatalf("openai 构造类型 = %T", provider)
			}
		case "anthropic":
			if _, ok := provider.(*AnthropicProvider); !ok {
				t.Fatalf("anthropic 构造类型 = %T", provider)
			}
		case "ollama":
			if _, ok := provider.(*OllamaProvider); !ok {
				t.Fatalf("ollama 构造类型 = %T", provider)
			}
		case "dashscope":
			if _, ok := provider.(*DashScopeProvider); !ok {
				t.Fatalf("dashscope 构造类型 = %T", provider)
			}
		}
	}
	if _, err := NewProvider(ProviderConfig{Type: "deepseek"}); err == nil {
		t.Fatal("未实现的 deepseek 不应被静默当成其他 Provider")
	}
}

func TestParseProviderConfigsRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{"json", `{`, "invalid AI gateway providers JSON"},
		{"trailing", `[] {}`, "invalid AI gateway providers JSON"},
		{"empty array", `[]`, "empty array"},
		{"unknown field", `[{"type":"ollama","models":["m"],"typo":true}]`, "unknown field"},
		{"missing type", `[{"models":["m"]}]`, "type is required"},
		{"unknown type", `[{"type":"deepseek","models":["m"]}]`, "unsupported type"},
		{"missing key", `[{"type":"openai","models":["m"]}]`, "apiKey is required"},
		{"missing models", `[{"type":"ollama"}]`, "models must not be empty"},
		{"duplicate", `[{"type":"ollama","models":["m"]},{"name":"ollama","type":"ollama","models":["n"]}]`, "duplicate name"},
		{"relative URL", `[{"type":"ollama","baseUrl":"/v1","models":["m"]}]`, "baseUrl"},
		{"userinfo URL", `[{"type":"ollama","baseUrl":"http://user@example.invalid/v1","models":["m"]}]`, "baseUrl"},
		{"non HTTP URL", `[{"type":"ollama","baseUrl":"file:///tmp/model","models":["m"]}]`, "baseUrl"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseProviderConfigs(tc.raw)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("错误应包含 %q: got %v", tc.wantErr, err)
			}
		})
	}
}
