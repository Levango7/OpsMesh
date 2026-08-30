package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the portal-svc.
type Config struct {
	HTTPPort        int           `json:"httpPort"`
	GRPCPort        int           `json:"grpcPort"`
	StoreType       string        `json:"storeType"` // "memory" or "sql"
	DSN             string        `json:"dsn"`       // MySQL DSN (if StoreType=sql)
	ShutdownTimeout time.Duration `json:"shutdownTimeout"`
}

// Load returns a Config populated from environment variables with defaults.
func Load() *Config {
	return &Config{
		HTTPPort:        getEnvInt("PORTAL_SVC_HTTP_PORT", 8080),
		GRPCPort:        getEnvInt("PORTAL_SVC_GRPC_PORT", 50051),
		StoreType:       getEnv("PORTAL_SVC_STORE_TYPE", "memory"),
		DSN:             getEnv("PORTAL_SVC_DSN", ""),
		ShutdownTimeout: getEnvDuration("PORTAL_SVC_SHUTDOWN_TIMEOUT", 10*time.Second),
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
