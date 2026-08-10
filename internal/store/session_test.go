// session_test.go 测试 SessionStore 接口的 InProcessSessionStore 实现。
//
// 测试策略：
//   - 直接构造 InProcessSessionStore，验证三类状态（黑名单/限流/改密令牌）的行为；
//   - 不测试 RedisSessionStore（需真实 Redis，由集成测试覆盖）；
//   - 覆盖边界：空入参、过期清理、一次性消费、滑动窗口重置。
package store

import (
	"testing"
	"time"
)

// TestInProcessSessionStore_Blacklist 验证 JWT 黑名单基本行为。
func TestInProcessSessionStore_Blacklist(t *testing.T) {
	s := NewInProcessSessionStore()
	defer s.Close()

	// 空jti 不校验。
	if s.IsBlacklisted("") {
		t.Fatal("空 jti 应返回 false")
	}

	// 加入黑名单后应命中。
	s.Blacklist("jti-1", time.Minute)
	if !s.IsBlacklisted("jti-1") {
		t.Fatal("jti-1 应在黑名单中")
	}

	// 未加入的不命中。
	if s.IsBlacklisted("jti-2") {
		t.Fatal("jti-2 不应在黑名单中")
	}

	// 空 jti 加入黑名单 no-op。
	s.Blacklist("", time.Minute)
	// 不应 panic，且不影响其他条目。
	if !s.IsBlacklisted("jti-1") {
		t.Fatal("jti-1 仍应在黑名单中")
	}
}

// TestInProcessSessionStore_BlacklistExpiry 验证黑名单过期后自动清理。
func TestInProcessSessionStore_BlacklistExpiry(t *testing.T) {
	s := NewInProcessSessionStore()
	defer s.Close()

	// 加入短 TTL 黑名单。
	s.Blacklist("jti-short", 50*time.Millisecond)
	if !s.IsBlacklisted("jti-short") {
		t.Fatal("jti-short 应在黑名单中")
	}

	// 等待过期。
	time.Sleep(60 * time.Millisecond)
	if s.IsBlacklisted("jti-short") {
		t.Fatal("jti-short 过期后应不在黑名单中")
	}
}

// TestInProcessSessionStore_PurgeBlacklist 验证 PurgeBlacklist 清理过期条目。
func TestInProcessSessionStore_PurgeBlacklist(t *testing.T) {
	s := NewInProcessSessionStore()
	defer s.Close()

	s.Blacklist("jti-1", time.Minute)
	s.Blacklist("jti-2", 50*time.Millisecond)

	// 等待 jti-2 过期。
	time.Sleep(60 * time.Millisecond)

	// PurgeBlacklist 清理过期条目。
	s.PurgeBlacklist()

	// jti-1 仍应存在，jti-2 应被清理。
	if !s.IsBlacklisted("jti-1") {
		t.Fatal("jti-1 未过期，应仍在黑名单中")
	}
	if s.IsBlacklisted("jti-2") {
		t.Fatal("jti-2 已过期，应被清理")
	}
}

// TestInProcessSessionStore_RateLimit 验证滑动窗口计数器基本行为。
func TestInProcessSessionStore_RateLimit(t *testing.T) {
	s := NewInProcessSessionStore()
	defer s.Close()

	// 空key 返回 0。
	if n := s.IncrRateLimit("", time.Minute); n != 0 {
		t.Fatalf("空 key 应返回 0，得到 %d", n)
	}

	// 首次计数返回 1。
	if n := s.IncrRateLimit("ip:1.2.3.4", time.Minute); n != 1 {
		t.Fatalf("首次计数应返回 1，得到 %d", n)
	}

	// 第二次计数返回 2。
	if n := s.IncrRateLimit("ip:1.2.3.4", time.Minute); n != 2 {
		t.Fatalf("第二次计数应返回 2，得到 %d", n)
	}

	// 第三次计数返回 3。
	if n := s.IncrRateLimit("ip:1.2.3.4", time.Minute); n != 3 {
		t.Fatalf("第三次计数应返回 3，得到 %d", n)
	}

	// 不同 key 独立计数。
	if n := s.IncrRateLimit("ip:5.6.7.8", time.Minute); n != 1 {
		t.Fatalf("不同 key 首次计数应返回 1，得到 %d", n)
	}
}

// TestInProcessSessionStore_RateLimitWindowReset 验证窗口过期后计数重置。
func TestInProcessSessionStore_RateLimitWindowReset(t *testing.T) {
	s := NewInProcessSessionStore()
	defer s.Close()

	// 短窗口计数。
	s.IncrRateLimit("key", 50*time.Millisecond)
	s.IncrRateLimit("key", 50*time.Millisecond)
	if n := s.IncrRateLimit("key", 50*time.Millisecond); n != 3 {
		t.Fatalf("窗口内第三次计数应返回 3，得到 %d", n)
	}

	// 等待窗口过期。
	time.Sleep(60 * time.Millisecond)

	// 新窗口计数应重置为 1。
	if n := s.IncrRateLimit("key", 50*time.Millisecond); n != 1 {
		t.Fatalf("窗口过期后应重置为 1，得到 %d", n)
	}
}

// TestInProcessSessionStore_ResetRateLimit 验证重置计数。
func TestInProcessSessionStore_ResetRateLimit(t *testing.T) {
	s := NewInProcessSessionStore()
	defer s.Close()

	s.IncrRateLimit("key", time.Minute)
	s.IncrRateLimit("key", time.Minute)

	// 重置后计数应从 1 开始。
	s.ResetRateLimit("key")
	if n := s.IncrRateLimit("key", time.Minute); n != 1 {
		t.Fatalf("重置后首次计数应返回 1，得到 %d", n)
	}

	// 重置不存在的 key no-op。
	s.ResetRateLimit("nonexistent")
}

// TestInProcessSessionStore_ChangePasswordToken 验证改密令牌一次性消费。
func TestInProcessSessionStore_ChangePasswordToken(t *testing.T) {
	s := NewInProcessSessionStore()
	defer s.Close()

	// 创建令牌。
	if err := s.CreateChangePasswordToken("token-1", "user-1", time.Minute); err != nil {
		t.Fatalf("创建改密令牌失败: %v", err)
	}

	// 消费令牌应返回 userID。
	userID, ok := s.ConsumeChangePasswordToken("token-1")
	if !ok || userID != "user-1" {
		t.Fatalf("消费令牌应返回 (user-1, true)，得到 (%s, %v)", userID, ok)
	}

	// 再次消费应失败（一次性）。
	if _, ok := s.ConsumeChangePasswordToken("token-1"); ok {
		t.Fatal("一次性令牌不应被重复消费")
	}

	// 消费不存在的令牌应失败。
	if _, ok := s.ConsumeChangePasswordToken("nonexistent"); ok {
		t.Fatal("不存在的令牌应消费失败")
	}

	// 空令牌应消费失败。
	if _, ok := s.ConsumeChangePasswordToken(""); ok {
		t.Fatal("空令牌应消费失败")
	}
}

// TestInProcessSessionStore_ChangePasswordTokenExpiry 验证改密令牌过期后消费失败。
func TestInProcessSessionStore_ChangePasswordTokenExpiry(t *testing.T) {
	s := NewInProcessSessionStore()
	defer s.Close()

	// 创建短 TTL 令牌。
	if err := s.CreateChangePasswordToken("token-short", "user-1", 50*time.Millisecond); err != nil {
		t.Fatalf("创建改密令牌失败: %v", err)
	}

	// 等待过期。
	time.Sleep(60 * time.Millisecond)

	// 过期后消费应失败。
	if _, ok := s.ConsumeChangePasswordToken("token-short"); ok {
		t.Fatal("过期令牌应消费失败")
	}
}

// TestInProcessSessionStore_ChangePasswordTokenEmpty 验证空令牌创建失败。
func TestInProcessSessionStore_ChangePasswordTokenEmpty(t *testing.T) {
	s := NewInProcessSessionStore()
	defer s.Close()

	if err := s.CreateChangePasswordToken("", "user-1", time.Minute); err == nil {
		t.Fatal("空令牌创建应失败")
	}
}

// TestInProcessSessionStore_PurgeChangePasswordTokens 验证清理过期改密令牌。
func TestInProcessSessionStore_PurgeChangePasswordTokens(t *testing.T) {
	s := NewInProcessSessionStore()
	defer s.Close()

	s.CreateChangePasswordToken("token-1", "user-1", time.Minute)
	s.CreateChangePasswordToken("token-2", "user-2", 50*time.Millisecond)

	// 等待 token-2 过期。
	time.Sleep(60 * time.Millisecond)

	// 清理过期令牌。
	s.PurgeChangePasswordTokens()

	// token-1 仍可消费，token-2 已被清理。
	if _, ok := s.ConsumeChangePasswordToken("token-1"); !ok {
		t.Fatal("token-1 未过期，应可消费")
	}
	if _, ok := s.ConsumeChangePasswordToken("token-2"); ok {
		t.Fatal("token-2 已过期，应被清理")
	}
}
