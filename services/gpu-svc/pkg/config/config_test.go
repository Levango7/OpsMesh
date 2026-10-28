package config

import (
	"testing"
	"time"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"GPU_SVC_HTTP_PORT", "GPU_SVC_SHUTDOWN_TIMEOUT", "OLLAMA_URL",
		"GPU_SVC_OLLAMA_URL", "GPU_SVC_METRICS_INTERVAL",
		"OTEL_EXPORTER_OTLP_ENDPOINT", "LOG_LEVEL",
	} {
		t.Setenv(k, "")
	}
}

func TestLoad_Defaults(t *testing.T) {
	clearEnv(t)
	c := Load()
	want := &Config{
		HTTPPort:        8090,
		ShutdownTimeout: 10 * time.Second,
		OllamaURL:       "http://localhost:11434",
		MetricsInterval: 30 * time.Second,
		OTelEndpoint:    "",
		LogLevel:        "info",
	}
	if *c != *want {
		t.Errorf("默认配置不符：\n got=%+v\nwant=%+v", *c, *want)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv("GPU_SVC_HTTP_PORT", "8400")
	t.Setenv("GPU_SVC_SHUTDOWN_TIMEOUT", "20s")
	t.Setenv("GPU_SVC_OLLAMA_URL", "http://ollama:11434")
	t.Setenv("GPU_SVC_METRICS_INTERVAL", "5s")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "otel:4317")
	t.Setenv("LOG_LEVEL", "warn")

	c := Load()
	if c.HTTPPort != 8400 || c.ShutdownTimeout != 20*time.Second || c.MetricsInterval != 5*time.Second {
		t.Errorf("端口/时长未覆盖：%+v", c)
	}
	if c.OllamaURL != "http://ollama:11434" {
		t.Errorf("OllamaURL 未覆盖：%q", c.OllamaURL)
	}
	if c.OTelEndpoint != "otel:4317" || c.LogLevel != "warn" {
		t.Errorf("可观测配置未覆盖：%+v", c)
	}
}

// TestLoad_OllamaURLPrecedence 钉住优先级：OLLAMA_URL > GPU_SVC_OLLAMA_URL > 默认。
func TestLoad_OllamaURLPrecedence(t *testing.T) {
	clearEnv(t)
	t.Setenv("OLLAMA_URL", "http://generic:11434")
	t.Setenv("GPU_SVC_OLLAMA_URL", "http://specific:11434")
	if c := Load(); c.OllamaURL != "http://generic:11434" {
		t.Errorf("通用 OLLAMA_URL 应优先，得到 %q", c.OllamaURL)
	}

	clearEnv(t)
	t.Setenv("GPU_SVC_OLLAMA_URL", "http://specific:11434")
	if c := Load(); c.OllamaURL != "http://specific:11434" {
		t.Errorf("服务专用变量应作为次选，得到 %q", c.OllamaURL)
	}
}

func TestLoad_InvalidValuesFallBack(t *testing.T) {
	clearEnv(t)
	t.Setenv("GPU_SVC_HTTP_PORT", "http")
	t.Setenv("GPU_SVC_METRICS_INTERVAL", "30")

	c := Load()
	if c.HTTPPort != 8090 {
		t.Errorf("非法端口应回落 8090，得到 %d", c.HTTPPort)
	}
	if c.MetricsInterval != 30*time.Second {
		t.Errorf("缺单位时长应回落 30s，得到 %v", c.MetricsInterval)
	}
}
