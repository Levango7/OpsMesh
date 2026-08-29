package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the bot-svc.
type Config struct {
	HTTPPort        int           `json:"httpPort"`
	ShutdownTimeout time.Duration `json:"shutdownTimeout"`
	OpsMeshAPIURL   string        `json:"opsMeshApiUrl"`
	WecomToken      string        `json:"wecomToken"`
	FeishuToken     string        `json:"feishuToken"`
	SlackToken      string        `json:"slackToken"`
	DingtalkToken   string        `json:"dingtalkToken"`
	RateLimitPerMin int           `json:"rateLimitPerMin"`
}

// Load returns a Config populated from environment variables with defaults.
func Load() *Config {
	return &Config{
		HTTPPort:        getEnvInt("BOT_SVC_HTTP_PORT", 8080),
		ShutdownTimeout: getEnvDuration("BOT_SVC_SHUTDOWN_TIMEOUT", 10*time.Second),
		OpsMeshAPIURL:   getEnv("BOT_SVC_OPSMESH_API_URL", "http://localhost:9090"),
		WecomToken:      getEnv("BOT_SVC_WECOM_TOKEN", ""),
		FeishuToken:     getEnv("BOT_SVC_FEISHU_TOKEN", ""),
		SlackToken:      getEnv("BOT_SVC_SLACK_TOKEN", ""),
		DingtalkToken:   getEnv("BOT_SVC_DINGTALK_TOKEN", ""),
		RateLimitPerMin: getEnvInt("BOT_SVC_RATE_LIMIT_PER_MIN", 30),
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
