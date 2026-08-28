package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the gpu-svc.
type Config struct {
	HTTPPort        int           `json:"httpPort"`
	ShutdownTimeout time.Duration `json:"shutdownTimeout"`
	OllamaURL       string        `json:"ollamaURL"`
	MetricsInterval time.Duration `json:"metricsInterval"`

	// OTel tracing settings.
	OTelEndpoint string `json:"otelEndpoint"` // OTLP gRPC collector address (empty = disabled)
	LogLevel     string `json:"logLevel"`     // debug, info, warn, error (default: info)
}

// Load returns a Config populated from environment variables with defaults.
func Load() *Config {
	return &Config{
		HTTPPort:        getEnvInt("GPU_SVC_HTTP_PORT", 8090),
		ShutdownTimeout: getEnvDuration("GPU_SVC_SHUTDOWN_TIMEOUT", 10*time.Second),
		OllamaURL:       getEnv("OLLAMA_URL", getEnv("GPU_SVC_OLLAMA_URL", "http://localhost:11434")),
		MetricsInterval: getEnvDuration("GPU_SVC_METRICS_INTERVAL", 30*time.Second),
		OTelEndpoint:    getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		LogLevel:        getEnv("LOG_LEVEL", "info"),
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
