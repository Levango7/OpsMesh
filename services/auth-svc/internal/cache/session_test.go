// session_test.go — Redis SessionStore 降级测试（TD-60）。
//
// 测试环境无 Redis，验证降级行为：所有操作 no-op，Validate 返回 (nil, nil) 不阻止。
package cache

import (
	"errors"
	"testing"
	"time"
)

func TestSessionStore_DegradedMode(t *testing.T) {
	// Redis 不可用（测试环境无 Redis）→ 降级为无状态模式。
	ss := NewSessionStore("localhost:1", 24*time.Hour) // 确保不可达的端口
	if ss.Enabled() {
		t.Fatal("Redis 不可达时 Enabled 应为 false")
	}

	// Create：no-op（不报错）。
	err := ss.Create(&Session{SessionID: "s1", UserID: "u1"})
	if err != nil {
		t.Fatalf("降级模式 Create 应 no-op 返回 nil，实际 %v", err)
	}

	// Validate：返回 (nil, nil) 表示"不阻止"（降级到 JWT 无状态校验）。
	sess, err := ss.Validate("s1")
	if err != nil {
		t.Fatalf("降级模式 Validate 应返回 nil error，实际 %v", err)
	}
	if sess != nil {
		t.Error("降级模式 Validate 应返回 nil session（不阻止）")
	}

	// Revoke：审计整改批次2——降级时不再静默 no-op，必须返回
	// ErrSessionStoreUnavailable（access token 仍有效到自然过期）。
	if err := ss.Revoke("s1"); !errors.Is(err, ErrSessionStoreUnavailable) {
		t.Fatalf("降级模式 Revoke 应返回 ErrSessionStoreUnavailable，实际 %v", err)
	}

	// RevokeAllForUser：同上。
	if err := ss.RevokeAllForUser("u1"); !errors.Is(err, ErrSessionStoreUnavailable) {
		t.Fatalf("降级模式 RevokeAllForUser 应返回 ErrSessionStoreUnavailable，实际 %v", err)
	}

	// Refresh：no-op。
	if err := ss.Refresh("s1"); err != nil {
		t.Fatalf("降级模式 Refresh 应 no-op，实际 %v", err)
	}

	_ = ss.Close()
}
