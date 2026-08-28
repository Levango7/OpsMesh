package circuit

import (
	"errors"
	"testing"
	"time"
)

func TestNewBreaker(t *testing.T) {
	b := New("test", 3, 10*time.Second)
	if b.State() != StateClosed {
		t.Fatalf("initial state = %v, want Closed", b.State())
	}
	if b.Name() != "test" {
		t.Fatalf("name = %q, want test", b.Name())
	}
}

func TestBreakerClosedToOpen(t *testing.T) {
	b := New("test", 3, 10*time.Second)
	for i := 0; i < 3; i++ {
		_ = b.Execute(func() error {
			return errors.New("fail")
		})
	}
	if b.State() != StateOpen {
		t.Fatalf("state = %v, want Open", b.State())
	}
}

func TestBreakerOpenRejectsCalls(t *testing.T) {
	b := New("test", 1, 10*time.Second)
	_ = b.Execute(func() error {
		return errors.New("fail")
	})
	if b.State() != StateOpen {
		t.Fatalf("state = %v, want Open", b.State())
	}
	err := b.Execute(func() error {
		t.Fatal("should not be called")
		return nil
	})
	if err != ErrOpen {
		t.Fatalf("err = %v, want ErrOpen", err)
	}
}

func TestBreakerHalfOpenToClosed(t *testing.T) {
	b := New("test", 1, 50*time.Millisecond)
	_ = b.Execute(func() error {
		return errors.New("fail")
	})
	if b.State() != StateOpen {
		t.Fatalf("state = %v, want Open", b.State())
	}
	time.Sleep(100 * time.Millisecond)
	if b.State() != StateHalfOpen {
		t.Fatalf("state after timeout = %v, want HalfOpen", b.State())
	}
	err := b.Execute(func() error {
		return nil
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if b.State() != StateClosed {
		t.Fatalf("state after success = %v, want Closed", b.State())
	}
}

func TestBreakerHalfOpenToOpen(t *testing.T) {
	b := New("test", 1, 50*time.Millisecond)
	_ = b.Execute(func() error {
		return errors.New("fail")
	})
	time.Sleep(100 * time.Millisecond)
	err := b.Execute(func() error {
		return errors.New("fail again")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if b.State() != StateOpen {
		t.Fatalf("state = %v, want Open", b.State())
	}
}

func TestBreakerSuccessResetsFailures(t *testing.T) {
	b := New("test", 3, 10*time.Second)
	_ = b.Execute(func() error { return errors.New("fail") })
	_ = b.Execute(func() error { return errors.New("fail") })
	_ = b.Execute(func() error { return nil })
	_ = b.Execute(func() error { return errors.New("fail") })
	if b.State() != StateClosed {
		t.Fatalf("state = %v, want Closed (failures reset after success)", b.State())
	}
}

func TestBreakerReset(t *testing.T) {
	b := New("test", 1, 10*time.Second)
	_ = b.Execute(func() error { return errors.New("fail") })
	b.Reset()
	if b.State() != StateClosed {
		t.Fatalf("state after reset = %v, want Closed", b.State())
	}
	err := b.Execute(func() error { return nil })
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

func TestBreakerExecuteReturnsFnError(t *testing.T) {
	b := New("test", 5, 10*time.Second)
	want := errors.New("specific error")
	err := b.Execute(func() error { return want })
	if err != want {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestBreakerConcurrent(t *testing.T) {
	b := New("test", 100, 10*time.Second)
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = b.Execute(func() error { return nil })
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
	if b.State() != StateClosed {
		t.Fatalf("state = %v, want Closed", b.State())
	}
}

func TestBreakerStateString(t *testing.T) {
	if StateClosed != 0 || StateOpen != 1 || StateHalfOpen != 2 {
		t.Fatal("state constants changed")
	}
}

func TestBreakerMaxFailuresZero(t *testing.T) {
	b := New("test", 0, 10*time.Second)
	// maxFailures=0 means it opens on first failure.
	_ = b.Execute(func() error { return errors.New("fail") })
	if b.State() != StateOpen {
		t.Fatalf("state = %v, want Open", b.State())
	}
}

func TestBreakerTimeoutZero(t *testing.T) {
	b := New("test", 1, 0)
	_ = b.Execute(func() error { return errors.New("fail") })
	// With timeout=0, it should immediately allow half-open.
	err := b.Execute(func() error { return nil })
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

func TestBreakerNilFnResult(t *testing.T) {
	b := New("test", 1, 10*time.Second)
	err := b.Execute(func() error { return nil })
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

func TestBreakerFailureCount(t *testing.T) {
	b := New("test", 5, 10*time.Second)
	for i := 0; i < 4; i++ {
		_ = b.Execute(func() error { return errors.New("fail") })
	}
	if b.State() != StateClosed {
		t.Fatalf("state = %v, want Closed (4 < 5)", b.State())
	}
	_ = b.Execute(func() error { return errors.New("fail") })
	if b.State() != StateOpen {
		t.Fatalf("state = %v, want Open (5 >= 5)", b.State())
	}
}

func TestBreakerOpenToHalfOpenTransition(t *testing.T) {
	b := New("test", 1, 100*time.Millisecond)
	_ = b.Execute(func() error { return errors.New("fail") })
	if b.State() != StateOpen {
		t.Fatal("should be open")
	}
	// Before timeout, should still be open.
	time.Sleep(50 * time.Millisecond)
	if b.State() != StateOpen {
		t.Fatal("should still be open before timeout")
	}
	// After timeout, should transition to half-open on next call.
	time.Sleep(100 * time.Millisecond)
	_ = b.Execute(func() error { return nil })
	if b.State() != StateClosed {
		t.Fatalf("state = %v, want Closed", b.State())
	}
}

func TestBreakerMultipleCycles(t *testing.T) {
	b := New("test", 1, 50*time.Millisecond)
	for i := 0; i < 5; i++ {
		_ = b.Execute(func() error { return errors.New("fail") })
		if b.State() != StateOpen {
			t.Fatalf("cycle %d: state = %v, want Open", i, b.State())
		}
		time.Sleep(100 * time.Millisecond)
		_ = b.Execute(func() error { return nil })
		if b.State() != StateClosed {
			t.Fatalf("cycle %d: state = %v, want Closed", i, b.State())
		}
	}
}

func TestBreakerExecutePropagatesError(t *testing.T) {
	b := New("test", 5, 10*time.Second)
	expected := errors.New("test error")
	var got error
	for i := 0; i < 3; i++ {
		got = b.Execute(func() error { return expected })
	}
	if got != expected {
		t.Fatalf("err = %v, want %v", got, expected)
	}
}

func TestBreakerConcurrentOpenClose(t *testing.T) {
	b := New("test", 50, 10*time.Millisecond)
	done := make(chan struct{})
	for i := 0; i < 20; i++ {
		go func() {
			for j := 0; j < 200; j++ {
				_ = b.Execute(func() error {
					if j%3 == 0 {
						return errors.New("fail")
					}
					return nil
				})
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 20; i++ {
		<-done
	}
	// Just verify it doesn't panic or deadlock.
	_ = b.State()
}

func BenchmarkBreakerExecute(b *testing.B) {
	br := New("bench", 100, time.Second)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = br.Execute(func() error { return nil })
	}
}
