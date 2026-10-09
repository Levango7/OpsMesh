// session_extra_test.go 会话存储（InProcessSessionStore / RedisSessionStore）边界与
// 自定义错误类型用例（TD-61 末批：自 store_extra_test.go 中段拆分；三组测试中的内存/多 schema
// 两组已分别回流 memory 包与 multischema 包，本文件是留在父包的会话组）。
package store

import (
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/store/model"
)

// ============================================================================
// InProcessSessionStore 边界
// ============================================================================

// TestInProcessSessionStore_IsBlacklisted_Empty 验证空 jti 返回 false。
func TestInProcessSessionStore_IsBlacklisted_Empty(t *testing.T) {
	s := NewInProcessSessionStore()
	if s.IsBlacklisted("") {
		t.Fatal("空 jti 应返回 false")
	}
}

// TestInProcessSessionStore_Blacklist_Empty 验证空 jti 不写入。
func TestInProcessSessionStore_Blacklist_Empty(t *testing.T) {
	s := NewInProcessSessionStore()
	s.Blacklist("", time.Minute)
	if s.IsBlacklisted("") {
		t.Fatal("空 jti 不应被加入黑名单")
	}
}

// TestInProcessSessionStore_IsBlacklisted_Expired 验证过期条目返回 false 并清理。
func TestInProcessSessionStore_IsBlacklisted_Expired(t *testing.T) {
	s := NewInProcessSessionStore()
	s.Blacklist("jti1", -time.Minute) // 已过期
	if s.IsBlacklisted("jti1") {
		t.Fatal("过期 jti 应返回 false")
	}
}

// TestInProcessSessionStore_PurgeBlacklist_Extra 验证 PurgeBlacklist 清理过期条目。
func TestInProcessSessionStore_PurgeBlacklist_Extra(t *testing.T) {
	s := NewInProcessSessionStore()
	s.Blacklist("jti1", time.Minute)  // 未过期
	s.Blacklist("jti2", -time.Minute) // 已过期
	s.PurgeBlacklist()
	if !s.IsBlacklisted("jti1") {
		t.Fatal("jti1 未过期应保留")
	}
	if s.IsBlacklisted("jti2") {
		t.Fatal("jti2 已过期应被清理")
	}
}

// TestInProcessSessionStore_IncrRateLimit_Empty 验证空 key 返回 0。
func TestInProcessSessionStore_IncrRateLimit_Empty(t *testing.T) {
	s := NewInProcessSessionStore()
	if n := s.IncrRateLimit("", time.Minute); n != 0 {
		t.Fatalf("IncrRateLimit 空key = %d, want 0", n)
	}
}

// TestInProcessSessionStore_IncrRateLimit_Window 验证窗口内累计、窗口外重置。
func TestInProcessSessionStore_IncrRateLimit_Window(t *testing.T) {
	s := NewInProcessSessionStore()
	if n := s.IncrRateLimit("k1", time.Minute); n != 1 {
		t.Fatalf("首次 = %d, want 1", n)
	}
	if n := s.IncrRateLimit("k1", time.Minute); n != 2 {
		t.Fatalf("二次 = %d, want 2", n)
	}
	// 窗口过期后重置：手动改 window 为很久以前
	s.mu.Lock()
	s.rateLimits["k1"].window = time.Now().Add(-2 * time.Minute)
	s.mu.Unlock()
	if n := s.IncrRateLimit("k1", time.Minute); n != 1 {
		t.Fatalf("窗口过期后应重置 = %d, want 1", n)
	}
}

// TestInProcessSessionStore_ResetRateLimit_Extra 验证 ResetRateLimit。
func TestInProcessSessionStore_ResetRateLimit_Extra(t *testing.T) {
	s := NewInProcessSessionStore()
	s.IncrRateLimit("k1", time.Minute)
	s.ResetRateLimit("k1")
	if n := s.IncrRateLimit("k1", time.Minute); n != 1 {
		t.Fatalf("Reset 后应从 1 开始 = %d, want 1", n)
	}
	s.ResetRateLimit("") // 空key 不 panic
}

// TestInProcessSessionStore_CreateChangePasswordToken_Empty 验证空 token 返回错误。
func TestInProcessSessionStore_CreateChangePasswordToken_Empty(t *testing.T) {
	s := NewInProcessSessionStore()
	if err := s.CreateChangePasswordToken("", "u1", time.Minute); err == nil {
		t.Fatal("空 token 应返回错误")
	}
}

// TestInProcessSessionStore_ConsumeChangePasswordToken 验证一次性消费与过期。
func TestInProcessSessionStore_ConsumeChangePasswordToken(t *testing.T) {
	s := NewInProcessSessionStore()
	if err := s.CreateChangePasswordToken("tok1", "u1", time.Minute); err != nil {
		t.Fatalf("CreateChangePasswordToken 失败: %v", err)
	}
	// 正常消费
	uid, ok := s.ConsumeChangePasswordToken("tok1")
	if !ok || uid != "u1" {
		t.Fatalf("Consume = (%q,%v), want (u1,true)", uid, ok)
	}
	// 二次消费失败
	if _, ok := s.ConsumeChangePasswordToken("tok1"); ok {
		t.Fatal("二次消费应失败")
	}
	// 不存在
	if _, ok := s.ConsumeChangePasswordToken("no-exist"); ok {
		t.Fatal("不存在应返回 false")
	}
	// 空 token
	if _, ok := s.ConsumeChangePasswordToken(""); ok {
		t.Fatal("空 token 应返回 false")
	}
	// 过期
	if err := s.CreateChangePasswordToken("tok2", "u2", -time.Minute); err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if _, ok := s.ConsumeChangePasswordToken("tok2"); ok {
		t.Fatal("过期 token 应返回 false")
	}
}

// TestInProcessSessionStore_PurgeChangePasswordTokens_Extra 验证清理过期 token。
func TestInProcessSessionStore_PurgeChangePasswordTokens_Extra(t *testing.T) {
	s := NewInProcessSessionStore()
	s.CreateChangePasswordToken("tok1", "u1", time.Minute)  // 未过期
	s.CreateChangePasswordToken("tok2", "u2", -time.Minute) // 已过期
	s.PurgeChangePasswordTokens()
	// tok1 仍可消费
	if _, ok := s.ConsumeChangePasswordToken("tok1"); !ok {
		t.Fatal("tok1 未过期应保留")
	}
}

// TestInProcessSessionStore_Close 验证 Close 返回 nil。
func TestInProcessSessionStore_Close(t *testing.T) {
	s := NewInProcessSessionStore()
	if err := s.Close(); err != nil {
		t.Fatalf("Close 应返回 nil, got %v", err)
	}
}

// ============================================================================
// RedisSessionStore 构造与 key 拼接（不依赖真实 Redis）
// ============================================================================

// TestRedisSessionStore_New_EmptyAddr 验证空 addr 返回错误。
func TestRedisSessionStore_New_EmptyAddr(t *testing.T) {
	if _, err := NewRedisSessionStore("", "", "opsmesh:", time.Second); err == nil {
		t.Fatal("空 addr 应返回错误")
	}
}

// TestRedisSessionStore_New_InvalidAddr 验证无效 addr 不 fail-fast（仅日志）。
func TestRedisSessionStore_New_InvalidAddr(t *testing.T) {
	// 无效地址：连接失败但不应 fail-fast
	s, err := NewRedisSessionStore("127.0.0.1:1", "", "opsmesh:", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRedisSessionStore 不应 fail-fast: %v", err)
	}
	if s == nil {
		t.Fatal("应返回非 nil store")
	}
	defer s.Close()
}

// TestRedisSessionStore_New_DefaultPrefix 验证空 prefix 使用默认值。
func TestRedisSessionStore_New_DefaultPrefix(t *testing.T) {
	s, err := NewRedisSessionStore("127.0.0.1:1", "", "", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRedisSessionStore 失败: %v", err)
	}
	defer s.Close()
	// 验证 prefix 通过 key 拼接可见
	if got := s.key("x"); got != "opsmesh:x" {
		t.Fatalf("default prefix key = %q, want opsmesh:x", got)
	}
}

// TestRedisSessionStore_New_DefaultDialTimeout 验证 dialTimeout<=0 使用默认值。
func TestRedisSessionStore_New_DefaultDialTimeout(t *testing.T) {
	s, err := NewRedisSessionStore("127.0.0.1:1", "", "opsmesh:", 0)
	if err != nil {
		t.Fatalf("NewRedisSessionStore 失败: %v", err)
	}
	defer s.Close()
}

// TestRedisSessionStore_New_PasswordWiredToClient 验证口令透传到 redis.Options
// （此前 Redis 无认证支持，密码无从传递）。
func TestRedisSessionStore_New_PasswordWiredToClient(t *testing.T) {
	s, err := NewRedisSessionStore("127.0.0.1:1", "s3cret", "opsmesh:", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRedisSessionStore 失败: %v", err)
	}
	defer s.Close()
	if got := s.client.Options().Password; got != "s3cret" {
		t.Fatalf("redis client Password = %q, want s3cret", got)
	}

	// 空口令（默认）不发送 AUTH。
	s2, err := NewRedisSessionStore("127.0.0.1:1", "", "opsmesh:", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRedisSessionStore 失败: %v", err)
	}
	defer s2.Close()
	if got := s2.client.Options().Password; got != "" {
		t.Fatalf("空口令时 redis client Password = %q, want 空", got)
	}
}

// TestRedisSessionStore_KeyBuilders 验证各 key 拼接方法。
func TestRedisSessionStore_KeyBuilders(t *testing.T) {
	s, err := NewRedisSessionStore("127.0.0.1:1", "", "opsmesh:", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRedisSessionStore 失败: %v", err)
	}
	defer s.Close()
	if got := s.key("x"); got != "opsmesh:x" {
		t.Fatalf("key = %q, want opsmesh:x", got)
	}
	if got := s.blacklistKey("jti1"); got != "opsmesh:blacklist:jti1" {
		t.Fatalf("blacklistKey = %q, want opsmesh:blacklist:jti1", got)
	}
	if got := s.rateLimitKey("k1"); got != "opsmesh:ratelimit:k1" {
		t.Fatalf("rateLimitKey = %q, want opsmesh:ratelimit:k1", got)
	}
	if got := s.cpTokenKey("tok1"); got != "opsmesh:cptoken:tok1" {
		t.Fatalf("cpTokenKey = %q, want opsmesh:cptoken:tok1", got)
	}
}

// TestRedisSessionStore_IsBlacklisted_Empty 验证空 jti 返回 false（不查 Redis）。
func TestRedisSessionStore_IsBlacklisted_Empty(t *testing.T) {
	s, err := NewRedisSessionStore("127.0.0.1:1", "", "opsmesh:", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRedisSessionStore 失败: %v", err)
	}
	defer s.Close()
	if s.IsBlacklisted("") {
		t.Fatal("空 jti 应返回 false")
	}
}

// TestRedisSessionStore_Blacklist_Empty 验证空 jti 不写入。
func TestRedisSessionStore_Blacklist_Empty(t *testing.T) {
	s, err := NewRedisSessionStore("127.0.0.1:1", "", "opsmesh:", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRedisSessionStore 失败: %v", err)
	}
	defer s.Close()
	s.Blacklist("", time.Minute) // 不应 panic
}

// TestRedisSessionStore_PurgeBlacklist_NoOp 验证 PurgeBlacklist 是 no-op。
func TestRedisSessionStore_PurgeBlacklist_NoOp(t *testing.T) {
	s, err := NewRedisSessionStore("127.0.0.1:1", "", "opsmesh:", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRedisSessionStore 失败: %v", err)
	}
	defer s.Close()
	s.PurgeBlacklist() // 不应 panic
}

// TestRedisSessionStore_IncrRateLimit_Empty 验证空 key 返回 0。
func TestRedisSessionStore_IncrRateLimit_Empty(t *testing.T) {
	s, err := NewRedisSessionStore("127.0.0.1:1", "", "opsmesh:", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRedisSessionStore 失败: %v", err)
	}
	defer s.Close()
	if n := s.IncrRateLimit("", time.Minute); n != 0 {
		t.Fatalf("IncrRateLimit 空key = %d, want 0", n)
	}
}

// TestRedisSessionStore_ResetRateLimit_Empty 验证空 key 不 panic。
func TestRedisSessionStore_ResetRateLimit_Empty(t *testing.T) {
	s, err := NewRedisSessionStore("127.0.0.1:1", "", "opsmesh:", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRedisSessionStore 失败: %v", err)
	}
	defer s.Close()
	s.ResetRateLimit("") // 不应 panic
}

// TestRedisSessionStore_CreateChangePasswordToken_Empty 验证空 token 返回错误。
func TestRedisSessionStore_CreateChangePasswordToken_Empty(t *testing.T) {
	s, err := NewRedisSessionStore("127.0.0.1:1", "", "opsmesh:", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRedisSessionStore 失败: %v", err)
	}
	defer s.Close()
	if err := s.CreateChangePasswordToken("", "u1", time.Minute); err == nil {
		t.Fatal("空 token 应返回错误")
	}
}

// TestRedisSessionStore_ConsumeChangePasswordToken_Empty 验证空 token 返回 false。
func TestRedisSessionStore_ConsumeChangePasswordToken_Empty(t *testing.T) {
	s, err := NewRedisSessionStore("127.0.0.1:1", "", "opsmesh:", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRedisSessionStore 失败: %v", err)
	}
	defer s.Close()
	if _, ok := s.ConsumeChangePasswordToken(""); ok {
		t.Fatal("空 token 应返回 false")
	}
}

// TestRedisSessionStore_PurgeChangePasswordTokens_NoOp 验证 PurgeChangePasswordTokens 是 no-op。
func TestRedisSessionStore_PurgeChangePasswordTokens_NoOp(t *testing.T) {
	s, err := NewRedisSessionStore("127.0.0.1:1", "", "opsmesh:", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRedisSessionStore 失败: %v", err)
	}
	defer s.Close()
	s.PurgeChangePasswordTokens() // 不应 panic
}

// TestRedisSessionStore_Close_NilClient 验证 Close nil client 不 panic。
func TestRedisSessionStore_Close_NilClient(t *testing.T) {
	s := &RedisSessionStore{client: nil}
	if err := s.Close(); err != nil {
		t.Fatalf("Close nil client 应返回 nil, got %v", err)
	}
}

// TestErrRedisAddrRequired 验证 errRedisAddrRequired 错误信息。
func TestErrRedisAddrRequired(t *testing.T) {
	if errRedisAddrRequired.Error() == "" {
		t.Fatal("errRedisAddrRequired 应有非空错误信息")
	}
}

// TestErrChangePasswordTokenRequired 验证 errChangePasswordTokenRequired 错误信息。
func TestErrChangePasswordTokenRequired(t *testing.T) {
	if errChangePasswordTokenRequired.Error() == "" {
		t.Fatal("errChangePasswordTokenRequired 应有非空错误信息")
	}
}

// TestErrRefreshTokenHashRequired 验证 model.ErrRefreshTokenHashRequired 错误信息。
func TestErrRefreshTokenHashRequired(t *testing.T) {
	if model.ErrRefreshTokenHashRequired.Error() == "" {
		t.Fatal("model.ErrRefreshTokenHashRequired 应有非空错误信息")
	}
}
