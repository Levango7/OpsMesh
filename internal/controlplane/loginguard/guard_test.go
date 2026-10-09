// guard_test.go 防爆破/限流器的语义与内部态用例（TD-87 批 2：自父包 auth_extra_test.go 随类型迁入——
// 这些用例直接断言内部结构（ips/mu/rateRec/令牌桶常量），跨包无法访问，按惯例与类型同住）。
package loginguard

import (
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/store"
)

func TestLoginGuard_StartSweepStopSweep_Extra(t *testing.T) {
	g := New(store.NewInProcessSessionStore())
	g.StartSweep(50 * time.Millisecond)
	// 等待一两次 sweep 周期
	time.Sleep(120 * time.Millisecond)
	// stopSweep 应让 goroutine 退出
	g.StopSweep()
	// 幂等：再次调用不应 panic
	g.StopSweep()
}

func TestLoginGuard_Sweep_Extra(t *testing.T) {
	g := New(store.NewInProcessSessionStore())
	// 注入一个令牌已回满且超过 1 小时未活动的 IP 记录
	g.mu.Lock()
	g.ips["old-ip"] = &rateRec{tokens: loginRateBurst, last: time.Now().Add(-2 * time.Hour)}
	g.ips["recent-ip"] = &rateRec{tokens: loginRateBurst, last: time.Now()}
	g.mu.Unlock()
	g.Sweep()
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.ips["old-ip"]; ok {
		t.Fatal("old-ip should be swept")
	}
	if _, ok := g.ips["recent-ip"]; !ok {
		t.Fatal("recent-ip should remain")
	}
}

func TestLoginGuard_Allow_Extra(t *testing.T) {
	g := New(store.NewInProcessSessionStore())
	// 首次应放行（令牌桶满）
	if !g.Allow("1.1.1.1") {
		t.Fatal("first allow should pass")
	}
	// 耗尽令牌后应限流
	for i := 0; i < loginRateBurst; i++ {
		g.Allow("1.1.1.1")
	}
	if g.Allow("1.1.1.1") {
		t.Fatal("should be rate limited after burst exhausted")
	}
	// 不同 IP 应独立限流
	if !g.Allow("2.2.2.2") {
		t.Fatal("different IP should pass")
	}
}

func TestLoginGuard_RecordFailLockedReset_Extra(t *testing.T) {
	g := New(store.NewInProcessSessionStore())
	// 记录失败直到触发锁定
	var locked bool
	for i := 0; i < loginMaxFails; i++ {
		locked = g.RecordFail("attacker")
	}
	if !locked {
		t.Fatal("should be locked after max fails")
	}
	if !g.Locked("attacker") {
		t.Fatal("locked() should return true")
	}
	// 重置后应解锁
	g.ResetFail("attacker")
	// resetFail 只清除失败计数，不清除锁定标记；锁定靠 TTL 自然过期
	// 此处主要覆盖代码路径
}

// =============================================================================
// createChangePasswordToken / consumeChangePasswordToken
// =============================================================================
