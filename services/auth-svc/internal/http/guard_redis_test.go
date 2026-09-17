// guard_redis_test.go — loginGuard Redis 降级测试（TD-60）。
//
// 验证 Redis 不可用时 guard 降级为内存计数（与 newLoginGuard 等价）。
package http

import (
	"testing"
	"time"
)

func TestLoginGuard_RedisDegradedToMemory(t *testing.T) {
	// cache=nil → 纯内存模式（与 newLoginGuard 等价）。
	g := newLoginGuardWithCache(nil)
	// 5 次失败 → 锁定（内存计数）。
	for i := 0; i < guardMaxFails; i++ {
		g.recordFail("user-degraded")
	}
	if !g.accountLocked("user-degraded") {
		t.Fatal("降级模式：5 次失败后应锁定（内存计数）")
	}
	// 成功复位。
	g.recordSuccess("user-degraded")
	if g.accountLocked("user-degraded") {
		t.Fatal("降级模式：成功后应复位")
	}
}

func TestLoginGuard_RedisDegradedIPRateLimit(t *testing.T) {
	g := newLoginGuardWithCache(nil)
	ip := "10.0.0.99"
	// burst=5：前 5 次放行。
	for i := 0; i < 5; i++ {
		if !g.allowIP(ip) {
			t.Fatalf("第 %d 次应放行", i+1)
		}
	}
	if g.allowIP(ip) {
		t.Fatal("超 burst 应限流")
	}
}

func TestLoginGuard_RedisDegradedWindowReset(t *testing.T) {
	g := newLoginGuardWithCache(nil)
	// 4 次失败（未达阈值）→ 模拟窗口过期 → 再 1 次应重置计数。
	for i := 0; i < 4; i++ {
		g.recordFail("window-user")
	}
	g.mu.Lock()
	rec := g.fails["window-user"]
	rec.windowAt = time.Now().Add(-guardFailWindow - time.Minute)
	g.mu.Unlock()
	g.recordFail("window-user")
	g.mu.Lock()
	fails := g.fails["window-user"].fails
	g.mu.Unlock()
	if fails != 1 {
		t.Fatalf("过期窗口后计数应重置为 1，实际 %d", fails)
	}
}
