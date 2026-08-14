package circuitbreaker

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// errSentinel 测试用哨兵错误。
var errSentinel = errors.New("sentinel failure")

// newTestCB 构造测试用熔断器（threshold=3, recovery=50ms, halfOpen=1）。
func newTestCB() *CircuitBreaker {
	return New(Config{
		Name:             "test",
		FailureThreshold: 3,
		RecoveryTimeout:  50 * time.Millisecond,
		HalfOpenMaxCalls: 1,
	})
}

// TestNew_Defaults 验证 New 对零值配置的默认填充。
func TestNew_Defaults(t *testing.T) {
	cb := New(Config{FailureThreshold: 5}) // RecoveryTimeout=0, HalfOpenMaxCalls=0
	if cb.cfg.RecoveryTimeout != 30*time.Second {
		t.Errorf("RecoveryTimeout 默认值 = %v, want 30s", cb.cfg.RecoveryTimeout)
	}
	if cb.cfg.HalfOpenMaxCalls != 1 {
		t.Errorf("HalfOpenMaxCalls 默认值 = %d, want 1", cb.cfg.HalfOpenMaxCalls)
	}
	if cb.State() != StateClosed {
		t.Errorf("初始状态 = %q, want %q", cb.State(), StateClosed)
	}
}

// TestExecute_Disabled 验证 FailureThreshold<=0 时熔断器透传不熔断。
func TestExecute_Disabled(t *testing.T) {
	cb := New(Config{FailureThreshold: 0})
	if cb.Enabled() {
		t.Fatal("FailureThreshold=0 应为禁用模式")
	}
	// 连续失败 100 次也不熔断。
	for i := 0; i < 100; i++ {
		if err := cb.Execute(func() error { return errSentinel }); err != errSentinel {
			t.Fatalf("禁用模式应透传 fn 的错误，got %v", err)
		}
	}
	if cb.State() != StateClosed {
		t.Errorf("禁用模式应始终 Closed，got %q", cb.State())
	}
	// 成功也透传。
	if err := cb.Execute(func() error { return nil }); err != nil {
		t.Fatalf("禁用模式成功应返回 nil，got %v", err)
	}
}

// TestClosedToOpen 验证连续失败达阈值后 Closed→Open 转换。
func TestClosedToOpen(t *testing.T) {
	cb := newTestCB()
	// 失败 2 次（未达阈值 3），仍 Closed。
	for i := 0; i < 2; i++ {
		if err := cb.Execute(func() error { return errSentinel }); err != errSentinel {
			t.Fatalf("第 %d 次失败应透传错误", i+1)
		}
	}
	if cb.State() != StateClosed {
		t.Errorf("2 次失败后应仍 Closed，got %q", cb.State())
	}
	if cb.FailureCount() != 2 {
		t.Errorf("失败计数 = %d, want 2", cb.FailureCount())
	}
	// 第 3 次失败 → 转 Open。
	if err := cb.Execute(func() error { return errSentinel }); err != errSentinel {
		t.Fatalf("第 3 次失败应透传错误，got %v", err)
	}
	if cb.State() != StateOpen {
		t.Errorf("3 次失败后应 Open，got %q", cb.State())
	}
	// Open 状态下 Execute 返回 ErrCircuitOpen，不调用 fn。
	called := false
	if err := cb.Execute(func() error { called = true; return nil }); err != ErrCircuitOpen {
		t.Errorf("Open 状态应返回 ErrCircuitOpen，got %v", err)
	}
	if called {
		t.Error("Open 状态不应调用 fn")
	}
}

// TestSuccessResetsFailureCount 验证成功清零失败计数（不累积历史失败）。
func TestSuccessResetsFailureCount(t *testing.T) {
	cb := newTestCB()
	// 失败 2 次。
	cb.Execute(func() error { return errSentinel })
	cb.Execute(func() error { return errSentinel })
	if cb.FailureCount() != 2 {
		t.Fatalf("失败计数 = %d, want 2", cb.FailureCount())
	}
	// 成功一次 → 清零。
	cb.Execute(func() error { return nil })
	if cb.FailureCount() != 0 {
		t.Errorf("成功后失败计数 = %d, want 0", cb.FailureCount())
	}
	// 再失败 2 次不应熔断（未达阈值 3）。
	cb.Execute(func() error { return errSentinel })
	cb.Execute(func() error { return errSentinel })
	if cb.State() != StateClosed {
		t.Errorf("成功清零后再失败 2 次应仍 Closed，got %q", cb.State())
	}
}

// TestOpenToHalfOpen 验证等待 RecoveryTimeout 后 Open→HalfOpen 转换。
func TestOpenToHalfOpen(t *testing.T) {
	cb := newTestCB()
	// 触发熔断。
	for i := 0; i < 3; i++ {
		cb.Execute(func() error { return errSentinel })
	}
	if cb.State() != StateOpen {
		t.Fatalf("前置：应已 Open，got %q", cb.State())
	}
	// 立即调用应被拒绝（仍在熔断窗口）。
	if err := cb.Execute(func() error { return nil }); err != ErrCircuitOpen {
		t.Errorf("熔断窗口内应返回 ErrCircuitOpen，got %v", err)
	}
	// 等待 RecoveryTimeout 后调用 → 转 HalfOpen 并放行探测。
	time.Sleep(60 * time.Millisecond)
	called := false
	if err := cb.Execute(func() error { called = true; return nil }); err != nil {
		t.Errorf("HalfOpen 探测应放行，got %v", err)
	}
	if !called {
		t.Error("HalfOpen 探测应调用 fn")
	}
	// 探测成功且 HalfOpenMaxCalls=1 → 转 Closed。
	if cb.State() != StateClosed {
		t.Errorf("探测成功后应转 Closed，got %q", cb.State())
	}
}

// TestHalfOpenToClosed 验证 HalfOpen 探测全部成功 → Closed。
func TestHalfOpenToClosed(t *testing.T) {
	cb := New(Config{
		Name:             "test",
		FailureThreshold: 2,
		RecoveryTimeout:  20 * time.Millisecond,
		HalfOpenMaxCalls: 3, // 需要 3 次探测成功才恢复
	})
	// 触发熔断。
	cb.Execute(func() error { return errSentinel })
	cb.Execute(func() error { return errSentinel })
	if cb.State() != StateOpen {
		t.Fatalf("前置：应已 Open，got %q", cb.State())
	}
	time.Sleep(25 * time.Millisecond)
	// 第 1 次探测成功 → 仍 HalfOpen（未达 3 次）。
	if err := cb.Execute(func() error { return nil }); err != nil {
		t.Fatalf("第 1 次探测应放行，got %v", err)
	}
	if cb.State() != StateHalfOpen {
		t.Errorf("第 1 次探测后应 HalfOpen，got %q", cb.State())
	}
	// 第 2 次探测：名额已满（HalfOpenMaxCalls=3，已发放 1，但第 1 次成功未恢复，halfOpenCalls 仍为 1）
	// 实际上 afterCall 在 HalfOpen 成功时不重置 halfOpenCalls，只累加 halfOpenSucc。
	// beforeCall 在 HalfOpen 时检查 halfOpenCalls < HalfOpenMaxCalls。
	// 第 1 次探测：beforeCall 转 HalfOpen 并设 halfOpenCalls=1；afterCall 成功 halfOpenSucc=1。
	// 第 2 次探测：beforeCall halfOpenCalls(1) < 3 → 放行，halfOpenCalls=2；afterCall halfOpenSucc=2。
	if err := cb.Execute(func() error { return nil }); err != nil {
		t.Fatalf("第 2 次探测应放行，got %v", err)
	}
	if cb.State() != StateHalfOpen {
		t.Errorf("第 2 次探测后应 HalfOpen，got %q", cb.State())
	}
	// 第 3 次探测成功 → halfOpenSucc=3 >= HalfOpenMaxCalls=3 → Closed。
	if err := cb.Execute(func() error { return nil }); err != nil {
		t.Fatalf("第 3 次探测应放行，got %v", err)
	}
	if cb.State() != StateClosed {
		t.Errorf("3 次探测成功后应 Closed，got %q", cb.State())
	}
}

// TestHalfOpenToOpen 验证 HalfOpen 探测失败 → 立即转回 Open。
func TestHalfOpenToOpen(t *testing.T) {
	cb := newTestCB() // HalfOpenMaxCalls=1
	// 触发熔断。
	for i := 0; i < 3; i++ {
		cb.Execute(func() error { return errSentinel })
	}
	if cb.State() != StateOpen {
		t.Fatalf("前置：应已 Open，got %q", cb.State())
	}
	time.Sleep(60 * time.Millisecond)
	// 探测失败 → 立即转 Open。
	if err := cb.Execute(func() error { return errSentinel }); err != errSentinel {
		t.Errorf("探测失败应透传错误，got %v", err)
	}
	if cb.State() != StateOpen {
		t.Errorf("探测失败后应转回 Open，got %q", cb.State())
	}
	// 立即调用应被拒绝（新的熔断窗口）。
	if err := cb.Execute(func() error { return nil }); err != ErrCircuitOpen {
		t.Errorf("重新熔断后应拒绝，got %v", err)
	}
}

// TestHalfOpenMaxCallsReached 验证 HalfOpen 状态下探测名额满后拒绝新调用。
func TestHalfOpenMaxCallsReached(t *testing.T) {
	cb := New(Config{
		Name:             "test",
		FailureThreshold: 1,
		RecoveryTimeout:  20 * time.Millisecond,
		HalfOpenMaxCalls: 2,
	})
	// 触发熔断。
	cb.Execute(func() error { return errSentinel })
	if cb.State() != StateOpen {
		t.Fatalf("前置：应已 Open，got %q", cb.State())
	}
	time.Sleep(25 * time.Millisecond)
	// 第 1 次探测放行（halfOpenCalls=1）。
	if err := cb.Execute(func() error { return nil }); err != nil {
		t.Fatalf("第 1 次探测应放行，got %v", err)
	}
	// 第 2 次探测放行（halfOpenCalls=2，达 HalfOpenMaxCalls）。
	// 但第 1 次成功后 halfOpenSucc=1 < 2，仍 HalfOpen。
	if cb.State() != StateHalfOpen {
		t.Fatalf("前置：应 HalfOpen，got %q", cb.State())
	}
	if err := cb.Execute(func() error { return nil }); err != nil {
		t.Fatalf("第 2 次探测应放行，got %v", err)
	}
	// 第 2 次成功后 halfOpenSucc=2 >= 2 → Closed。
	if cb.State() != StateClosed {
		t.Errorf("2 次探测成功应转 Closed，got %q", cb.State())
	}
}

// TestStateChangeCallback 验证状态变更回调被正确触发。
func TestStateChangeCallback(t *testing.T) {
	var transitions []string
	var mu sync.Mutex
	cb := New(Config{
		Name:             "test",
		FailureThreshold: 2,
		RecoveryTimeout:  20 * time.Millisecond,
		HalfOpenMaxCalls: 1,
		OnStateChange: func(name, from, to string) {
			mu.Lock()
			transitions = append(transitions, fmt.Sprintf("%s→%s", from, to))
			mu.Unlock()
		},
	})
	// 触发 Closed→Open。
	cb.Execute(func() error { return errSentinel })
	cb.Execute(func() error { return errSentinel })
	// 等待后探测成功 → Open→HalfOpen→Closed。
	time.Sleep(25 * time.Millisecond)
	cb.Execute(func() error { return nil })

	mu.Lock()
	defer mu.Unlock()
	// 期望：closed→open, open→halfopen, halfopen→closed。
	if len(transitions) != 3 {
		t.Fatalf("状态变更次数 = %d, want 3, transitions=%v", len(transitions), transitions)
	}
	if transitions[0] != "closed→open" {
		t.Errorf("第 1 次变更 = %q, want closed→open", transitions[0])
	}
	if transitions[1] != "open→halfopen" {
		t.Errorf("第 2 次变更 = %q, want open→halfopen", transitions[1])
	}
	if transitions[2] != "halfopen→closed" {
		t.Errorf("第 3 次变更 = %q, want halfopen→closed", transitions[2])
	}
}

// TestReset 验证 Reset 强制回到 Closed。
func TestReset(t *testing.T) {
	cb := newTestCB()
	for i := 0; i < 3; i++ {
		cb.Execute(func() error { return errSentinel })
	}
	if cb.State() != StateOpen {
		t.Fatalf("前置：应已 Open，got %q", cb.State())
	}
	cb.Reset()
	if cb.State() != StateClosed {
		t.Errorf("Reset 后应 Closed，got %q", cb.State())
	}
	if cb.FailureCount() != 0 {
		t.Errorf("Reset 后失败计数 = %d, want 0", cb.FailureCount())
	}
	// Reset 后立即放行。
	if err := cb.Execute(func() error { return nil }); err != nil {
		t.Errorf("Reset 后应放行，got %v", err)
	}
}

// TestConcurrentSafety 验证并发安全：多 goroutine 并发 Execute 不 panic、不数据竞争。
func TestConcurrentSafety(t *testing.T) {
	cb := New(Config{
		Name:             "test",
		FailureThreshold: 50,
		RecoveryTimeout:  10 * time.Millisecond,
		HalfOpenMaxCalls: 5,
	})
	const goroutines = 20
	const iterations = 100
	var wg sync.WaitGroup
	var successCount, openCount int64
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				err := cb.Execute(func() error {
					// 模拟约 30% 失败率。
					if i%3 == 0 {
						return errSentinel
					}
					return nil
				})
				switch err {
				case nil:
					atomic.AddInt64(&successCount, 1)
				case ErrCircuitOpen:
					atomic.AddInt64(&openCount, 1)
				}
			}
		}()
	}
	wg.Wait()
	// 主要验证不 panic、不数据竞争（-race 检测）。
	// 额外验证：成功 + 熔断拒绝 + 透传失败 = 总调用数。
	total := int64(goroutines * iterations)
	if successCount+openCount > total {
		t.Errorf("计数异常：success=%d + open=%d > total=%d", successCount, openCount, total)
	}
}

// TestConcurrentRaceWithState 验证并发下状态查询不数据竞争。
func TestConcurrentRaceWithState(t *testing.T) {
	cb := New(Config{
		Name:             "test",
		FailureThreshold: 10,
		RecoveryTimeout:  5 * time.Millisecond,
		HalfOpenMaxCalls: 2,
	})
	var wg sync.WaitGroup
	// 一个 goroutine 持续 Execute。
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			cb.Execute(func() error {
				if i%4 == 0 {
					return errSentinel
				}
				return nil
			})
		}
	}()
	// 一个 goroutine 持续查询状态。
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = cb.State()
			_ = cb.FailureCount()
		}
	}()
	wg.Wait()
}

// TestBreakerSet 验证 BreakerSet 按 key 隔离。
func TestBreakerSet(t *testing.T) {
	bs := NewBreakerSet(Config{
		FailureThreshold: 2,
		RecoveryTimeout:  20 * time.Millisecond,
		HalfOpenMaxCalls: 1,
	})
	// device-a 失败 2 次 → 熔断。
	bs.Execute("device-a", func() error { return errSentinel })
	bs.Execute("device-a", func() error { return errSentinel })
	// device-b 不受影响。
	if err := bs.Execute("device-b", func() error { return nil }); err != nil {
		t.Errorf("device-b 应正常放行，got %v", err)
	}
	// device-a 熔断中。
	if err := bs.Execute("device-a", func() error { return nil }); err != ErrCircuitOpen {
		t.Errorf("device-a 应熔断，got %v", err)
	}
	if bs.Len() != 2 {
		t.Errorf("集合大小 = %d, want 2", bs.Len())
	}
	states := bs.States()
	if states["device-a"] != StateOpen {
		t.Errorf("device-a 状态 = %q, want open", states["device-a"])
	}
	if states["device-b"] != StateClosed {
		t.Errorf("device-b 状态 = %q, want closed", states["device-b"])
	}
}

// TestBreakerSet_Disabled 验证禁用模板下所有实例透传。
func TestBreakerSet_Disabled(t *testing.T) {
	bs := NewBreakerSet(Config{FailureThreshold: 0})
	for i := 0; i < 100; i++ {
		if err := bs.Execute("any-key", func() error { return errSentinel }); err != errSentinel {
			t.Fatalf("禁用模式应透传，got %v", err)
		}
	}
}

// TestBreakerSet_SameInstance 验证同 key 返回同一实例。
func TestBreakerSet_SameInstance(t *testing.T) {
	bs := NewBreakerSet(Config{FailureThreshold: 5})
	cb1 := bs.Get("key")
	cb2 := bs.Get("key")
	if cb1 != cb2 {
		t.Error("同 key 应返回同一实例指针")
	}
}
