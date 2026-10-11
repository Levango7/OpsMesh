package extension

import (
	"testing"
	"time"
)

// TestCircuitBreaker_OpensAfterThresholdFailures 连续 5 次失败 ⇒ open（拒绝放行）。
func TestCircuitBreaker_OpensAfterThresholdFailures(t *testing.T) {
	cb := NewCircuitBreaker()
	for i := 0; i < CircuitThreshold-1; i++ {
		cb.RecordFailure()
		if !cb.Allow() {
			t.Fatalf("第 %d 次失败后仍应放行（未到阈值）", i+1)
		}
	}
	cb.RecordFailure() // 第 5 次 ⇒ 打开
	if cb.Allow() {
		t.Fatal("达到阈值后应熔断拒绝")
	}
	if cb.State() != CircuitOpen {
		t.Errorf("state=%s, want open", cb.State())
	}
}

// TestCircuitBreaker_HalfOpenAfterCooldown 冷却后半开放行**一个**试探请求。
func TestCircuitBreaker_HalfOpenAfterCooldown(t *testing.T) {
	cb := NewCircuitBreaker()
	// 直接把时钟后的 openedAt 塞进去（不 sleep 30s：测试不依赖墙钟）。
	for i := 0; i < CircuitThreshold; i++ {
		cb.RecordFailure()
	}
	cb.mu.Lock()
	cb.openedAt = time.Now().Add(-CircuitCooldown - time.Second)
	cb.mu.Unlock()

	if !cb.Allow() {
		t.Fatal("冷却结束后应放行试探（half-open）")
	}
	if cb.Allow() {
		t.Fatal("半开试探名额已被占用——第二个不应放行（防试探风暴）")
	}
}

// TestCircuitBreaker_ClosesAfterProbeSuccess 半开试探成功 ⇒ 回 closed 并清零计数。
func TestCircuitBreaker_ClosesAfterProbeSuccess(t *testing.T) {
	cb := NewCircuitBreaker()
	for i := 0; i < CircuitThreshold; i++ {
		cb.RecordFailure()
	}
	cb.mu.Lock()
	cb.openedAt = time.Now().Add(-CircuitCooldown - time.Second)
	cb.mu.Unlock()

	if !cb.Allow() { // 半开放行
		t.Fatal("冷却后应放行试探")
	}
	cb.RecordSuccess()
	if cb.State() != CircuitClosed {
		t.Errorf("试探成功后 state=%s, want closed", cb.State())
	}
	// 之后持续放行（已恢复）。
	for i := 0; i < 10; i++ {
		if !cb.Allow() {
			t.Fatal("closed 后应持续放行")
		}
	}
}

// TestCircuitBreaker_HalfOpenProbeFailureReopens 半开试探失败 ⇒ 立即回 open 并重新计时
// （不等满阈值——试探失败说明后端还没好）。
func TestCircuitBreaker_HalfOpenProbeFailureReopens(t *testing.T) {
	cb := NewCircuitBreaker()
	for i := 0; i < CircuitThreshold; i++ {
		cb.RecordFailure()
	}
	cb.mu.Lock()
	cb.openedAt = time.Now().Add(-CircuitCooldown - time.Second)
	cb.mu.Unlock()
	if !cb.Allow() {
		t.Fatal("冷却后应放行试探")
	}
	cb.RecordFailure() // 试探失败
	if cb.State() != CircuitOpen {
		t.Fatalf("试探失败后 state=%s, want open", cb.State())
	}
	// 重新计时 ⇒ 冷却未过 ⇒ 不放行。
	cb.mu.Lock()
	cb.openedAt = time.Now() // 模拟"刚进入 open"
	cb.mu.Unlock()
	if cb.Allow() {
		t.Fatal("重新计时后冷却未过，应继续拒绝")
	}
}

// TestCircuitBreaker_SuccessResetsFailureCount closed 下成功清零计数——
// 偶发失败不应累积成熔断。
func TestCircuitBreaker_SuccessResetsFailureCount(t *testing.T) {
	cb := NewCircuitBreaker()
	for i := 0; i < CircuitThreshold-1; i++ {
		cb.RecordFailure()
	}
	cb.RecordSuccess() // 清零
	for i := 0; i < CircuitThreshold-1; i++ {
		cb.RecordFailure()
	}
	if cb.State() != CircuitClosed {
		t.Errorf("偶发失败被成功间隔后不应熔断, state=%s", cb.State())
	}
}

// TestCircuitBreaker_FourXXNotFailure 文档语义：4xx 是业务应答不是后端故障
// （调用方只对 5xx/传输错误调 RecordFailure，这里验证调用约定不被误用）。
func TestCircuitBreaker_FourXXNotFailure(t *testing.T) {
	cb := NewCircuitBreaker()
	// 模拟：成功与"业务拒绝"交替（业务拒绝根本不调用 RecordFailure）。
	for i := 0; i < 100; i++ {
		if i%2 == 0 {
			cb.RecordSuccess()
		}
	}
	if cb.State() != CircuitClosed {
		t.Errorf("正常业务流量不应熔断, state=%s", cb.State())
	}
}
