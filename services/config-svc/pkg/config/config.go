package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// MinEncryptionKeyLen 对称加密密钥的最小字节数（AES-256 = 32 字节）。
const MinEncryptionKeyLen = 32

// Config holds all configuration for the config-svc.
type Config struct {
	GRPCPort        int           `json:"grpcPort"`
	HTTPPort        int           `json:"httpPort"`
	StoreType       string        `json:"storeType"` // "memory" or "sql"
	DSN             string        `json:"dsn"`       // SQLStore DSN (if StoreType=sql)
	RedisAddr       string        `json:"redisAddr"` // Redis address (if StoreType=sql)
	ShutdownTimeout time.Duration `json:"shutdownTimeout"`
	EncryptionKey   string        `json:"encryptionKey"`  // Key for secrets encryption at rest
	MaxHistorySize  int           `json:"maxHistorySize"` // Max versions to retain per config/secret

	// AllowInsecureDevSecret 仅供本地开发/CI 放行空加密密钥的显式逃生舱。
	// P0 安全修复：EncryptionKey 此前默认值是公开字面量，改为空 + 启动期强制校验。
	AllowInsecureDevSecret bool `json:"allowInsecureDevSecret"`

	// OTel tracing settings.
	OTelEndpoint string `json:"otelEndpoint"` // OTLP gRPC collector address (empty = disabled)
	LogLevel     string `json:"logLevel"`     // debug, info, warn, error (default: info)
}

// Load returns a Config populated from environment variables with defaults.
func Load() *Config {
	return &Config{
		GRPCPort:        getEnvInt("CONFIG_SVC_GRPC_PORT", 50054),
		HTTPPort:        getEnvInt("CONFIG_SVC_HTTP_PORT", 8083),
		StoreType:       getEnv("CONFIG_SVC_STORE_TYPE", "memory"),
		DSN:             getEnv("CONFIG_SVC_DSN", ""),
		RedisAddr:       getEnv("CONFIG_SVC_REDIS_ADDR", ""),
		ShutdownTimeout: getEnvDuration("CONFIG_SVC_SHUTDOWN_TIMEOUT", 10*time.Second),
		EncryptionKey:   getEnv("CONFIG_SVC_ENCRYPTION_KEY", ""),
		MaxHistorySize:  getEnvInt("CONFIG_SVC_MAX_HISTORY_SIZE", 50),
		OTelEndpoint:    getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		LogLevel:        getEnv("LOG_LEVEL", "info"),

		AllowInsecureDevSecret: getEnv("CONFIG_SVC_ALLOW_INSECURE_DEV_SECRET", "false") == "true",
	}
}

// Validate 校验不可静默降级的配置项。
//
// P0 安全修复：EncryptionKey 此前有公开默认值
// "default-encryption-key-change-in-production"。config-svc 用它加密下发的配置
// 密文（secret 轮换、配置版本），拿到该字面量即可解密任意租户的存量密文。
// 改为默认值空 + 启动期强制校验。
func (c *Config) Validate() error {
	if c.EncryptionKey == "" {
		if c.AllowInsecureDevSecret {
			return nil
		}
		return fmt.Errorf(
			"CONFIG_SVC_ENCRYPTION_KEY 未设置：config-svc 用它加密下发的配置密文，"+
				"缺失等价于密文可被任意人解密。生产环境必须显式注入（≥%d 字节）。"+
				"本地开发可设 CONFIG_SVC_ALLOW_INSECURE_DEV_SECRET=true 显式放行",
			MinEncryptionKeyLen)
	}
	if len(c.EncryptionKey) < MinEncryptionKeyLen && !c.AllowInsecureDevSecret {
		return fmt.Errorf(
			"CONFIG_SVC_ENCRYPTION_KEY 过短（%d 字节 < %d 字节）：弱密钥可被离线爆破后解密存量密文。"+
				"本地开发可设 CONFIG_SVC_ALLOW_INSECURE_DEV_SECRET=true 显式放行",
			len(c.EncryptionKey), MinEncryptionKeyLen)
	}
	return nil
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
