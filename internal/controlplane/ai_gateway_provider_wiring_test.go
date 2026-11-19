package controlplane

import (
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
)

func TestPrepareAIGatewayOptionsEmptyKeepsLegacyBehavior(t *testing.T) {
	opts, err := prepareAIGatewayOptions(&config.Config{})
	if err != nil {
		t.Fatalf("空配置不应报错: %v", err)
	}
	if opts.embedProvider != "openai" || len(opts.configs) != 0 {
		t.Fatalf("空配置默认应 openai 且零 Provider: %+v", opts)
	}
	if got := len(budgetedGateway(opts).ListProviders()); got != 0 {
		t.Fatalf("空配置注册数 = %d, want 0", got)
	}
}

func TestPrepareAIGatewayOptionsRegistersProviders(t *testing.T) {
	cfg := &config.Config{AIGatewayProviders: `[
		{"name":"local","type":"ollama","baseUrl":"http://127.0.0.1:11434","models":["qwen"]},
		{"name":"cloud","type":"openai","apiKey":"test-only","models":["gpt-test"]}
	]`, AIGatewayEmbedProvider: "local"}
	opts, err := prepareAIGatewayOptions(cfg)
	if err != nil {
		t.Fatalf("prepareAIGatewayOptions: %v", err)
	}
	// 四个消费方都从同一 options 建 Gateway；此处逐一重复构造，证明不是只接 AI handler。
	for i := 0; i < 4; i++ {
		providers := budgetedGateway(opts).ListProviders()
		if len(providers) != 2 {
			t.Fatalf("第 %d 个 Gateway 注册数 = %d, want 2", i+1, len(providers))
		}
	}
	if newBudgetedAIGatewayHandler(opts) == nil || newBudgetedAgentHandler(opts) == nil || newBudgetedKnowledgeHandler(opts) == nil {
		t.Fatal("AI handler / agent / knowledge 必须都能带 Provider 配置构造")
	}
}

func TestPrepareAIGatewayOptionsSingleProviderDefaultsEmbed(t *testing.T) {
	opts, err := prepareAIGatewayOptions(&config.Config{
		AIGatewayProviders: `[{"name":"local","type":"ollama","models":["qwen"]}]`,
	})
	if err != nil {
		t.Fatalf("prepareAIGatewayOptions: %v", err)
	}
	if opts.embedProvider != "local" {
		t.Fatalf("单 Provider 时 embed 默认 = %q, want local", opts.embedProvider)
	}
}

func TestPrepareAIGatewayOptionsFailFast(t *testing.T) {
	cases := []struct {
		name    string
		cfg     *config.Config
		wantErr string
	}{
		{"bad JSON", &config.Config{AIGatewayProviders: `{`}, "invalid AI gateway providers JSON"},
		{"embed without providers", &config.Config{AIGatewayEmbedProvider: "local"}, "requires at least one"},
		{"unknown embed", &config.Config{
			AIGatewayProviders:     `[{"name":"local","type":"ollama","models":["m"]}]`,
			AIGatewayEmbedProvider: "missing",
		}, "is not configured"},
		{"ambiguous default", &config.Config{AIGatewayProviders: `[
			{"name":"a","type":"ollama","models":["m"]},
			{"name":"b","type":"ollama","models":["m"]}
		]`}, "\"openai\" is not configured"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := prepareAIGatewayOptions(tc.cfg)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("错误应包含 %q: got %v", tc.wantErr, err)
			}
		})
	}
}

func TestNewServerFailsFastOnInvalidAIGatewayProviders(t *testing.T) {
	_, err := NewServer(&config.Config{
		Production: true, Mode: "controlplane", Store: "memory", EventBus: "noop",
		TaskMaxRetries: 3, GRPCPort: 0, HTTPPort: 0, MetricsPort: 0,
		AIGatewayProviders: `{`,
	})
	if err == nil || !strings.Contains(err.Error(), "AI Gateway Provider 装配失败") {
		t.Fatalf("非法 Provider 配置必须让真实启动 fail-fast: %v", err)
	}
}
