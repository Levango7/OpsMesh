package config

import (
	"testing"
	"time"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"RUNBOOK_SVC_HTTP_PORT", "RUNBOOK_SVC_SHUTDOWN_TIMEOUT",
		"RUNBOOK_SVC_STORE_TYPE", "RUNBOOK_SVC_DSN",
	} {
		t.Setenv(k, "")
	}
}

func TestLoad_Defaults(t *testing.T) {
	clearEnv(t)
	c := Load()
	want := &Config{
		HTTPPort:        8082,
		ShutdownTimeout: 10 * time.Second,
		StoreType:       "memory",
		DSN:             "",
	}
	if *c != *want {
		t.Errorf("默认配置不符：\n got=%+v\nwant=%+v", *c, *want)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv("RUNBOOK_SVC_HTTP_PORT", "8200")
	t.Setenv("RUNBOOK_SVC_SHUTDOWN_TIMEOUT", "25s")
	t.Setenv("RUNBOOK_SVC_STORE_TYPE", "sql")
	t.Setenv("RUNBOOK_SVC_DSN", "dsn-y")

	c := Load()
	if c.HTTPPort != 8200 || c.ShutdownTimeout != 25*time.Second {
		t.Errorf("端口/时长未覆盖：%+v", c)
	}
	if c.StoreType != "sql" || c.DSN != "dsn-y" {
		t.Errorf("存储配置未覆盖：%+v", c)
	}
}

func TestLoad_InvalidValuesFallBack(t *testing.T) {
	clearEnv(t)
	t.Setenv("RUNBOOK_SVC_HTTP_PORT", "eighty")
	t.Setenv("RUNBOOK_SVC_SHUTDOWN_TIMEOUT", "10")

	c := Load()
	if c.HTTPPort != 8082 {
		t.Errorf("非法端口应回落 8082，得到 %d", c.HTTPPort)
	}
	if c.ShutdownTimeout != 10*time.Second {
		t.Errorf("缺单位时长应回落 10s，得到 %v", c.ShutdownTimeout)
	}
}
