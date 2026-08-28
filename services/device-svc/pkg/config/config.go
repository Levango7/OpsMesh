package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the device-svc.
type Config struct {
	GRPCPort        int           `json:"grpcPort"`
	HTTPPort        int           `json:"httpPort"`
	JWTSecret       string        `json:"jwtSecret"`
	StoreType       string        `json:"storeType"` // "memory" or "sql"
	DSN             string        `json:"dsn"`       // SQLStore DSN (if StoreType=sql)
	RedisAddr       string        `json:"redisAddr"` // Redis address (if StoreType=sql)
	ShutdownTimeout time.Duration `json:"shutdownTimeout"`

	// OTel tracing settings.
	OTelEndpoint string `json:"otelEndpoint"` // OTLP gRPC collector address (empty = disabled)
	LogLevel     string `json:"logLevel"`     // debug, info, warn, error (default: info)
}

// Load returns a Config populated from environment variables with defaults.
func Load() *Config {
	return &Config{
		GRPCPort:        getEnvInt("DEVICE_SVC_GRPC_PORT", 50052),
		HTTPPort:        getEnvInt("DEVICE_SVC_HTTP_PORT", 8081),
		JWTSecret:       getEnv("DEVICE_SVC_JWT_SECRET", "default-jwt-secret-change-in-production"),
		StoreType:       getEnv("DEVICE_SVC_STORE_TYPE", "memory"),
		DSN:             getEnv("DEVICE_SVC_DSN", ""),
		RedisAddr:       getEnv("DEVICE_SVC_REDIS_ADDR", ""),
		ShutdownTimeout: getEnvDuration("DEVICE_SVC_SHUTDOWN_TIMEOUT", 10*time.Second),
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
