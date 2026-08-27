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
	StoreType       string        `json:"storeType"`
	ShutdownTimeout time.Duration `json:"shutdownTimeout"`
	MaxTasks        int           `json:"maxTasks"`
	MaxRetries      int           `json:"maxRetries"`
	TaskTimeout     int           `json:"taskTimeout"`
}

// Load returns a Config populated from environment variables with defaults.
func Load() *Config {
	return &Config{
		GRPCPort:        getEnvInt("TASK_SVC_GRPC_PORT", 50052),
		HTTPPort:        getEnvInt("TASK_SVC_HTTP_PORT", 8081),
		StoreType:       getEnv("TASK_SVC_STORE_TYPE", "memory"),
		ShutdownTimeout: getEnvDuration("TASK_SVC_SHUTDOWN_TIMEOUT", 10*time.Second),
		MaxTasks:        getEnvInt("TASK_SVC_MAX_TASKS", 10000),
		MaxRetries:      getEnvInt("TASK_SVC_MAX_RETRIES", 3),
		TaskTimeout:     getEnvInt("TASK_SVC_TASK_TIMEOUT", 300),
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
