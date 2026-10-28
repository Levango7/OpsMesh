package config

import (
	"testing"
	"time"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"PORTAL_SVC_HTTP_PORT", "PORTAL_SVC_GRPC_PORT", "PORTAL_SVC_STORE_TYPE",
		"PORTAL_SVC_DSN", "PORTAL_SVC_SHUTDOWN_TIMEOUT",
	} {
		t.Setenv(k, "")
	}
}

func TestLoad_Defaults(t *testing.T) {
	clearEnv(t)
	c := Load()
	want := &Config{
		HTTPPort:        8080,
		GRPCPort:        50051,
		StoreType:       "memory",
		DSN:             "",
		ShutdownTimeout: 10 * time.Second,
	}
	if *c != *want {
		t.Errorf("默认配置不符：\n got=%+v\nwant=%+v", *c, *want)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv("PORTAL_SVC_HTTP_PORT", "8100")
	t.Setenv("PORTAL_SVC_GRPC_PORT", "5100")
	t.Setenv("PORTAL_SVC_STORE_TYPE", "sql")
	t.Setenv("PORTAL_SVC_DSN", "dsn-x")
	t.Setenv("PORTAL_SVC_SHUTDOWN_TIMEOUT", "30s")

	c := Load()
	if c.HTTPPort != 8100 || c.GRPCPort != 5100 {
		t.Errorf("端口未覆盖：%+v", c)
	}
	if c.StoreType != "sql" || c.DSN != "dsn-x" {
		t.Errorf("存储配置未覆盖：%+v", c)
	}
	if c.ShutdownTimeout != 30*time.Second {
		t.Errorf("优雅关闭时长未覆盖：%v", c.ShutdownTimeout)
	}
}

// TestLoad_InvalidValuesFallBack 钉住解析失败回落默认值（静默）的既有语义。
func TestLoad_InvalidValuesFallBack(t *testing.T) {
	clearEnv(t)
	t.Setenv("PORTAL_SVC_HTTP_PORT", "abc")
	t.Setenv("PORTAL_SVC_GRPC_PORT", "50.5")
	t.Setenv("PORTAL_SVC_SHUTDOWN_TIMEOUT", "10")

	c := Load()
	if c.HTTPPort != 8080 || c.GRPCPort != 50051 {
		t.Errorf("非法端口应回落默认值：%+v", c)
	}
	if c.ShutdownTimeout != 10*time.Second {
		t.Errorf("缺单位时长应回落 10s，得到 %v", c.ShutdownTimeout)
	}
}
