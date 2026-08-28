package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the config-svc.
type Config struct {
	GRPCPort        int           `json:"grpcPort"`
	HTTPPort        int           `json:"httpPort"`
	StoreType       string        `json:"storeType"` // "memory" or "sql"
	DSN             string        `json:"dsn"`       // SQLStore DSN (if StoreType=sql)
	RedisAddr       string        `json:"redisAddr"` // Redis address (if StoreType=sql)
	ShutdownTimeout time.Duration `json:"shutdownTimeout"`
	EncryptionKey   string        `json:"encryptionKey"` // Key for secrets encryption at rest
	MaxHistorySize  int           `json:"maxHistorySize"` // Max versions to retain per config/secret

	// OTel tracing settings.
	OTelEndpoint string `json:"otelEndpoint"` // OTLP gRPC collector address (empty = disabled)
	LogLevel     string `json:"logLevel"`     // debug, info, warn, error (default: info)
}

// Load returns a Config populated from environment variables with defaults.
func Load() *Config {
	return &Config{
		GRPCPort:        getEnvInt("CONFIG_SVC_GRPC_PORT", 50054),
		HTTPPort:        getEnvInt("CONFIG_SVC_HTTP_PORT", 8083),
		StoreType:       getEnv("CONFIG_SVC_STORE_TYPE", "memory"),
		DSN:             getEnv("CONFIG_SVC_DSN", ""),
		RedisAddr:       getEnv("CONFIG_SVC_REDIS_ADDR", ""),
		ShutdownTimeout: getEnvDuration("CONFIG_SVC_SHUTDOWN_TIMEOUT", 10*time.Second),
		EncryptionKey:   getEnv("CONFIG_SVC_ENCRYPTION_KEY", "default-encryption-key-change-in-production"),
		MaxHistorySize:  getEnvInt("CONFIG_SVC_MAX_HISTORY_SIZE", 50),
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
