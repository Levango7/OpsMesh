package config

import (
	"errors"
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the alert-svc.
type Config struct {
	GRPCPort        int           `json:"grpcPort"`
	HTTPPort        int           `json:"httpPort"`
	StoreType       string        `json:"storeType"` // "memory" or "sql"
	DSN             string        `json:"dsn"`       // SQLStore DSN (if StoreType=sql)
	RedisAddr       string        `json:"redisAddr"` // Redis address (if StoreType=sql)
	ShutdownTimeout time.Duration `json:"shutdownTimeout"`

	// OTel tracing settings.
	OTelEndpoint string `json:"otelEndpoint"` // OTLP gRPC collector address (empty = disabled)
	LogLevel     string `json:"logLevel"`     // debug, info, warn, error (default: info)

	// PagerDuty notification settings.
	PagerDutyEnabled    bool   `json:"pagerDutyEnabled"`
	PagerDutyRoutingKey string `json:"pagerDutyRoutingKey"`
	PagerDutyAPIURL     string `json:"pagerDutyApiUrl"`

	// GRPCAuthToken 是 gRPC 面的共享密钥（ALERT_SVC_GRPC_TOKEN，默认空 = 不设鉴权）。
	// 为什么默认空：出厂把该端口绑在 127.0.0.1（compose 第 714-724 行 + 门禁第 18 节），
	// 且全仓没有任何组件 dial 它 ⇒ 空默认值不改变现有部署的行为。
	// 但一旦 PAGERDUTY_ENABLED=true，这条无鉴权 gRPC 面就成了"改告警状态并外发值班"的唯一入口
	// （ack/resolve 在服务侧没有 REST 路由），所以那种组合下必须显式给密钥 —— 见 Validate()。
	GRPCAuthToken string `json:"-"` // 不进 JSON：日志/诊断里不能带出密钥
}

// ErrNoAuthTokenWithPagerDuty 是 Validate() 唯一可能返回的错误，命名出来是为了让测试与
// 文档能引用同一个判据，而不是靠匹配日志字符串（日志文案改了测试就假失败）。
var ErrNoAuthTokenWithPagerDuty = errors.New(
	"PAGERDUTY_ENABLED=true 必须同时设置 ALERT_SVC_GRPC_TOKEN：ack/resolve 只有这条无鉴权 gRPC 入口，" +
		"否则任何能连到该端口的人都能改告警状态并触发值班外发")

// Validate 校验"能力开关"与"该能力的暴露面"是否配套。放在 config 包里是为了可测——
// main 里只调用它，判据本身不在 package main 里（那处没法被单测覆盖）。
func (c *Config) Validate() error {
	if c.PagerDutyEnabled && c.GRPCAuthToken == "" {
		return ErrNoAuthTokenWithPagerDuty
	}
	return nil
}

// Load returns a Config populated from environment variables with defaults.
func Load() *Config {
	return &Config{
		GRPCPort:            getEnvInt("ALERT_SVC_GRPC_PORT", 50051),
		HTTPPort:            getEnvInt("ALERT_SVC_HTTP_PORT", 8080),
		StoreType:           getEnv("ALERT_SVC_STORE_TYPE", "memory"),
		DSN:                 getEnv("ALERT_SVC_DSN", ""),
		RedisAddr:           getEnv("ALERT_SVC_REDIS_ADDR", ""),
		ShutdownTimeout:     getEnvDuration("ALERT_SVC_SHUTDOWN_TIMEOUT", 10*time.Second),
		OTelEndpoint:        getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		LogLevel:            getEnv("LOG_LEVEL", "info"),
		PagerDutyEnabled:    getEnvBool("PAGERDUTY_ENABLED", false),
		PagerDutyRoutingKey: getEnv("PAGERDUTY_ROUTING_KEY", ""),
		PagerDutyAPIURL:     getEnv("PAGERDUTY_API_URL", "https://events.pagerduty.com/v2/enqueue"),
		GRPCAuthToken:       getEnv("ALERT_SVC_GRPC_TOKEN", ""),
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

func getEnvBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}
