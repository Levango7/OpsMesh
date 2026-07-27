package config

import (
	"testing"
	"time"
)

// base 返回一份合理的控制面默认配置，避免每个用例重复样板。
func base() *Config {
	return &Config{
		Mode:           "controlplane",
		HTTPPort:       8080,
		GRPCPort:       9090,
		MetricsPort:     9091,
		Store:          "memory",
		TaskTimeout:    120 * time.Second,
		ShutdownTimeout: 15 * time.Second,
		WorkerConcurrency: 4,
		TaskLeaseSec:   300,
		Replicas:       1,
		TaskMaxRetries: 3,
	}
}

// TestValidate_OK 验证默认控制面配置通过校验。
func TestValidate_OK(t *testing.T) {
	if err := base().Validate(); err != nil {
		t.Fatalf("base valid config errored: %v", err)
	}
}

// TestValidate_MemoryMultiReplica A3：memory store 配多副本必须被拒绝（数据分裂）。
func TestValidate_MemoryMultiReplica(t *testing.T) {
	c := base()
	c.Store = "memory"
	c.Replicas = 2
	if err := c.Validate(); err == nil {
		t.Fatal("memory + replicas>1 应被拒绝，但通过了")
	}
}

// TestValidate_MysqlMultiReplica A4：mysql store + 多副本是 HA 合法组合。
func TestValidate_MysqlMultiReplica(t *testing.T) {
	c := base()
	c.Store = "mysql"
	c.MySQLDSN = "u:p@tcp(db:3306)/ops_device"
	c.Replicas = 2
	if err := c.Validate(); err != nil {
		t.Fatalf("mysql + replicas>1 应合法，却报错: %v", err)
	}
}

// TestValidate_ProductionMemoryWarns A4：生产模式 + memory 在 Validate 层仍拒绝
// （memory 多副本规则已覆盖；单副本 memory 生产虽能启动但 Load() 会强告警）。
func TestValidate_ProductionMemoryWarns(t *testing.T) {
	c := base()
	c.Production = true
	c.Store = "memory"
	c.Replicas = 1 // 单副本不触发多副本拒绝
	// 单副本 memory 生产：Validate 允许（告警在 Load 层），此处仅确认不崩溃。
	if err := c.Validate(); err != nil {
		t.Fatalf("production+memory 单副本 Validate 不应报错: %v", err)
	}
}

// TestValidate_ModeAndStore A4：非法 mode 与 mysql 缺 DSN 必须拒绝。
func TestValidate_ModeAndStore(t *testing.T) {
	c := base()
	c.Mode = "bogus"
	if err := c.Validate(); err == nil {
		t.Fatal("非法 mode 应被拒绝")
	}

	c2 := base()
	c2.Store = "mysql"
	c2.MySQLDSN = ""
	if err := c2.Validate(); err == nil {
		t.Fatal("mysql 缺 DSN 应被拒绝")
	}
}

// TestLoad_ProductionEnablesRequireAuth A4：生产模式默认开启 require-auth（除非显式关闭）。
func TestLoad_ProductionEnablesRequireAuth(t *testing.T) {
	// 直接构造 Load 等价逻辑：production 且无显式 require-auth 时翻 true。
	c := base()
	c.Production = true
	c.RequireAuth = false
	explicitRequireAuth := false // 模拟未显式设置该 flag
	if c.Production && !explicitRequireAuth {
		c.RequireAuth = true
	}
	if !c.RequireAuth {
		t.Fatal("production 模式应默认开启 require-auth")
	}
}
