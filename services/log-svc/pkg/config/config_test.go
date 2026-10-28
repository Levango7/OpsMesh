package config

import (
	"testing"
	"time"
)

// log-svc 的配置不走环境变量，只有 DefaultConfig；这里钉住开发默认值，
// 任何改动都会影响本地起服务时的监听地址、内存容量与各后端连接参数。
func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()

	if c.Server.Address != ":9090" {
		t.Errorf("Server.Address = %q，期望 :9090", c.Server.Address)
	}
	if c.Server.MaxConcurrentStreams != 1000 {
		t.Errorf("MaxConcurrentStreams = %d，期望 1000", c.Server.MaxConcurrentStreams)
	}
	if c.Server.MaxRecvMsgSize != 64*1024*1024 {
		t.Errorf("MaxRecvMsgSize = %d，期望 64MB", c.Server.MaxRecvMsgSize)
	}

	if c.LogStore.Backend != "memory" {
		t.Errorf("默认后端 = %q，期望 memory", c.LogStore.Backend)
	}
	if c.LogStore.Memory.Capacity != 5000 || !c.LogStore.Memory.EnableIndex {
		t.Errorf("内存后端默认值不对：%+v", c.LogStore.Memory)
	}

	if c.LogStore.SQL.MaxOpenConns != 25 || c.LogStore.SQL.MaxIdleConns != 5 {
		t.Errorf("SQL 连接池默认值不对：%+v", c.LogStore.SQL)
	}
	if c.LogStore.SQL.ConnMaxLifetime != 5*time.Minute {
		t.Errorf("ConnMaxLifetime = %v，期望 5m", c.LogStore.SQL.ConnMaxLifetime)
	}
	if c.LogStore.SQL.DSN != "" {
		t.Errorf("默认不应带 DSN，得到 %q", c.LogStore.SQL.DSN)
	}

	if c.LogStore.Loki.Timeout != 30*time.Second || c.LogStore.Loki.LabelApp != "opsmesh" {
		t.Errorf("Loki 默认值不对：%+v", c.LogStore.Loki)
	}
	if c.LogStore.ES.Timeout != 30*time.Second || c.LogStore.ES.Index != "opsmesh-logs" {
		t.Errorf("ES 默认值不对：%+v", c.LogStore.ES)
	}

	if c.Health.Address != ":8080" {
		t.Errorf("Health.Address = %q，期望 :8080", c.Health.Address)
	}
}

// TestDefaultConfig_Independent 钉住"每次返回新对象"：DefaultConfig 被多处调用，
// 若共享底层 map/slice 之外的指针语义被改成单例，调用方的修改会互相污染。
func TestDefaultConfig_Independent(t *testing.T) {
	a := DefaultConfig()
	a.LogStore.Memory.Capacity = 1
	a.Server.Address = ":1"

	b := DefaultConfig()
	if b.LogStore.Memory.Capacity != 5000 || b.Server.Address != ":9090" {
		t.Errorf("两次 DefaultConfig 共享了状态：%+v", b)
	}
}
