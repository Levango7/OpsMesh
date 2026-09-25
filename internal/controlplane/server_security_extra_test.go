package controlplane

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/store"
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

// TestRateLimiter_BucketCapBounded 验证 IP 桶数上限（P1-5）：达到上限后不再新建桶（内存有界），
// 但已跟踪的 IP 仍受速率限制，新 IP 不因上限被误杀。
func TestRateLimiter_BucketCapBounded(t *testing.T) {
	// 手工构造（不启动 sweepLoop，避免与 -race 下的字段写入竞争）。
	rl := &rateLimiter{buckets: map[string]*tokenBucket{}, ratePerSec: 1, sweepInterval: time.Hour, maxBuckets: 3}
	for i := 1; i <= 3; i++ {
		if !rl.allow(fmt.Sprintf("10.0.0.%d", i)) {
			t.Fatalf("第 %d 个 IP 首次请求应放行", i)
		}
	}
	if len(rl.buckets) != 3 {
		t.Fatalf("桶数 = %d, want 3", len(rl.buckets))
	}
	// 第 4 个 IP：桶已满 → 放行但不建桶（内存不再增长）。
	if !rl.allow("10.0.0.99") {
		t.Fatal("桶满时新 IP 应放行（不误杀），只是不跟踪")
	}
	if len(rl.buckets) != 3 {
		t.Fatalf("桶数应保持 3（有界），实际 %d", len(rl.buckets))
	}
	if rl.untracked != 1 {
		t.Fatalf("untracked = %d, want 1", rl.untracked)
	}
	// 已跟踪 IP 的超速请求仍被拒绝（限流器对在途攻击依然有效）。
	if rl.allow("10.0.0.1") {
		t.Fatal("已跟踪 IP 超出速率应被拒绝（429）")
	}
}

// TestRateLimiter_BucketCapEvictsIdleFirst 验证桶满时优先清理空闲桶，
// 使活跃期后的新 IP 重新纳入跟踪（而非永久不跟踪）。
func TestRateLimiter_BucketCapEvictsIdleFirst(t *testing.T) {
	rl := &rateLimiter{buckets: map[string]*tokenBucket{}, ratePerSec: 1, sweepInterval: time.Millisecond, maxBuckets: 2}
	rl.allow("10.0.0.1")
	rl.allow("10.0.0.2")
	time.Sleep(5 * time.Millisecond) // 两个桶均超过 sweepInterval → 视为空闲
	if !rl.allow("10.0.0.3") {
		t.Fatal("空闲桶清理后应放行新 IP")
	}
	if _, ok := rl.buckets["10.0.0.3"]; !ok {
		t.Fatal("空闲桶应先被清理，新 IP 应被跟踪")
	}
	if rl.untracked != 0 {
		t.Fatalf("清理出空间后不应出现未跟踪请求，untracked = %d", rl.untracked)
	}
	if len(rl.buckets) != 1 {
		t.Fatalf("桶数 = %d, want 1（两个空闲桶被清理）", len(rl.buckets))
	}
}
