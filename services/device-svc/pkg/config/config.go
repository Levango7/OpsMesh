package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// MinJWTSecretLen HS256 签名密钥的最小字节数，与 controlplane 保持一致。
const MinJWTSecretLen = 32

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

	// D3+ 自动纳管循环配置（TD-60 阶段 2 补齐）。
	// AutoProvisionInterval 后台循环间隔（默认 5min；<=0 时不启动循环）。
	AutoProvisionInterval time.Duration `json:"autoProvisionInterval"`
	// AutoProvisionMaxBackoff 退避上限（默认 30min）。
	AutoProvisionMaxBackoff time.Duration `json:"autoProvisionMaxBackoff"`
	// SegmentCIDR 后台循环扫描的目标网段（空=循环不执行扫描）。
	SegmentCIDR string `json:"segmentCIDR"`

	// D3+ bootstrap agent 二进制分发配置（TD-60 阶段 2 补齐）。
	// AgentBinDir agent 二进制分发目录（按平台/架构组织：opsmesh-agent-{os}-{arch}）。
	// 空=回退当前进程二进制（仅开发/单机部署）。
	AgentBinDir string `json:"agentBinDir"`

	// AllowInsecureDevSecret 仅供本地开发/CI 放行空签名密钥的显式逃生舱。
	// P0 安全修复：JWTSecret 此前默认值是公开字面量，改为空 + 启动期强制校验。
	AllowInsecureDevSecret bool `json:"allowInsecureDevSecret"`
}

// Load returns a Config populated from environment variables with defaults.
func Load() *Config {
	return &Config{
		GRPCPort:                getEnvInt("DEVICE_SVC_GRPC_PORT", 50052),
		HTTPPort:                getEnvInt("DEVICE_SVC_HTTP_PORT", 8081),
		JWTSecret:               getEnv("DEVICE_SVC_JWT_SECRET", ""),
		ProvisionSecret:         getEnv("DEVICE_SVC_PROVISION_SECRET", ""), // 空=启动时随机生成（每次重启 token 失效，生产建议固定配置）
		StoreType:               getEnv("DEVICE_SVC_STORE_TYPE", "memory"),
		DSN:                     getEnv("DEVICE_SVC_DSN", ""),
		RedisAddr:               getEnv("DEVICE_SVC_REDIS_ADDR", ""),
		ShutdownTimeout:         getEnvDuration("DEVICE_SVC_SHUTDOWN_TIMEOUT", 10*time.Second),
		OTelEndpoint:            getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		LogLevel:                getEnv("LOG_LEVEL", "info"),
		CIDRWhitelist:           getEnv("DEVICE_SVC_CIDR_WHITELIST", ""),
		DiscoverTimeout:         getEnvDuration("DEVICE_SVC_DISCOVER_TIMEOUT", 60*time.Second),
		AutoProvision:           valBool("DEVICE_SVC_AUTO_PROVISION", false, "auto-provision"),
		ProvisionSSHKey:         getEnv("DEVICE_SVC_PROVISION_SSH_KEY", ""),
		ProvisionSSHUser:        getEnv("DEVICE_SVC_PROVISION_SSH_USER", "root"),
		ProvisionSSHKP:          getEnv("DEVICE_SVC_PROVISION_SSH_KEY_PASS", ""),
		ProvisionSSHKnownHosts:  getEnv("DEVICE_SVC_PROVISION_SSH_KNOWN_HOSTS", ""),
		AdvertiseAddr:           getEnv("DEVICE_SVC_ADVERTISE_ADDR", ""),
		AutoProvisionInterval:   getEnvDuration("DEVICE_SVC_AUTO_PROVISION_INTERVAL", 5*time.Minute),
		AutoProvisionMaxBackoff: getEnvDuration("DEVICE_SVC_AUTO_PROVISION_MAX_BACKOFF", 30*time.Minute),
		SegmentCIDR:             getEnv("DEVICE_SVC_SEGMENT_CIDR", ""),
		AgentBinDir:             getEnv("DEVICE_SVC_AGENT_BIN_DIR", ""),

		AllowInsecureDevSecret: getEnv("DEVICE_SVC_ALLOW_INSECURE_DEV_SECRET", "false") == "true",
	}
}

// Validate 校验不可静默降级的配置项。
//
// P0 安全修复：JWTSecret 此前有公开默认值 "default-jwt-secret-change-in-production"。
// device-svc 用同一个密钥挂 tenant.Middleware 做 HTTP 网关鉴权，拿到该字面量的人
// 可自行签发任意 tenant_id 的 token，配合 X-Tenant-ID 头即可跨租户读取数据。
// 改为默认值空 + 启动期强制校验。
func (c *Config) Validate() error {
	if c.JWTSecret == "" {
		if c.AllowInsecureDevSecret {
			return nil
		}
		return fmt.Errorf(
			"DEVICE_SVC_JWT_SECRET 未设置：device-svc 用它签发/校验租户 token，"+
				"缺失将导致任意租户身份可被伪造。生产环境必须显式注入（≥%d 字节）。"+
				"本地开发可设 DEVICE_SVC_ALLOW_INSECURE_DEV_SECRET=true 显式放行",
			MinJWTSecretLen)
	}
	if len(c.JWTSecret) < MinJWTSecretLen && !c.AllowInsecureDevSecret {
		return fmt.Errorf(
			"DEVICE_SVC_JWT_SECRET 过短（%d 字节 < %d 字节）：弱密钥可被离线爆破后伪造租户身份。"+
				"本地开发可设 DEVICE_SVC_ALLOW_INSECURE_DEV_SECRET=true 显式放行",
			len(c.JWTSecret), MinJWTSecretLen)
	}
	return nil
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
