package notify

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// mockChannel mock Channel 实现，用于测试重试逻辑。
// failCount: 前 N 次返回 error，之后返回 nil（模拟临时故障后恢复）。
// 若 failCount < 0 则永远失败。
type mockChannel struct {
	calls     int32 // Send 调用次数（原子计数）
	failCount int32 // 前 N 次失败（-1=永远失败）
}

func (m *mockChannel) Send(msg *Message) error {
	n := atomic.AddInt32(&m.calls, 1)
	if m.failCount < 0 {
		return errors.New("permanent failure")
	}
	if n <= m.failCount {
		return errors.New("transient failure")
	}
	return nil
}

// TestSendWithRetry_SuccessFirstTry 验证首次成功不重试。
func TestSendWithRetry_SuccessFirstTry(t *testing.T) {
	ch := &mockChannel{failCount: 0}
	policy := &RetryPolicy{MaxAttempts: 3, Interval: 1 * time.Millisecond, Backoff: 2.0}
	if err := SendWithRetry(ch, &Message{Title: "x"}, policy); err != nil {
		t.Fatalf("SendWithRetry err = %v", err)
	}
	if atomic.LoadInt32(&ch.calls) != 1 {
		t.Fatalf("calls = %d, want 1 (success on first try)", ch.calls)
	}
}

// TestSendWithRetry_RetryThenSuccess 验证失败后重试成功。
func TestSendWithRetry_RetryThenSuccess(t *testing.T) {
	ch := &mockChannel{failCount: 2} // 前 2 次失败，第 3 次成功
	policy := &RetryPolicy{MaxAttempts: 5, Interval: 1 * time.Millisecond, Backoff: 2.0}
	if err := SendWithRetry(ch, &Message{Title: "x"}, policy); err != nil {
		t.Fatalf("SendWithRetry err = %v", err)
	}
	if atomic.LoadInt32(&ch.calls) != 3 {
		t.Fatalf("calls = %d, want 3 (2 failures + 1 success)", ch.calls)
	}
}

// TestSendWithRetry_MaxAttemptsExceeded 验证达到最大次数后放弃。
func TestSendWithRetry_MaxAttemptsExceeded(t *testing.T) {
	ch := &mockChannel{failCount: -1} // 永远失败
	policy := &RetryPolicy{MaxAttempts: 3, Interval: 1 * time.Millisecond, Backoff: 2.0}
	err := SendWithRetry(ch, &Message{Title: "x"}, policy)
	if err == nil {
		t.Fatal("expected error after max attempts")
	}
	if !strings.Contains(err.Error(), "after 3 attempts") {
		t.Fatalf("error should mention 3 attempts: %v", err)
	}
	if atomic.LoadInt32(&ch.calls) != 3 {
		t.Fatalf("calls = %d, want 3 (max attempts)", ch.calls)
	}
}

// TestSendWithRetry_NilChannel 验证 nil channel 静默返回 nil。
func TestSendWithRetry_NilChannel(t *testing.T) {
	if err := SendWithRetry(nil, &Message{Title: "x"}, nil); err != nil {
		t.Fatalf("nil channel err = %v", err)
	}
}

// TestSendWithRetry_NilMsg 验证 nil msg 静默返回 nil。
func TestSendWithRetry_NilMsg(t *testing.T) {
	ch := &mockChannel{}
	if err := SendWithRetry(ch, nil, nil); err != nil {
		t.Fatalf("nil msg err = %v", err)
	}
	if atomic.LoadInt32(&ch.calls) != 0 {
		t.Fatalf("calls = %d, want 0 (nil msg skipped)", ch.calls)
	}
}

// TestSendWithRetry_NilPolicy 验证 nil policy 使用默认策略。
func TestSendWithRetry_NilPolicy(t *testing.T) {
	ch := &mockChannel{failCount: 0}
	// nil policy → DefaultRetryPolicy（MaxAttempts=3）
	// 但默认 Interval=5s，测试中首次即成功，不会等待
	if err := SendWithRetry(ch, &Message{Title: "x"}, nil); err != nil {
		t.Fatalf("SendWithRetry err = %v", err)
	}
	if atomic.LoadInt32(&ch.calls) != 1 {
		t.Fatalf("calls = %d, want 1", ch.calls)
	}
}

// TestSendWithRetry_ZeroPolicyFields 验证零值字段使用默认。
func TestSendWithRetry_ZeroPolicyFields(t *testing.T) {
	ch := &mockChannel{failCount: 0}
	policy := &RetryPolicy{MaxAttempts: 0, Interval: 0, Backoff: 0} // 全零
	if err := SendWithRetry(ch, &Message{Title: "x"}, policy); err != nil {
		t.Fatalf("SendWithRetry err = %v", err)
	}
	// 首次成功，不触发重试，所以零值不影响
	if atomic.LoadInt32(&ch.calls) != 1 {
		t.Fatalf("calls = %d, want 1", ch.calls)
	}
}

// TestSendWithRetryContext_Cancelled 验证 ctx 取消时停止重试。
func TestSendWithRetryContext_Cancelled(t *testing.T) {
	ch := &mockChannel{failCount: -1} // 永远失败
	ctx, cancel := context.WithCancel(context.Background())
	// 立即取消（在首次发送前）
	cancel()
	policy := &RetryPolicy{MaxAttempts: 10, Interval: 1 * time.Millisecond, Backoff: 2.0}
	err := SendWithRetryContext(ctx, ch, &Message{Title: "x"}, policy)
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
	if !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("error should mention cancelled: %v", err)
	}
}

// TestSendWithRetryContext_CancelDuringBackoff 验证退避等待期间取消。
func TestSendWithRetryContext_CancelDuringBackoff(t *testing.T) {
	ch := &mockChannel{failCount: -1} // 永远失败
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	policy := &RetryPolicy{MaxAttempts: 10, Interval: 100 * time.Millisecond, Backoff: 2.0}
	// 在首次失败后、退避等待期间取消
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	err := SendWithRetryContext(ctx, ch, &Message{Title: "x"}, policy)
	if err == nil {
		t.Fatal("expected error on cancelled context during backoff")
	}
}

// TestRetryPolicy_BackoffDuration 验证退避时长计算。
func TestRetryPolicy_BackoffDuration(t *testing.T) {
	p := RetryPolicy{Interval: 1 * time.Second, Backoff: 2.0}
	// 第 1 次重试前等待 Interval
	if d := p.backoffDuration(1); d != 1*time.Second {
		t.Fatalf("backoff(1) = %v, want 1s", d)
	}
	// 第 2 次重试前等待 Interval*Backoff = 2s
	if d := p.backoffDuration(2); d != 2*time.Second {
		t.Fatalf("backoff(2) = %v, want 2s", d)
	}
	// 第 3 次重试前等待 Interval*Backoff^2 = 4s
	if d := p.backoffDuration(3); d != 4*time.Second {
		t.Fatalf("backoff(3) = %v, want 4s", d)
	}
}

// TestRetryPolicy_Normalize 验证零值规范化。
func TestRetryPolicy_Normalize(t *testing.T) {
	p := RetryPolicy{}.normalize()
	if p.MaxAttempts != 3 {
		t.Fatalf("MaxAttempts = %d, want 3", p.MaxAttempts)
	}
	if p.Interval != 5*time.Second {
		t.Fatalf("Interval = %v, want 5s", p.Interval)
	}
	if p.Backoff != 2.0 {
		t.Fatalf("Backoff = %v, want 2.0", p.Backoff)
	}
}
