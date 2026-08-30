package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the incident-svc.
type Config struct {
	HTTPPort          int           `json:"httpPort"`
	GRPCPort          int           `json:"grpcPort"`
	StoreType         string        `json:"storeType"` // "memory" or "sql"
	DSN               string        `json:"dsn"`       // MySQL DSN (if StoreType=sql)
	ShutdownTimeout   time.Duration `json:"shutdownTimeout"`
	AggregationWindow time.Duration `json:"aggregationWindow"`
}

// Load returns a Config populated from environment variables with defaults.
func Load() *Config {
	return &Config{
		HTTPPort:          getEnvInt("INCIDENT_SVC_HTTP_PORT", 8082),
		GRPCPort:          getEnvInt("INCIDENT_SVC_GRPC_PORT", 50052),
		StoreType:         getEnv("INCIDENT_SVC_STORE_TYPE", "memory"),
		DSN:               getEnv("INCIDENT_SVC_DSN", ""),
		ShutdownTimeout:   getEnvDuration("INCIDENT_SVC_SHUTDOWN_TIMEOUT", 10*time.Second),
		AggregationWindow: getEnvDuration("INCIDENT_SVC_AGGREGATION_WINDOW", 5*time.Minute),
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
