package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the alert-svc.
type Config struct {
	GRPCPort        int           `json:"grpcPort"`
	HTTPPort        int           `json:"httpPort"`
	StoreType       string        `json:"storeType"` // "memory" or "sql"
	DSN             string        `json:"dsn"`       // SQLStore DSN (if StoreType=sql)
	RedisAddr       string        `json:"redisAddr"` // Redis address (if StoreType=sql)
	ShutdownTimeout time.Duration `json:"shutdownTimeout"`

	// OTel tracing settings.
	OTelEndpoint string `json:"otelEndpoint"` // OTLP gRPC collector address (empty = disabled)
	LogLevel     string `json:"logLevel"`     // debug, info, warn, error (default: info)

	// PagerDuty notification settings.
	PagerDutyEnabled    bool   `json:"pagerDutyEnabled"`
	PagerDutyRoutingKey string `json:"pagerDutyRoutingKey"`
	PagerDutyAPIURL     string `json:"pagerDutyApiUrl"`
}

// Load returns a Config populated from environment variables with defaults.
func Load() *Config {
	return &Config{
		GRPCPort:            getEnvInt("ALERT_SVC_GRPC_PORT", 50051),
		HTTPPort:            getEnvInt("ALERT_SVC_HTTP_PORT", 8080),
		StoreType:           getEnv("ALERT_SVC_STORE_TYPE", "memory"),
		DSN:                 getEnv("ALERT_SVC_DSN", ""),
		RedisAddr:           getEnv("ALERT_SVC_REDIS_ADDR", ""),
		ShutdownTimeout:     getEnvDuration("ALERT_SVC_SHUTDOWN_TIMEOUT", 10*time.Second),
		OTelEndpoint:        getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		LogLevel:            getEnv("LOG_LEVEL", "info"),
		PagerDutyEnabled:    getEnvBool("PAGERDUTY_ENABLED", false),
		PagerDutyRoutingKey: getEnv("PAGERDUTY_ROUTING_KEY", ""),
		PagerDutyAPIURL:     getEnv("PAGERDUTY_API_URL", "https://events.pagerduty.com/v2/enqueue"),
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

func getEnvBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}
