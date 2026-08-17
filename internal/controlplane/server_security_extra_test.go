package controlplane

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"opsmesh/internal/config"
	"opsmesh/internal/store"
)

// 本文件补全 server_security.go 中 0% 覆盖的限流器：
//   - newRateLimiter / allow / sweepLoop / rateLimitMiddleware

func TestRateLimiter_AllowWithinBurst(t *testing.T) {
	rl := newRateLimiter(5, time.Hour)
	// 容量=5，前 5 次应全部放行
	for i := 0; i < 5; i++ {
		if !rl.allow("1.2.3.4") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
}

func TestRateLimiter_DenyOverBurst(t *testing.T) {
	rl := newRateLimiter(2, time.Hour)
	rl.allow("1.2.3.4")
	rl.allow("1.2.3.4")
	if rl.allow("1.2.3.4") {
		t.Error("3rd request should be denied")
	}
}

func TestRateLimiter_DifferentIPs(t *testing.T) {
	rl := newRateLimiter(1, time.Hour)
	if !rl.allow("1.1.1.1") {
		t.Error("first IP should be allowed")
	}
	if !rl.allow("2.2.2.2") {
		t.Error("second IP should be allowed")
	}
}

func TestRateLimiter_Refill(t *testing.T) {
	rl := newRateLimiter(10, time.Hour)
	// 消耗一些令牌
	for i := 0; i < 5; i++ {
		rl.allow("ip")
	}
	// 等待令牌补充（10/s 速率，60ms 应补充 ~0.6 个，不够；等待 200ms 补充 2 个）
	time.Sleep(200 * time.Millisecond)
	if !rl.allow("ip") {
		t.Error("should be allowed after refill")
	}
}

func TestRateLimitMiddleware_NoLimiter(t *testing.T) {
	s := &Server{
		store: store.NewMemoryStore(),
		cfg:   &config.Config{Demo: true},
	}
	called := false
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	wrapped := s.rateLimitMiddleware(h)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if !called {
		t.Error("handler should be called when rateLimiter is nil")
	}
}

func TestRateLimitMiddleware_HealthzBypass(t *testing.T) {
	s := &Server{
		store:       store.NewMemoryStore(),
		cfg:         &config.Config{Demo: true},
		rateLimiter: newRateLimiter(1, time.Hour),
	}
	// 先消耗令牌
	s.rateLimiter.allow("1.2.3.4")
	called := false
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	wrapped := s.rateLimitMiddleware(h)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if !called {
		t.Error("healthz should bypass rate limit")
	}
}

func TestRateLimitMiddleware_ReadyzBypass(t *testing.T) {
	s := &Server{
		store:       store.NewMemoryStore(),
		cfg:         &config.Config{Demo: true},
		rateLimiter: newRateLimiter(1, time.Hour),
	}
	s.rateLimiter.allow("1.2.3.4")
	called := false
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	wrapped := s.rateLimitMiddleware(h)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if !called {
		t.Error("readyz should bypass rate limit")
	}
}

func TestRateLimitMiddleware_AllowAndDeny(t *testing.T) {
	s := &Server{
		store:       store.NewMemoryStore(),
		cfg:         &config.Config{Demo: true},
		rateLimiter: newRateLimiter(1, time.Hour),
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := s.rateLimitMiddleware(h)
	// 第一次放行
	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
	req1.RemoteAddr = "1.2.3.4:1234"
	rec1 := httptest.NewRecorder()
	wrapped.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Errorf("first: %d, want 200", rec1.Code)
	}
	// 第二次拒绝
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
	req2.RemoteAddr = "1.2.3.4:1234"
	rec2 := httptest.NewRecorder()
	wrapped.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("second: %d, want 429", rec2.Code)
	}
}
