// Redis 认证与多副本会话后端配置的加载与校验测试。
//
// 背景：此前 Redis 无任何认证支持（Go 侧不发 AUTH），生产 Redis 只能裸奔；
// 且「多副本未配 session-store」的告警条件误写为 store=="memory"（上方已拒绝该组合，
// 故条件永假），使 Helm 生产 overlay 的 replicas=3 长期静默——
// 登出/限流/改密令牌不跨副本共享，爆破防护被放大 3 倍。
package config

import (
	"os"
	"strings"
	"testing"
)

// TestLoad_RedisPasswordDefaultEmpty 验证默认不发送 AUTH。
func TestLoad_RedisPasswordDefaultEmpty(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	if got := loadForTest().RedisPassword; got != "" {
		t.Fatalf("RedisPassword 默认值 = %q, want 空", got)
	}
}

// TestLoad_RedisPasswordFromFlagAndEnv 验证 --redis-password / OPSMESH_REDIS_PASSWORD 与优先级。
func TestLoad_RedisPasswordFromFlagAndEnv(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()

	if got := loadForTest("--redis-password=s3cret").RedisPassword; got != "s3cret" {
		t.Fatalf("--redis-password=s3cret → RedisPassword = %q", got)
	}
	if err := os.Setenv("OPSMESH_REDIS_PASSWORD", "envpass"); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	if got := loadForTest().RedisPassword; got != "envpass" {
		t.Fatalf("OPSMESH_REDIS_PASSWORD=envpass → RedisPassword = %q", got)
	}
	// 显式 flag 优先于 env。
	if got := loadForTest("--redis-password=flagpass").RedisPassword; got != "flagpass" {
		t.Fatalf("flag 应优先于 env: RedisPassword = %q", got)
	}
}

// TestValidate_SessionStoreAcceptsUserinfoPassword 验证内嵌口令形式被接受。
func TestValidate_SessionStoreAcceptsUserinfoPassword(t *testing.T) {
	for _, raw := range []string{
		"redis://:pass@localhost:6379",   // 无用户名，仅口令
		"redis://default:pass@r:6379",    // Redis 6+ ACL 默认用户
		"redis://:p%40ss@localhost:6379", // 百分号编码的 @
	} {
		c := base()
		c.SessionStore = raw
		if err := c.Validate(); err != nil {
			t.Fatalf("session-store=%q 应通过: %v", raw, err)
		}
	}
}

// TestValidate_SessionStoreRejectsMalformed 验证畸形 session-store 被拒绝。
func TestValidate_SessionStoreRejectsMalformed(t *testing.T) {
	for _, raw := range []string{
		"mysql://localhost:3306",    // 非 redis scheme
		"redis://",                  // 无 host
		"redis://:onlypass@",        // 有口令但无 host
		"://:pass@host:6379",        // scheme 缺失（非法 URL）
		"redis://:p%ZZss@host:6379", // 非法百分号编码
	} {
		c := base()
		c.SessionStore = raw
		if err := c.Validate(); err == nil {
			t.Fatalf("session-store=%q 应被拒绝", raw)
		}
	}
}

// TestValidate_ProductionMultiReplicaRequiresSessionStore 验证生产多副本必须配置共享会话后端。
func TestValidate_ProductionMultiReplicaRequiresSessionStore(t *testing.T) {
	// 生产 + 3 副本 + mysql store + 无 session-store：拒绝（此前该场景静默放行）。
	c := base()
	prodReadyDefaults(c)
	c.Store = "mysql"
	c.MySQLDSN = "u:p@tcp(db:3306)/db"
	c.Replicas = 3
	err := c.Validate()
	if err == nil {
		t.Fatal("生产 + 多副本 + 无 session-store 应被拒绝")
	}
	if !strings.Contains(err.Error(), "--session-store") {
		t.Fatalf("错误消息应提示 --session-store, got %q", err.Error())
	}

	// 补上 session-store 后通过。
	c2 := base()
	prodReadyDefaults(c2)
	c2.Store = "mysql"
	c2.MySQLDSN = "u:p@tcp(db:3306)/db"
	c2.Replicas = 3
	c2.SessionStore = "redis://:redispass@redis:6379"
	if err := c2.Validate(); err != nil {
		t.Fatalf("生产 + 多副本 + session-store 应通过: %v", err)
	}

	// 生产单副本：无共享状态需求，放行。
	c3 := base()
	prodReadyDefaults(c3)
	c3.Store = "mysql"
	c3.MySQLDSN = "u:p@tcp(db:3306)/db"
	c3.Replicas = 1
	if err := c3.Validate(); err != nil {
		t.Fatalf("生产单副本应通过: %v", err)
	}
}

// TestValidate_NonProductionMultiReplicaWarnsButPasses 验证非生产多副本仅告警不阻断。
func TestValidate_NonProductionMultiReplicaWarnsButPasses(t *testing.T) {
	c := base()
	c.Store = "mysql"
	c.MySQLDSN = "u:p@tcp(db:3306)/db"
	c.Replicas = 3
	if err := c.Validate(); err != nil {
		t.Fatalf("非生产多副本应放行（仅告警）: %v", err)
	}
}
