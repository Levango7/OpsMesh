package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the task-svc.
type Config struct {
	GRPCPort        int           `json:"grpcPort"`
	HTTPPort        int           `json:"httpPort"`
	StoreType       string        `json:"storeType"` // "memory" or "sql"
	DSN             string        `json:"dsn"`       // MySQL DSN (if StoreType=sql)
	ShutdownTimeout time.Duration `json:"shutdownTimeout"`
	MaxTasks        int           `json:"maxTasks"`
	MaxRetries      int           `json:"maxRetries"`
	TaskTimeout     int           `json:"taskTimeout"`

	// OTel tracing settings.
	OTelEndpoint string `json:"otelEndpoint"` // OTLP gRPC collector address (empty = disabled)
	LogLevel     string `json:"logLevel"`     // debug, info, warn, error (default: info)

	// ShadowMode 影子模式开关（A-2 阶段任务迁移双轨对照用）。
	// true=在常规 scheduler 之外额外启动只读影子循环，评估 task 派生/回收期望并与
	// 现状对比；不写任何 store 状态。默认 false（关闭=常规服务模式）。
	ShadowMode bool `json:"shadowMode"`
}

// Load returns a Config populated from environment variables with defaults.
func Load() *Config {
	return &Config{
		GRPCPort:        getEnvInt("TASK_SVC_GRPC_PORT", 50052),
		HTTPPort:        getEnvInt("TASK_SVC_HTTP_PORT", 8081),
		StoreType:       getEnv("TASK_SVC_STORE_TYPE", "memory"),
		DSN:             getEnv("TASK_SVC_DSN", ""),
		ShutdownTimeout: getEnvDuration("TASK_SVC_SHUTDOWN_TIMEOUT", 10*time.Second),
		MaxTasks:        getEnvInt("TASK_SVC_MAX_TASKS", 10000),
		MaxRetries:      getEnvInt("TASK_SVC_MAX_RETRIES", 3),
		TaskTimeout:     getEnvInt("TASK_SVC_TASK_TIMEOUT", 300),
		OTelEndpoint:    getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		LogLevel:        getEnv("LOG_LEVEL", "info"),
		ShadowMode:      getEnv("TASK_SVC_SHADOW_MODE", "false") == "true",
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
