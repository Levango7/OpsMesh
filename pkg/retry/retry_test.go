package retry

import (
	"errors"
	"testing"
	"time"
)

func TestDoSuccess(t *testing.T) {
	calls := 0
	err := Do(func() error {
		calls++
		return nil
	}, 3, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestDoRetryThenSuccess(t *testing.T) {
	calls := 0
	err := Do(func() error {
		calls++
		if calls < 3 {
			return errors.New("transient")
		}
		return nil
	}, 5, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestDoExhaustRetries(t *testing.T) {
	calls := 0
	err := Do(func() error {
		calls++
		return errors.New("persistent")
	}, 3, 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 4 { // 1 initial + 3 retries
		t.Fatalf("calls = %d, want 4", calls)
	}
}

func TestDoZeroRetries(t *testing.T) {
	calls := 0
	err := Do(func() error {
		calls++
		return errors.New("fail")
	}, 0, 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestDoWithConfigRetryable(t *testing.T) {
	cfg := Config{
		MaxRetries:    3,
		InitialDelay:  10 * time.Millisecond,
		BackoffFactor: 2.0,
		Jitter:        0,
		ShouldRetry: func(err error) bool {
			return IsRetryable(err)
		},
	}
	calls := 0
	err := DoWithConfig(func() error {
		calls++
		if calls < 3 {
			return Retryable(errors.New("transient"))
		}
		return nil
	}, cfg)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestDoWithConfigNonRetryable(t *testing.T) {
	cfg := Config{
		MaxRetries:    3,
		InitialDelay:  10 * time.Millisecond,
		BackoffFactor: 2.0,
		Jitter:        0,
		ShouldRetry: func(err error) bool {
			return IsRetryable(err)
		},
	}
	calls := 0
	err := DoWithConfig(func() error {
		calls++
		return errors.New("permanent")
	}, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (non-retryable)", calls)
	}
}

func TestRetryableError(t *testing.T) {
	err := Retryable(errors.New("test"))
	if !IsRetryable(err) {
		t.Fatal("expected retryable")
	}
	var re *RetryableError
	if !errors.As(err, &re) {
		t.Fatal("expected RetryableError type")
	}
}

func TestNonRetryableError(t *testing.T) {
	err := errors.New("test")
	if IsRetryable(err) {
		t.Fatal("expected non-retryable")
	}
}

func TestExponentialBackoff(t *testing.T) {
	cfg := Config{
		MaxRetries:    4,
		InitialDelay:  10 * time.Millisecond,
		MaxDelay:      100 * time.Millisecond,
		BackoffFactor: 2.0,
		Jitter:        0,
	}
	var prev time.Time
	var intervals []time.Duration
	calls := 0
	_ = DoWithConfig(func() error {
		calls++
		now := time.Now()
		if !prev.IsZero() {
			intervals = append(intervals, now.Sub(prev))
		}
		prev = now
		return errors.New("fail")
	}, cfg)
	if len(intervals) != 4 {
		t.Fatalf("intervals = %d, want 4", len(intervals))
	}
	// Verify intervals are increasing (exponential).
	for i := 1; i < len(intervals); i++ {
		if intervals[i] <= intervals[i-1] {
			t.Fatalf("interval %d (%v) not greater than %d (%v)", i, intervals[i], i-1, intervals[i-1])
		}
	}
}

func TestMaxDelayCap(t *testing.T) {
	cfg := Config{
		MaxRetries:    10,
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      200 * time.Millisecond,
		BackoffFactor: 10.0,
		Jitter:        0,
	}
	var maxInterval time.Duration
	var prev time.Time
	calls := 0
	_ = DoWithConfig(func() error {
		calls++
		now := time.Now()
		if !prev.IsZero() {
			d := now.Sub(prev)
			if d > maxInterval {
				maxInterval = d
			}
		}
		prev = now
		return errors.New("fail")
	}, cfg)
	if maxInterval > 300*time.Millisecond {
		t.Fatalf("max interval %v exceeds cap", maxInterval)
	}
}

func TestApplyJitter(t *testing.T) {
	delay := 100 * time.Millisecond
	for i := 0; i < 100; i++ {
		d := applyJitter(delay, 0.2)
		if d < 80*time.Millisecond || d > 120*time.Millisecond {
			t.Fatalf("jittered delay %v out of range [80ms, 120ms]", d)
		}
	}
}

func TestApplyJitterZero(t *testing.T) {
	delay := 100 * time.Millisecond
	d := applyJitter(delay, 0)
	if d != delay {
		t.Fatalf("jitter=0 should not change delay: got %v, want %v", d, delay)
	}
}

func TestApplyJitterOne(t *testing.T) {
	delay := 100 * time.Millisecond
	for i := 0; i < 100; i++ {
		d := applyJitter(delay, 1.0)
		if d < 0 || d > 200*time.Millisecond {
			t.Fatalf("jitter=1 delay %v out of range [0, 200ms]", d)
		}
	}
}

func TestDoWithConfigDefaultRetryAll(t *testing.T) {
	cfg := DefaultConfig()
	cfg.InitialDelay = 10 * time.Millisecond
	calls := 0
	err := DoWithConfig(func() error {
		calls++
		if calls < 2 {
			return errors.New("transient")
		}
		return nil
	}, cfg)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

func TestDoWithConfigInvalidBackoff(t *testing.T) {
	cfg := Config{
		MaxRetries:    1,
		InitialDelay:  10 * time.Millisecond,
		BackoffFactor: -1,
	}
	calls := 0
	_ = DoWithConfig(func() error {
		calls++
		return errors.New("fail")
	}, cfg)
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestRetryableUnwrap(t *testing.T) {
	inner := errors.New("inner")
	wrapped := Retryable(inner)
	if errors.Unwrap(wrapped) != inner {
		t.Fatalf("unwrap = %v, want %v", errors.Unwrap(wrapped), inner)
	}
}

func TestDoReturnsLastError(t *testing.T) {
	err := Do(func() error {
		return errors.New("final error")
	}, 2, 10*time.Millisecond)
	if err == nil || err.Error() != "final error" {
		t.Fatalf("err = %v, want 'final error'", err)
	}
}

func TestDoConcurrent(t *testing.T) {
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			_ = Do(func() error { return nil }, 3, 1*time.Millisecond)
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestDoWithConfigZeroMaxDelay(t *testing.T) {
	cfg := Config{
		MaxRetries:    2,
		InitialDelay:  10 * time.Millisecond,
		MaxDelay:      0,
		BackoffFactor: 2.0,
	}
	calls := 0
	_ = DoWithConfig(func() error {
		calls++
		return errors.New("fail")
	}, cfg)
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestDoWithConfigNegativeBackoff(t *testing.T) {
	cfg := Config{
		MaxRetries:    2,
		InitialDelay:  10 * time.Millisecond,
		BackoffFactor: -1,
	}
	calls := 0
	_ = DoWithConfig(func() error {
		calls++
		return errors.New("fail")
	}, cfg)
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

// recordSleeps 临时把 sleep 换成记录器（不真实等待），返回记录到的退避时长。
func recordSleeps(t *testing.T) *[]time.Duration {
	t.Helper()
	orig := sleep
	var got []time.Duration
	sleep = func(d time.Duration) { got = append(got, d) }
	t.Cleanup(func() { sleep = orig })
	return &got
}

func TestDoWithConfigJitterBounds(t *testing.T) {
	cfg := Config{
		MaxRetries:    100,
		InitialDelay:  1 * time.Millisecond,
		MaxDelay:      1 * time.Millisecond,
		BackoffFactor: 1.0,
		Jitter:        0.5,
	}
	// 名实相符：记录每次退避时长并断言落在 jitter 区间 [0.5ms, 1.5ms]。
	// 原实现连跑 100×100 次真实 sleep（实测 16s）却只数调用次数、没验证 jitter 边界。
	got := recordSleeps(t)
	for i := 0; i < 100; i++ {
		calls := 0
		_ = DoWithConfig(func() error {
			calls++
			return errors.New("fail")
		}, cfg)
		if calls != 101 {
			t.Fatalf("calls = %d, want 101", calls)
		}
	}
	if want := 100 * 100; len(*got) != want {
		t.Fatalf("记录到 %d 次退避，want %d", len(*got), want)
	}
	lo := time.Duration(float64(cfg.InitialDelay) * (1 - cfg.Jitter)) // 0.5ms
	hi := time.Duration(float64(cfg.InitialDelay) * (1 + cfg.Jitter)) // 1.5ms
	for i, d := range *got {
		if d < lo || d > hi {
			t.Fatalf("第 %d 次退避 %v 超出 jitter 区间 [%v, %v]", i, d, lo, hi)
		}
	}
}

func TestDoWithConfigRetryableThenSuccess(t *testing.T) {
	cfg := Config{
		MaxRetries:    5,
		InitialDelay:  10 * time.Millisecond,
		BackoffFactor: 2.0,
		ShouldRetry: func(err error) bool {
			return err.Error() == "retry"
		},
	}
	calls := 0
	err := DoWithConfig(func() error {
		calls++
		if calls < 4 {
			return errors.New("retry")
		}
		return nil
	}, cfg)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if calls != 4 {
		t.Fatalf("calls = %d, want 4", calls)
	}
}

func TestDoWithConfigNonRetryableImmediate(t *testing.T) {
	cfg := Config{
		MaxRetries:    5,
		InitialDelay:  10 * time.Millisecond,
		BackoffFactor: 2.0,
		ShouldRetry: func(err error) bool {
			return err.Error() == "retry"
		},
	}
	calls := 0
	err := DoWithConfig(func() error {
		calls++
		return errors.New("permanent")
	}, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func BenchmarkDo(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = Do(func() error { return nil }, 3, 1*time.Millisecond)
	}
}

func BenchmarkDoWithConfig(b *testing.B) {
	cfg := DefaultConfig()
	cfg.InitialDelay = 1 * time.Millisecond
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DoWithConfig(func() error { return nil }, cfg)
	}
}
