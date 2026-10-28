package config

import (
	"testing"
	"time"
)

// clearEnv 把本包读到的环境变量全部置空：getEnv*/getEnvBool 把空串视同未设置，
// 因此等价于"干净环境"，避免 CI 上的 LOG_LEVEL/HOSTNAME/JWT_SECRET 等污染默认值断言。
func clearEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"TASK_SVC_GRPC_PORT", "TASK_SVC_HTTP_PORT", "TASK_SVC_STORE_TYPE", "TASK_SVC_DSN",
		"TASK_SVC_SHUTDOWN_TIMEOUT", "TASK_SVC_MAX_TASKS", "TASK_SVC_MAX_RETRIES",
		"TASK_SVC_TASK_TIMEOUT", "OTEL_EXPORTER_OTLP_ENDPOINT", "LOG_LEVEL",
		"TASK_SVC_SHADOW_MODE", "TASK_SVC_HTTP_GATEWAY_ENABLED", "JWT_SECRET",
		"TASK_SVC_LEADER_MODE", "TASK_SVC_LEADER_LEASE", "TASK_SVC_LEADER_HOLDER",
		"TASK_SVC_LEADER_TTL", "HOSTNAME",
	}
	for _, k := range keys {
		t.Setenv(k, "")
	}
}

func TestLoad_Defaults(t *testing.T) {
	clearEnv(t)
	c := Load()

	want := &Config{
		GRPCPort:           50052,
		HTTPPort:           8081,
		StoreType:          "memory",
		DSN:                "",
		ShutdownTimeout:    10 * time.Second,
		MaxTasks:           10000,
		MaxRetries:         3,
		TaskTimeout:        300,
		OTelEndpoint:       "",
		LogLevel:           "info",
		ShadowMode:         false,
		HTTPGatewayEnabled: true,
		JWTSecret:          "",
		LeaderMode:         "stub",
		LeaderLeaseName:    "task-svc-leader",
		LeaderHolderID:     "task-svc-single",
		LeaderTTL:          15 * time.Second,
	}
	if *c != *want {
		t.Errorf("默认配置不符：\n got=%+v\nwant=%+v", *c, *want)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv("TASK_SVC_GRPC_PORT", "6001")
	t.Setenv("TASK_SVC_HTTP_PORT", "6002")
	t.Setenv("TASK_SVC_STORE_TYPE", "sql")
	t.Setenv("TASK_SVC_DSN", "user:pass@tcp(db:3306)/x")
	t.Setenv("TASK_SVC_SHUTDOWN_TIMEOUT", "45s")
	t.Setenv("TASK_SVC_MAX_TASKS", "7")
	t.Setenv("TASK_SVC_MAX_RETRIES", "9")
	t.Setenv("TASK_SVC_TASK_TIMEOUT", "120")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "otel:4317")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("TASK_SVC_SHADOW_MODE", "true")
	t.Setenv("TASK_SVC_HTTP_GATEWAY_ENABLED", "false")
	t.Setenv("JWT_SECRET", "s3cret")
	t.Setenv("TASK_SVC_LEADER_MODE", "mysql")
	t.Setenv("TASK_SVC_LEADER_LEASE", "lease-x")
	t.Setenv("TASK_SVC_LEADER_HOLDER", "pod-0")
	t.Setenv("TASK_SVC_LEADER_TTL", "20s")

	c := Load()
	if c.GRPCPort != 6001 || c.HTTPPort != 6002 {
		t.Errorf("端口未覆盖：%+v", c)
	}
	if c.StoreType != "sql" || c.DSN != "user:pass@tcp(db:3306)/x" {
		t.Errorf("存储配置未覆盖：%+v", c)
	}
	if c.ShutdownTimeout != 45*time.Second || c.LeaderTTL != 20*time.Second {
		t.Errorf("时长未覆盖：%+v", c)
	}
	if c.MaxTasks != 7 || c.MaxRetries != 9 || c.TaskTimeout != 120 {
		t.Errorf("数值未覆盖：%+v", c)
	}
	if c.OTelEndpoint != "otel:4317" || c.LogLevel != "debug" || c.JWTSecret != "s3cret" {
		t.Errorf("字符串未覆盖：%+v", c)
	}
	if !c.ShadowMode || c.HTTPGatewayEnabled {
		t.Errorf("布尔未覆盖：shadow=%v gateway=%v", c.ShadowMode, c.HTTPGatewayEnabled)
	}
	if c.LeaderMode != "mysql" || c.LeaderLeaseName != "lease-x" || c.LeaderHolderID != "pod-0" {
		t.Errorf("选主配置未覆盖：%+v", c)
	}
}

// TestLoad_InvalidValuesFallBack 钉住"解析失败即回落默认值"的静默语义：
// 这是运维改错 env 时服务仍能启动的既有契约，改成报错属行为变更。
func TestLoad_InvalidValuesFallBack(t *testing.T) {
	clearEnv(t)
	t.Setenv("TASK_SVC_GRPC_PORT", "not-a-number")
	t.Setenv("TASK_SVC_MAX_RETRIES", "3.5")
	t.Setenv("TASK_SVC_SHUTDOWN_TIMEOUT", "10") // 缺单位
	t.Setenv("TASK_SVC_LEADER_TTL", "soon")

	c := Load()
	if c.GRPCPort != 50052 {
		t.Errorf("非法端口应回落 50052，得到 %d", c.GRPCPort)
	}
	if c.MaxRetries != 3 {
		t.Errorf("非法重试数应回落 3，得到 %d", c.MaxRetries)
	}
	if c.ShutdownTimeout != 10*time.Second {
		t.Errorf("缺单位的时长应回落 10s，得到 %v", c.ShutdownTimeout)
	}
	if c.LeaderTTL != 15*time.Second {
		t.Errorf("非法租约时长应回落 15s，得到 %v", c.LeaderTTL)
	}
}

// TestLoad_BoolParsing 钉住 task-svc 的布尔口径：
// ShadowMode 仅字面 "true" 生效；HTTPGatewayEnabled 除字面 "false" 外一律启用。
func TestLoad_BoolParsing(t *testing.T) {
	cases := []struct {
		shadow, gateway string
		wantShadow      bool
		wantGateway     bool
	}{
		{"", "", false, true},
		{"true", "", true, true},
		{"TRUE", "", false, true}, // 非字面 true 不生效
		{"1", "", false, true},
		{"", "false", false, false},
		{"", "FALSE", false, true}, // 非字面 false 不关闭
		{"", "0", false, true},
	}
	for _, tc := range cases {
		clearEnv(t)
		t.Setenv("TASK_SVC_SHADOW_MODE", tc.shadow)
		t.Setenv("TASK_SVC_HTTP_GATEWAY_ENABLED", tc.gateway)
		c := Load()
		if c.ShadowMode != tc.wantShadow || c.HTTPGatewayEnabled != tc.wantGateway {
			t.Errorf("shadow=%q gateway=%q 时得到 (%v, %v)，期望 (%v, %v)",
				tc.shadow, tc.gateway, c.ShadowMode, c.HTTPGatewayEnabled, tc.wantShadow, tc.wantGateway)
		}
	}
}

func TestLoad_LeaderHolderFallback(t *testing.T) {
	clearEnv(t)
	t.Setenv("HOSTNAME", "node-1")
	if c := Load(); c.LeaderHolderID != "node-1" {
		t.Errorf("未显式指定 holder 时应回落到 HOSTNAME，得到 %q", c.LeaderHolderID)
	}

	clearEnv(t)
	t.Setenv("HOSTNAME", "node-1")
	t.Setenv("TASK_SVC_LEADER_HOLDER", "explicit")
	if c := Load(); c.LeaderHolderID != "explicit" {
		t.Errorf("显式 holder 应优先于 HOSTNAME，得到 %q", c.LeaderHolderID)
	}
}
