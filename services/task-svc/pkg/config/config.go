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

	// HTTPGatewayEnabled 控制 HTTP 业务网关是否启用（P0 切流阻塞项）。
	// true=在 /health /ready /metrics 之外注册 /api/v1/tasks /api/v1/schedules 等 REST 端点。
	// 默认 true（启用）；测试/CI 可关闭以隔离 gRPC 行为。
	HTTPGatewayEnabled bool `json:"httpGatewayEnabled"`

	// JWTSecret 是 HTTP 网关验签 access token 的 HS256 对称密钥。
	// 非空时启用 token 校验（解析 userID/tenantID 注入上下文）；
	// 空串=仅租户隔离（从头/context 提取租户，不校验 token）。
	// 与 controlplane/auth-svc 的 JWT_SECRET 同源。
	JWTSecret string `json:"jwtSecret"`

	// LeaderMode 选主模式（多副本安全的核心开关，2026-09-27 补齐）：
	//   - "stub"（默认）：永真，单副本语义。多副本部署下 fire/reclaim 会
	//     重复执行——这是 TD-60 评估登记的缺陷，扩容前必须切 mysql。
	//   - "mysql"：基于 MySQL 租约的真选主（需 StoreType=sql 且 DSN 非空，
	//     否则启动即失败——声明了 HA 却静默跑 stub 属于静默降级，参照 TD-68 哲学）。
	LeaderMode string `json:"leaderMode"`
	// LeaderLeaseName 选主锁名（同库多套环境隔离时区分用）。
	LeaderLeaseName string `json:"leaderLeaseName"`
	// LeaderHolderID 本实例标识（K8s 下为 Pod 名；单机默认 hostname）。
	LeaderHolderID string `json:"leaderHolderID"`
	// LeaderTTL 租约有效期。副本失联后其余副本等待该时长方可接管。
	LeaderTTL time.Duration `json:"leaderTTL"`
}

// Load returns a Config populated from environment variables with defaults.
func Load() *Config {
	return &Config{
		GRPCPort:           getEnvInt("TASK_SVC_GRPC_PORT", 50052),
		HTTPPort:           getEnvInt("TASK_SVC_HTTP_PORT", 8081),
		StoreType:          getEnv("TASK_SVC_STORE_TYPE", "memory"),
		DSN:                getEnv("TASK_SVC_DSN", ""),
		ShutdownTimeout:    getEnvDuration("TASK_SVC_SHUTDOWN_TIMEOUT", 10*time.Second),
		MaxTasks:           getEnvInt("TASK_SVC_MAX_TASKS", 10000),
		MaxRetries:         getEnvInt("TASK_SVC_MAX_RETRIES", 3),
		TaskTimeout:        getEnvInt("TASK_SVC_TASK_TIMEOUT", 300),
		OTelEndpoint:       getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		LogLevel:           getEnv("LOG_LEVEL", "info"),
		ShadowMode:         getEnv("TASK_SVC_SHADOW_MODE", "false") == "true",
		HTTPGatewayEnabled: getEnv("TASK_SVC_HTTP_GATEWAY_ENABLED", "true") != "false",
		JWTSecret:          getEnv("JWT_SECRET", ""),
		LeaderMode:         getEnv("TASK_SVC_LEADER_MODE", "stub"),
		LeaderLeaseName:    getEnv("TASK_SVC_LEADER_LEASE", "task-svc-leader"),
		LeaderHolderID:     getEnv("TASK_SVC_LEADER_HOLDER", getEnv("HOSTNAME", "task-svc-single")),
		LeaderTTL:          getEnvDuration("TASK_SVC_LEADER_TTL", 15*time.Second),
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
