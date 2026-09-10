package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the device-svc.
type Config struct {
	GRPCPort        int           `json:"grpcPort"`
	HTTPPort        int           `json:"httpPort"`
	JWTSecret       string        `json:"jwtSecret"`
	ProvisionSecret string        `json:"provisionSecret"` // HMAC secret for install tokens (auto-generate if empty)
	StoreType       string        `json:"storeType"`       // "memory" or "sql"
	DSN             string        `json:"dsn"`             // SQLStore DSN (if StoreType=sql)
	RedisAddr       string        `json:"redisAddr"`       // Redis address (if StoreType=sql)
	ShutdownTimeout time.Duration `json:"shutdownTimeout"`

	// OTel tracing settings.
	OTelEndpoint string `json:"otelEndpoint"` // OTLP gRPC collector address (empty = disabled)
	LogLevel     string `json:"logLevel"`     // debug, info, warn, error (default: info)

	// D2 Discovery 真实化配置。
	// CIDRWhitelist 逗号分隔白名单（如 "10.0.0.0/16,192.168.1.0/24"）；
	// 空=不校验（与 controlplane ProvisionCIDRWhitelist 的向后兼容语义一致），
	// 生产部署文档须标注必配——防扫描云元数据网段（SSRF）与内网探测。
	CIDRWhitelist string `json:"cidrWhitelist"`
	// DiscoverTimeout 单个发现 job 的整体 ctx 超时（默认 60s，
	// MaxHosts=1024 × 800ms 单连超时 / 并发 64 的最坏情况约 13s，60s 余量充足）。
	DiscoverTimeout time.Duration `json:"discoverTimeout"`

	// D3 自动纳管配置（默认关闭，双闸：需显式开启 + 配置 SSH 私钥才推送）。
	// AutoProvision 启用自动纳管编排（默认 false）。
	AutoProvision bool `json:"autoProvision"`
	// ProvisionSSHKey SSH 私钥路径（空=仅签发 token 不推送）。
	ProvisionSSHKey string `json:"provisionSSHKey"`
	// ProvisionSSHUser SSH 登录用户（默认 "root"）。
	ProvisionSSHUser string `json:"provisionSSHUser"`
	// ProvisionSSHKP SSH 私钥密码（可选）。
	ProvisionSSHKP string `json:"provisionSSHKP"`
	// ProvisionSSHKnownHosts KnownHosts 文件路径（生产必配，防 MITM）。
	ProvisionSSHKnownHosts string `json:"provisionSSHKnownHosts"`
	// AdvertiseAddr 控制面对外地址（用于拼接 bootstrap URL）。
	AdvertiseAddr string `json:"advertiseAddr"`
}

// Load returns a Config populated from environment variables with defaults.
func Load() *Config {
	return &Config{
		GRPCPort:               getEnvInt("DEVICE_SVC_GRPC_PORT", 50052),
		HTTPPort:               getEnvInt("DEVICE_SVC_HTTP_PORT", 8081),
		JWTSecret:              getEnv("DEVICE_SVC_JWT_SECRET", "default-jwt-secret-change-in-production"),
		ProvisionSecret:        getEnv("DEVICE_SVC_PROVISION_SECRET", ""), // 空=启动时随机生成（每次重启 token 失效，生产建议固定配置）
		StoreType:              getEnv("DEVICE_SVC_STORE_TYPE", "memory"),
		DSN:                    getEnv("DEVICE_SVC_DSN", ""),
		RedisAddr:              getEnv("DEVICE_SVC_REDIS_ADDR", ""),
		ShutdownTimeout:        getEnvDuration("DEVICE_SVC_SHUTDOWN_TIMEOUT", 10*time.Second),
		OTelEndpoint:           getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		LogLevel:               getEnv("LOG_LEVEL", "info"),
		CIDRWhitelist:          getEnv("DEVICE_SVC_CIDR_WHITELIST", ""),
		DiscoverTimeout:        getEnvDuration("DEVICE_SVC_DISCOVER_TIMEOUT", 60*time.Second),
		AutoProvision:          valBool("DEVICE_SVC_AUTO_PROVISION", false, "auto-provision"),
		ProvisionSSHKey:        getEnv("DEVICE_SVC_PROVISION_SSH_KEY", ""),
		ProvisionSSHUser:       getEnv("DEVICE_SVC_PROVISION_SSH_USER", "root"),
		ProvisionSSHKP:         getEnv("DEVICE_SVC_PROVISION_SSH_KEY_PASS", ""),
		ProvisionSSHKnownHosts: getEnv("DEVICE_SVC_PROVISION_SSH_KNOWN_HOSTS", ""),
		AdvertiseAddr:          getEnv("DEVICE_SVC_ADVERTISE_ADDR", ""),
	}
}

func valBool(env string, def bool, _ string) bool {
	if v := os.Getenv(env); v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
	}
	return def
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
