// cleanup_refresh_tokens_multischema_test.go 清理代理方法路由到全局 schema 的回归
// （TD-61 末批：自父包 cleanup_refresh_tokens_test.go 尾段拆分）。
package multischema

import (
	"testing"
	"time"
)

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
