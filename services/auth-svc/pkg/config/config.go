package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the auth-svc.
type Config struct {
	GRPCPort        int           `json:"grpcPort"`
	HTTPPort        int           `json:"httpPort"`
	JWTSecret       string        `json:"jwtSecret"`
	AccessTokenTTL  time.Duration `json:"accessTokenTTL"`
	RefreshTokenTTL time.Duration `json:"refreshTokenTTL"`
	RedisAddr       string        `json:"redisAddr"`
	ShutdownTimeout time.Duration `json:"shutdownTimeout"`

	// OTel tracing settings.
	OTelEndpoint string `json:"otelEndpoint"` // OTLP gRPC collector address (empty = disabled)
	LogLevel     string `json:"logLevel"`     // debug, info, warn, error (default: info)
}

// Load returns a Config populated from environment variables with defaults.
func Load() *Config {
	return &Config{
		GRPCPort:        getEnvInt("AUTH_SVC_GRPC_PORT", 50052),
		HTTPPort:        getEnvInt("AUTH_SVC_HTTP_PORT", 8081),
		JWTSecret:       getEnv("AUTH_SVC_JWT_SECRET", "default-jwt-secret-change-in-production"),
		AccessTokenTTL:  getEnvDuration("AUTH_SVC_ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTokenTTL: getEnvDuration("AUTH_SVC_REFRESH_TOKEN_TTL", 7*24*time.Hour),
		RedisAddr:       getEnv("AUTH_SVC_REDIS_ADDR", ""),
		ShutdownTimeout: getEnvDuration("AUTH_SVC_SHUTDOWN_TIMEOUT", 10*time.Second),
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
