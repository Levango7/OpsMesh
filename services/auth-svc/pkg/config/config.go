package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// MinJWTSecretLen HS256 签名密钥的最小字节数，与 controlplane
// internal/config 的生产校验保持一致（32 字节 = 256 bit）。
const MinJWTSecretLen = 32

// Config holds all configuration for the auth-svc.
type Config struct {
	GRPCPort        int           `json:"grpcPort"`
	HTTPPort        int           `json:"httpPort"`
	JWTSecret       string        `json:"jwtSecret"`
	AccessTokenTTL  time.Duration `json:"accessTokenTTL"`
	RefreshTokenTTL time.Duration `json:"refreshTokenTTL"`
	RedisAddr       string        `json:"redisAddr"`
	StoreType       string        `json:"storeType"` // "memory" or "sql"
	DSN             string        `json:"dsn"`       // MySQL DSN (if StoreType=sql)
	ShutdownTimeout time.Duration `json:"shutdownTimeout"`

	// OTel tracing settings.
	OTelEndpoint string `json:"otelEndpoint"` // OTLP gRPC collector address (empty = disabled)
	LogLevel     string `json:"logLevel"`     // debug, info, warn, error (default: info)

	// HTTPEnabled A1 网关开关（R1 风险控制）：默认 false——auth-svc 不对外提供 HTTP，
	// 杜绝与 controlplane 并存期双轨 Cookie（opsmesh_at/opsmesh_rt）互写冲突。
	// 切流阶段（方案 B 验证后）显式设 true 启用。
	HTTPEnabled bool `json:"httpEnabled"`
	// CookieSecure 与 controlplane cookieSecure 同语义：显式 true 或 TLS 部署时
	// Cookie 加 Secure 属性（HTTP 反代终止 TLS 场景须显式配置）。
	CookieSecure bool `json:"cookieSecure"`
	// PublicRegister 公开注册闸门（对齐 controlplane --public-register，默认 true）：
	// true=开放 POST /api/v1/auth/register，但新用户一律 pending 须管理员审批；
	// false=关闭公开注册（仅管理员可经 POST /api/v1/users 创建，返回 403）。
	// 注意与「免审批」是两件事：本服务不提供免审批模式（R6 设计声明）。
	PublicRegister bool `json:"publicRegister"`

	// TD-60 安全能力配置（默认启用，测试环境可关闭）。

	// DeviceFPEnabled 设备指纹校验开关（默认 true）。
	// false 时所有设备放行（不触发未知设备二次验证），测试环境用。
	DeviceFPEnabled bool `json:"deviceFPEnabled"`
	// SessionStoreEnabled Redis Session 存储开关（默认 true）。
	// false 或 Redis 不可用时降级为 JWT 无状态模式。
	SessionStoreEnabled bool `json:"sessionStoreEnabled"`
	// SessionTTL Session 过期时间（默认 24h，滑动过期）。
	SessionTTL time.Duration `json:"sessionTTL"`
	// AdminPassword 初始 admin 口令（与 controlplane --admin-password 同语义）：
	// 非空时替换 seed 弱口令 admin123，须满足强口令策略；留空则生成随机口令并交付
	// （优先 AdminPasswordFile，其次打印一次到日志）。
	AdminPassword string `json:"adminPassword"`
	// AdminPasswordFile 未显式提供 AdminPassword 时，随机口令的落盘路径（权限 0600）。
	// 容器环境下日志会被集中采集留存，生产建议用 Secret 注入 AdminPassword 或改用本文件交付。
	AdminPasswordFile string `json:"adminPasswordFile"`

	// PasswordMinLen 强口令最小长度（默认 12；设 8 可降级到旧策略用于测试）。
	PasswordMinLen int `json:"passwordMinLen"`
	// PasswordRequireSpecial 强口令是否要求特殊字符（默认 true；false 降级到旧策略）。
	PasswordRequireSpecial bool `json:"passwordRequireSpecial"`

	// AllowInsecureDevSecret 仅供本地开发/CI 放行空签名密钥的显式逃生舱。
	// P0 安全修复：此前 JWTSecret 的默认值是公开字面量
	// "default-jwt-secret-change-in-production"，任何读过本仓库的人都能据此伪造
	// 携带任意 roles/permissions 的 HS256 token（requirePermission 直接信任
	// token 内的 permissions，不回查数据库）。默认值已改为空，改为启动期强制校验。
	AllowInsecureDevSecret bool `json:"allowInsecureDevSecret"`
}

// Load returns a Config populated from environment variables with defaults.
func Load() *Config {
	return &Config{
		GRPCPort:        getEnvInt("AUTH_SVC_GRPC_PORT", 50052),
		HTTPPort:        getEnvInt("AUTH_SVC_HTTP_PORT", 8081),
		JWTSecret:       getEnv("AUTH_SVC_JWT_SECRET", ""),
		AccessTokenTTL:  getEnvDuration("AUTH_SVC_ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTokenTTL: getEnvDuration("AUTH_SVC_REFRESH_TOKEN_TTL", 7*24*time.Hour),
		RedisAddr:       getEnv("AUTH_SVC_REDIS_ADDR", ""),
		StoreType:       getEnv("AUTH_SVC_STORE_TYPE", "memory"),
		DSN:             getEnv("AUTH_SVC_DSN", ""),
		ShutdownTimeout: getEnvDuration("AUTH_SVC_SHUTDOWN_TIMEOUT", 10*time.Second),
		OTelEndpoint:    getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		LogLevel:        getEnv("LOG_LEVEL", "info"),
		HTTPEnabled:     getEnv("AUTH_SVC_HTTP_ENABLED", "false") == "true",
		CookieSecure:    getEnv("AUTH_SVC_HTTP_COOKIE_SECURE", "false") == "true",
		PublicRegister:  getEnv("AUTH_SVC_PUBLIC_REGISTER", "true") != "false",

		AdminPassword:     getEnv("AUTH_SVC_ADMIN_PASSWORD", ""),
		AdminPasswordFile: getEnv("AUTH_SVC_ADMIN_PASSWORD_FILE", ""),

		// TD-60 安全能力：默认启用，测试环境可通过环境变量关闭。
		DeviceFPEnabled:        getEnv("AUTH_SVC_DEVICE_FP_ENABLED", "true") != "false",
		SessionStoreEnabled:    getEnv("AUTH_SVC_SESSION_STORE_ENABLED", "true") != "false",
		SessionTTL:             getEnvDuration("AUTH_SVC_SESSION_TTL", 24*time.Hour),
		PasswordMinLen:         getEnvInt("AUTH_SVC_PASSWORD_MIN_LEN", 12),
		PasswordRequireSpecial: getEnv("AUTH_SVC_PASSWORD_REQUIRE_SPECIAL", "true") != "false",

		AllowInsecureDevSecret: getEnv("AUTH_SVC_ALLOW_INSECURE_DEV_SECRET", "false") == "true",
	}
}

// Validate 校验配置中不可静默降级的项。
//
// P0 安全修复：JWTSecret 不再有内置默认值，必须由部署方显式注入。
// 空密钥或过短密钥一律拒绝启动——这是唯一能杜绝"公开字面量可伪造任意权限 token"的位置。
func (c *Config) Validate() error {
	if c.JWTSecret == "" {
		if c.AllowInsecureDevSecret {
			return nil
		}
		return fmt.Errorf(
			"AUTH_SVC_JWT_SECRET 未设置：auth-svc 用它签发并校验 HS256 access token，"+
				"缺失将导致任意身份可被伪造。生产环境必须显式注入（≥%d 字节）。"+
				"本地开发可设 AUTH_SVC_ALLOW_INSECURE_DEV_SECRET=true 显式放行",
			MinJWTSecretLen)
	}
	if len(c.JWTSecret) < MinJWTSecretLen && !c.AllowInsecureDevSecret {
		return fmt.Errorf(
			"AUTH_SVC_JWT_SECRET 过短（%d 字节 < %d 字节）：弱签名密钥可被离线爆破后伪造任意身份。"+
				"本地开发可设 AUTH_SVC_ALLOW_INSECURE_DEV_SECRET=true 显式放行",
			len(c.JWTSecret), MinJWTSecretLen)
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
