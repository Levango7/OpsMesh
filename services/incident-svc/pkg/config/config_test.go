package config

import (
	"testing"
	"time"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"INCIDENT_SVC_HTTP_PORT", "INCIDENT_SVC_GRPC_PORT", "INCIDENT_SVC_STORE_TYPE",
		"INCIDENT_SVC_DSN", "INCIDENT_SVC_SHUTDOWN_TIMEOUT", "INCIDENT_SVC_AGGREGATION_WINDOW",
	} {
		t.Setenv(k, "")
	}
}

func TestLoad_Defaults(t *testing.T) {
	clearEnv(t)
	c := Load()
	want := &Config{
		HTTPPort:          8082,
		GRPCPort:          50052,
		StoreType:         "memory",
		DSN:               "",
		ShutdownTimeout:   10 * time.Second,
		AggregationWindow: 5 * time.Minute,
	}
	if *c != *want {
		t.Errorf("默认配置不符：\n got=%+v\nwant=%+v", *c, *want)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv("INCIDENT_SVC_HTTP_PORT", "8300")
	t.Setenv("INCIDENT_SVC_GRPC_PORT", "5102")
	t.Setenv("INCIDENT_SVC_STORE_TYPE", "sql")
	t.Setenv("INCIDENT_SVC_DSN", "dsn-z")
	t.Setenv("INCIDENT_SVC_SHUTDOWN_TIMEOUT", "15s")
	t.Setenv("INCIDENT_SVC_AGGREGATION_WINDOW", "90s")

	c := Load()
	if c.HTTPPort != 8300 || c.GRPCPort != 5102 {
		t.Errorf("端口未覆盖：%+v", c)
	}
	if c.StoreType != "sql" || c.DSN != "dsn-z" {
		t.Errorf("存储配置未覆盖：%+v", c)
	}
	if c.ShutdownTimeout != 15*time.Second || c.AggregationWindow != 90*time.Second {
		t.Errorf("时长未覆盖：%+v", c)
	}
}

func TestLoad_InvalidValuesFallBack(t *testing.T) {
	clearEnv(t)
	t.Setenv("INCIDENT_SVC_GRPC_PORT", "x")
	t.Setenv("INCIDENT_SVC_AGGREGATION_WINDOW", "5")

	c := Load()
	if c.GRPCPort != 50052 {
		t.Errorf("非法端口应回落 50052，得到 %d", c.GRPCPort)
	}
	if c.AggregationWindow != 5*time.Minute {
		t.Errorf("缺单位时长应回落 5m，得到 %v", c.AggregationWindow)
	}
}
