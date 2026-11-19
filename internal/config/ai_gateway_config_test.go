package config

import "testing"

func TestLoad_AIGatewayProvidersFromEnv(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	raw := `[{"name":"local","type":"ollama","models":["qwen"]}]`
	t.Setenv("OPSMESH_AI_GATEWAY_PROVIDERS", raw)
	t.Setenv("OPSMESH_AI_GATEWAY_EMBED_PROVIDER", "local")
	cfg := loadForTest()
	if cfg.AIGatewayProviders != raw || cfg.AIGatewayEmbedProvider != "local" {
		t.Fatalf("AI Gateway env 未加载: providers=%q embed=%q", cfg.AIGatewayProviders, cfg.AIGatewayEmbedProvider)
	}
}

func TestLoad_AIGatewayProvidersDefaultsEmpty(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	cfg := loadForTest()
	if cfg.AIGatewayProviders != "" || cfg.AIGatewayEmbedProvider != "" {
		t.Fatalf("默认必须是不装配、embed 未显式指定: %+v", cfg)
	}
}
