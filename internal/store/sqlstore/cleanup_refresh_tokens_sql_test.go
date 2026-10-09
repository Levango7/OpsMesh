// cleanup_refresh_tokens_sql_test.go SQL 侧过期刷新令牌清理（需真实 MySQL）。
//
// 自 internal/store/cleanup_refresh_tokens_test.go 拆出（TD-61 批次 3-sql）。内存/多schema 侧见父包。
package sqlstore

import (
	"testing"
	"time"
)

// TestSQLStore_CleanupRefreshTokens 用真实 MySQL 验证 SQL 实现。
//
// 运行：
//
//	OPSMESH_TEST_MYSQL_DSN="user:pass@tcp(127.0.0.1:3306)/opsmesh?parseTime=true" \
//	go test ./internal/store/sqlstore/ -run TestSQLStore_CleanupRefreshTokens -v
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
