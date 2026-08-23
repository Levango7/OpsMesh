// cleanup_refresh_tokens_test.go 是 CleanupRefreshTokens（过期刷新令牌清理）的回归测试。
//
// 背景：reclaimLoop 周期调用 store.CleanupRefreshTokens() 清理 expires_at 已过期的
// refresh token（防登录防爆破表/DB 容量无界增长）。本文件覆盖三层实现的语义一致性：
//  1. MemoryStore：过期删 / 未过期留 / 零值 ExpiresAt 视为已过期 / 幂等（二次清零）；
//  2. SQLStore：真实 MySQL DELETE ... WHERE expires_at < now，返回 RowsAffected；
//  3. MultiSchemaStore：代理方法路由到全局 schema 的 store。
package store

import (
	"testing"
	"time"
)

// TestMemoryStore_CleanupRefreshTokens 验证 MemoryStore 清理语义。
func TestMemoryStore_CleanupRefreshTokens(t *testing.T) {
	m := NewMemoryStore()
	now := time.Now()

	// 过期 ×2、未过期 ×1、ExpiresAt 零值 ×1（零值早于任何 now → 视为已过期）。
	mustSave := func(hash, uid string, exp time.Time) {
		if err := m.SaveRefreshToken(&RefreshToken{TokenHash: hash, UserID: uid, ExpiresAt: exp}); err != nil {
			t.Fatalf("SaveRefreshToken(%s): %v", hash, err)
		}
	}
	mustSave("exp-1", "u1", now.Add(-time.Minute))
	mustSave("exp-2", "u2", now.Add(-time.Hour))
	mustSave("live-1", "u3", now.Add(time.Hour))
	mustSave("zero-1", "u4", time.Time{})

	if n := m.CleanupRefreshTokens(); n != 3 {
		t.Fatalf("CleanupRefreshTokens = %d, want 3（2 过期 + 1 零值）", n)
	}
	// 未过期的保留。
	if got := m.GetRefreshToken("live-1"); got == nil || got.UserID != "u3" {
		t.Fatalf("未过期 token 应保留; got=%+v", got)
	}
	// 已删除的不可见。
	if m.GetRefreshToken("exp-1") != nil || m.GetRefreshToken("zero-1") != nil {
		t.Fatal("过期 token 应已删除")
	}
	// 幂等：二次清理为 0。
	if n := m.CleanupRefreshTokens(); n != 0 {
		t.Fatalf("二次 CleanupRefreshTokens = %d, want 0", n)
	}
}

// TestMultiSchemaStore_CleanupRefreshTokens 验证代理方法路由到全局 store。
func TestMultiSchemaStore_CleanupRefreshTokens(t *testing.T) {
	m, _ := newTestMultiSchema()
	now := time.Now()

	// 经 MultiSchemaStore 写入（落全局 schema），含一条过期一条存活。
	if err := m.SaveRefreshToken(&RefreshToken{TokenHash: "ms-exp", UserID: "u1", ExpiresAt: now.Add(-time.Second)}); err != nil {
		t.Fatalf("SaveRefreshToken(ms-exp): %v", err)
	}
	if err := m.SaveRefreshToken(&RefreshToken{TokenHash: "ms-live", UserID: "u2", ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatalf("SaveRefreshToken(ms-live): %v", err)
	}

	if n := m.CleanupRefreshTokens(); n != 1 {
		t.Fatalf("CleanupRefreshTokens = %d, want 1", n)
	}
	if m.GetRefreshToken("ms-live") == nil {
		t.Fatal("未过期 token 应保留")
	}
	if m.GetRefreshToken("ms-exp") != nil {
		t.Fatal("过期 token 应已删除")
	}
}

// TestSQLStore_CleanupRefreshTokens 用真实 MySQL 验证 SQL 实现。
//
// 运行：
//
//	OPSMESH_TEST_MYSQL_DSN="user:pass@tcp(127.0.0.1:3306)/opsmesh?parseTime=true" \
//	go test ./internal/store/ -run TestSQLStore_CleanupRefreshTokens -v
func TestSQLStore_CleanupRefreshTokens(t *testing.T) {
	s, cleanup := newTestSQLStore(t)
	defer cleanup()

	now := time.Now().UTC()
	rows := []*RefreshToken{
		{TokenHash: "sql-exp-1", UserID: "u1", ExpiresAt: now.Add(-time.Minute)},
		{TokenHash: "sql-exp-2", UserID: "u2", ExpiresAt: now.Add(-time.Hour)},
		{TokenHash: "sql-live", UserID: "u3", ExpiresAt: now.Add(time.Hour)},
	}
	for _, rt := range rows {
		if err := s.SaveRefreshToken(rt); err != nil {
			t.Fatalf("SaveRefreshToken(%s): %v", rt.TokenHash, err)
		}
	}

	if n := s.CleanupRefreshTokens(); n != 2 {
		t.Fatalf("CleanupRefreshTokens = %d, want 2", n)
	}
	if got := s.GetRefreshToken("sql-live"); got == nil {
		t.Fatal("未过期 token 应保留")
	}
	if s.GetRefreshToken("sql-exp-1") != nil || s.GetRefreshToken("sql-exp-2") != nil {
		t.Fatal("过期 token 应已删除")
	}
}
