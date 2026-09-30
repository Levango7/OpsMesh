package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the runbook-svc.
type Config struct {
	HTTPPort        int           `json:"httpPort"`
	ShutdownTimeout time.Duration `json:"shutdownTimeout"`
	// StoreType 持久化后端："memory"（默认，重启即丢）或 "sql"（MySQL，转正前置）。
	// 声明 sql 但 DSN 缺失/连接失败时 main fail-fast——静默退回内存是
	// "部署成功、重启丢数据"的生产陷阱（TD-68 同哲学）。
	StoreType string `json:"storeType"`
	DSN       string `json:"dsn"`
}

// Load returns a Config populated from environment variables with defaults.
func Load() *Config {
	return &Config{
		HTTPPort:        getEnvInt("RUNBOOK_SVC_HTTP_PORT", 8082),
		ShutdownTimeout: getEnvDuration("RUNBOOK_SVC_SHUTDOWN_TIMEOUT", 10*time.Second),
		StoreType:       getEnv("RUNBOOK_SVC_STORE_TYPE", "memory"),
		DSN:             getEnv("RUNBOOK_SVC_DSN", ""),
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
