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
	StoreType       string        `json:"storeType"` // "memory" or "sql"
	DSN             string        `json:"dsn"`       // MySQL DSN (if StoreType=sql)
	ShutdownTimeout time.Duration `json:"shutdownTimeout"`

	// OTel tracing settings.
	OTelEndpoint string `json:"otelEndpoint"` // OTLP gRPC collector address (empty = disabled)
	LogLevel     string `json:"logLevel"`     // debug, info, warn, error (default: info)

	// HTTPEnabled A1 网关开关（R1 风险控制）：默认 false——auth-svc 不对外提供 HTTP，
	// 杜绝与 controlplane 并存期双轨 Cookie（opsmesh_at/opsmesh_rt）互写冲突。
	// 切流阶段（方案 B 验证后）显式设 true 启用。
	HTTPEnabled bool `json:"httpEnabled"`
	// CookieSecure 与 controlplane cookieSecure 同语义：显式 true 或 TLS 部署时
	// Cookie 加 Secure 属性（HTTP 反代终止 TLS 场景须显式配置）。
	CookieSecure bool `json:"cookieSecure"`
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
		StoreType:       getEnv("AUTH_SVC_STORE_TYPE", "memory"),
		DSN:             getEnv("AUTH_SVC_DSN", ""),
		ShutdownTimeout: getEnvDuration("AUTH_SVC_SHUTDOWN_TIMEOUT", 10*time.Second),
		OTelEndpoint:    getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		LogLevel:        getEnv("LOG_LEVEL", "info"),
		HTTPEnabled:     getEnv("AUTH_SVC_HTTP_ENABLED", "false") == "true",
		CookieSecure:    getEnv("AUTH_SVC_HTTP_COOKIE_SECURE", "false") == "true",
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
