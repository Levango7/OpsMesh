package config

import (
	"strings"
	"testing"
)

// persistentGuardConfig 返回「持久化 store + 非生产 + 未开 require-auth」的危险组合。
func persistentGuardConfig() *Config {
	c := base()
	c.Store = "mysql"
	c.MySQLDSN = "u:p@tcp(db:3306)/ops_device"
	return c
}

// TestValidateAuthStoreSafety_PersistentWithoutProductionFailsFast 验证审计陷阱：
// --store=mysql 漏传 --production 且未开 require-auth 时必须拒绝启动，且消息给出补救路径。
func TestValidateAuthStoreSafety_PersistentWithoutProductionFailsFast(t *testing.T) {
	err := persistentGuardConfig().ValidateAuthStoreSafety()
	if err == nil {
		t.Fatal("持久化 store + 非生产 + 未开 require-auth 应 fail-fast，但通过了")
	}
	for _, want := range []string{"persistent store", "--production", "--require-auth", "--allow-insecure-store"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("错误信息缺少 %q：%v", want, err)
		}
	}
}

// TestValidateAuthStoreSafety_AllowInsecureDowngradesToWarn 验证显式豁免把 ERROR 降级为 WARN（返回 nil）。
func TestValidateAuthStoreSafety_AllowInsecureDowngradesToWarn(t *testing.T) {
	c := persistentGuardConfig()
	c.AllowInsecureStore = true
	if err := c.ValidateAuthStoreSafety(); err != nil {
		t.Fatalf("allow-insecure-store 应将 fail-fast 降级为告警，却报错: %v", err)
	}
}

// TestValidateAuthStoreSafety_MemoryStoreOK 验证 memory store 不受守卫影响（demo/本地开发）。
func TestValidateAuthStoreSafety_MemoryStoreOK(t *testing.T) {
	c := base()
	c.Store = "memory"
	if err := c.ValidateAuthStoreSafety(); err != nil {
		t.Fatalf("memory store 不应被拒绝: %v", err)
	}
}

// TestValidateAuthStoreSafety_ProductionOK 验证生产模式下仅告警不阻断（--production 已在 Load 中开启 require-auth）。
func TestValidateAuthStoreSafety_ProductionOK(t *testing.T) {
	c := persistentGuardConfig()
	c.Production = true
	if err := c.ValidateAuthStoreSafety(); err != nil {
		t.Fatalf("生产模式不应被守卫阻断: %v", err)
	}
}

// TestValidateAuthStoreSafety_RequireAuthOK 验证 require-auth=true 时持久化 store 合法。
func TestValidateAuthStoreSafety_RequireAuthOK(t *testing.T) {
	c := persistentGuardConfig()
	c.RequireAuth = true
	if err := c.ValidateAuthStoreSafety(); err != nil {
		t.Fatalf("require-auth=true 时不应被拒绝: %v", err)
	}
}

// TestValidateAuthStoreSafety_NonMySQLPersistentAlsoGuarded 验证守卫覆盖任意非 memory 后端（postgres 等）。
func TestValidateAuthStoreSafety_NonMySQLPersistentAlsoGuarded(t *testing.T) {
	c := base()
	c.Store = "postgres"
	if err := c.ValidateAuthStoreSafety(); err == nil {
		t.Fatal("postgres 等持久化后端同样应受守卫约束")
	}
}

// TestLoad_AllowInsecureStoreFlag 验证 --allow-insecure-store flag 生效。
func TestLoad_AllowInsecureStoreFlag(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	c := loadForTest("--allow-insecure-store", "--store=mysql", "--mysql-dsn=u:p@tcp(db:3306)/ops_device")
	if !c.AllowInsecureStore {
		t.Fatal("--allow-insecure-store 应设置 AllowInsecureStore=true")
	}
}

// TestLoad_AllowInsecureStoreEnv 验证 env OPSMESH_ALLOW_INSECURE_STORE 兜底生效。
func TestLoad_AllowInsecureStoreEnv(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	t.Setenv("OPSMESH_ALLOW_INSECURE_STORE", "true")
	c := loadForTest("--store=mysql", "--mysql-dsn=u:p@tcp(db:3306)/ops_device")
	if !c.AllowInsecureStore {
		t.Fatal("OPSMESH_ALLOW_INSECURE_STORE=true 应设置 AllowInsecureStore=true")
	}
}
