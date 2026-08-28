package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the autoscaler-svc.
type Config struct {
	HTTPPort        int           `json:"httpPort"`
	ShutdownTimeout time.Duration `json:"shutdownTimeout"`

	// Prometheus settings.
	PrometheusURL string `json:"prometheusUrl"`

	// K8s settings.
	KubeConfig  string `json:"kubeConfig"`
	KubeNamespace string `json:"kubeNamespace"`

	// Cooldown settings.
	CooldownUp   time.Duration `json:"cooldownUp"`
	CooldownDown time.Duration `json:"cooldownDown"`
}

// Load returns a Config populated from environment variables with defaults.
func Load() *Config {
	return &Config{
		HTTPPort:       getEnvInt("AUTOSCALER_SVC_HTTP_PORT", 8080),
		ShutdownTimeout: getEnvDuration("AUTOSCALER_SVC_SHUTDOWN_TIMEOUT", 10*time.Second),
		PrometheusURL:  getEnv("PROMETHEUS_URL", "http://localhost:9090"),
		KubeConfig:     getEnv("KUBECONFIG", ""),
		KubeNamespace:  getEnv("KUBE_NAMESPACE", "default"),
		CooldownUp:     getEnvDuration("COOLDOWN_UP", 60*time.Second),
		CooldownDown:   getEnvDuration("COOLDOWN_DOWN", 300*time.Second),
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
