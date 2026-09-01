// Package ratelimit 的单元测试（白盒测试，可访问 cleanup/ttl 内部字段）。
//
// 覆盖范围：全局令牌桶、按 IP / 按用户限流的正常路径与超限路径、
// 过期条目清理、HTTP 中间件与 gRPC 拦截器的错误语义，以及并发安全。
package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// TestNewGlobalLimiter_BurstThenReject：全局令牌桶先放行 burst 个请求，随后拒绝。
func TestNewGlobalLimiter_BurstThenReject(t *testing.T) {
	// rps=0 意味着令牌不会补充，只消耗初始 burst，便于确定性断言
	l := NewGlobalLimiter(0, 3)
	for i := 1; i <= 3; i++ {
		if !l.Allow() {
			t.Fatalf("第 %d 个请求应被放行（burst=3）", i)
		}
	}
	if l.Allow() {
		t.Fatal("超出 burst 后第 4 个请求应被拒绝")
	}
}

// TestIPLimiter_AllowWithinBurstThenExceeded：每个 IP 独立令牌桶，先放行 burst 个再拒绝。
func TestIPLimiter_AllowWithinBurstThenExceeded(t *testing.T) {
	il := NewIPLimiter(0, 2) // 0 rps：不补充令牌，消耗完 burst 即拒绝
	for i := 1; i <= 2; i++ {
		if !il.Allow("10.0.0.1") {
			t.Fatalf("10.0.0.1 第 %d 个请求应被放行", i)
		}
	}
	if il.Allow("10.0.0.1") {
		t.Fatal("10.0.0.1 超出 burst 后应被拒绝")
	}
	// 不同 IP 拥有独立的令牌桶，不受 10.0.0.1 超限影响
	if !il.Allow("10.0.0.2") {
		t.Fatal("不同 IP 10.0.0.2 应有独立额度")
	}
}

// TestIPLimiter_TokenRefillOverTime：限流桶随时间补充令牌，拒绝后可恢复放行。
func TestIPLimiter_TokenRefillOverTime(t *testing.T) {
	// rps=1000 + burst=1：短窗口内可补充新令牌，避免测试真实等待 1 秒
	il := NewIPLimiter(1000, 1)
	if !il.Allow("10.0.0.1") {
		t.Fatal("第一个请求应被放行")
	}
	// 立即的第二个请求可能被拒（令牌未补充），但补充速率极高，有限重试内必然恢复
	var allowed bool
	for i := 0; i < 200; i++ {
		if il.Allow("10.0.0.1") {
			allowed = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !allowed {
		t.Fatal("令牌应随时间补充并恢复放行")
	}
}

// TestIPLimiter_CleanupRemovesStaleEntries：过期的 IP 条目会被清理，清理后额度重置。
func TestIPLimiter_CleanupRemovesStaleEntries(t *testing.T) {
	il := NewIPLimiter(0, 1)
	// 消耗掉 10.0.0.1 的全部额度
	if !il.Allow("10.0.0.1") {
		t.Fatal("首个请求应被放行")
	}
	// 白盒改写 lastSeen 与 ttl，使条目立即过期（避免真实等待 5 分钟）
	il.mu.Lock()
	il.ttl = time.Nanosecond
	il.limiters["10.0.0.1"].lastSeen = time.Now().Add(-time.Hour)
	staleIP := "10.0.0.9"
	il.limiters[staleIP] = &rateLimiterEntry{
		limiter:  nil, // 仅为验证清理逻辑，不会被 Allow 调用
		lastSeen: time.Now().Add(-time.Hour),
	}
	il.mu.Unlock()

	il.cleanup()

	il.mu.Lock()
	_, staleExists := il.limiters[staleIP]
	_, targetExists := il.limiters["10.0.0.1"]
	il.mu.Unlock()
	if staleExists || targetExists {
		t.Fatalf("过期条目应被清理，got staleExists=%v targetExists=%v", staleExists, targetExists)
	}
	// 清理后条目重建，额度重置（fresh limiter burst=1）
	if !il.Allow("10.0.0.1") {
		t.Fatal("条目被清理后额度应重置并重新放行")
	}
}

// TestIPLimiter_CleanupKeepsFreshEntries：未过期的条目不被清理，额度保持已消耗状态。
func TestIPLimiter_CleanupKeepsFreshEntries(t *testing.T) {
	il := NewIPLimiter(0, 1)
	if !il.Allow("10.0.0.1") {
		t.Fatal("首个请求应被放行")
	}
	il.cleanup() // lastSeen 刚更新，条目未过期
	il.mu.Lock()
	_, exists := il.limiters["10.0.0.1"]
	il.mu.Unlock()
	if !exists {
		t.Fatal("未过期条目不应被清理")
	}
	// 条目未重建，额度仍是已耗尽状态
	if il.Allow("10.0.0.1") {
		t.Fatal("条目未被清理时额度不应重置")
	}
}

// TestIPLimiter_ConcurrentAccess：并发调用 Allow 不发生数据竞争且结果合法。
func TestIPLimiter_ConcurrentAccess(t *testing.T) {
	il := NewIPLimiter(1000, 1000) // 高额度：并发下大部分请求应放行
	const goroutines = 50
	const perG = 20
	var wg sync.WaitGroup
	allowedCount := 0
	var mu sync.Mutex
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := 0
			for i := 0; i < perG; i++ {
				if il.Allow("10.0.0.1") {
					local++
				}
			}
			mu.Lock()
			allowedCount += local
			mu.Unlock()
		}()
	}
	wg.Wait()
	// burst=1000 + 高 rps：1000 个请求应全部放行（保守断言 > 0 以避免时序敏感）
	if allowedCount == 0 {
		t.Fatal("高额度下并发请求不应全部被拒绝")
	}
	if allowedCount > goroutines*perG {
		t.Fatalf("放行数 %d 超过总请求数 %d", allowedCount, goroutines*perG)
	}
}

// TestUserLimiter_AllowWithinBurstThenExceeded：每个用户独立令牌桶，先放行 burst 个再拒绝。
func TestUserLimiter_AllowWithinBurstThenExceeded(t *testing.T) {
	ul := NewUserLimiter(0, 2)
	for i := 1; i <= 2; i++ {
		if !ul.Allow("user-1") {
			t.Fatalf("user-1 第 %d 个请求应被放行", i)
		}
	}
	if ul.Allow("user-1") {
		t.Fatal("user-1 超出 burst 后应被拒绝")
	}
	if !ul.Allow("user-2") {
		t.Fatal("不同用户 user-2 应有独立额度")
	}
}

// TestUserLimiter_CleanupRemovesStaleEntries：过期的用户条目会被清理。
func TestUserLimiter_CleanupRemovesStaleEntries(t *testing.T) {
	ul := NewUserLimiter(0, 1)
	if !ul.Allow("user-1") {
		t.Fatal("首个请求应被放行")
	}
	// 白盒：把条目标记为远早于 ttl，使清理立即生效
	ul.mu.Lock()
	ul.ttl = time.Nanosecond
	ul.limiters["user-1"].lastSeen = time.Now().Add(-time.Hour)
	ul.mu.Unlock()

	ul.cleanup()

	ul.mu.Lock()
	_, exists := ul.limiters["user-1"]
	ul.mu.Unlock()
	if exists {
		t.Fatal("过期用户条目应被清理")
	}
	// 清理后额度重置
	if !ul.Allow("user-1") {
		t.Fatal("条目被清理后额度应重置并重新放行")
	}
}

// TestUserLimiter_ConcurrentAccess：并发调用 Allow 不发生数据竞争。
func TestUserLimiter_ConcurrentAccess(t *testing.T) {
	ul := NewUserLimiter(1000, 1000)
	var wg sync.WaitGroup
	var allowed int
	var mu sync.Mutex
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := 0
			for j := 0; j < 20; j++ {
				if ul.Allow("user-1") {
					n++
				}
			}
			mu.Lock()
			allowed += n
			mu.Unlock()
		}()
	}
	wg.Wait()
	if allowed == 0 {
		t.Fatal("高额度下并发请求不应全部被拒绝")
	}
}

// TestMiddleware_IPRateLimitExceeded：IP 超限时返回 429 与响应头，且不调用下游 handler。
func TestMiddleware_IPRateLimitExceeded(t *testing.T) {
	t.Setenv("GLOBAL_RPS", "1000")
	t.Setenv("IP_RPS", "1") // burst = 1*2 = 2
	t.Setenv("USER_RPS", "1000")

	called := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++ })
	mw := Middleware()(handler)

	// 同一 IP 连打 3 次：前 2 次放行（burst=2），第 3 次被 IP 限流拒绝
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "1.2.3.4:5678"
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, req)
		if i < 2 && rec.Code != http.StatusOK {
			t.Fatalf("第 %d 次请求应放行，got %d", i+1, rec.Code)
		}
		if i == 2 {
			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("第 3 次请求应返回 429，got %d", rec.Code)
			}
			if got := rec.Header().Get("X-RateLimit-Limit"); got != "1" {
				t.Fatalf("X-RateLimit-Limit = %q, want 1", got)
			}
			if got := rec.Header().Get("Retry-After"); got != "1" {
				t.Fatalf("Retry-After = %q, want 1", got)
			}
		}
	}
	if called != 2 {
		t.Fatalf("下游 handler 应只被调用 2 次，got %d", called)
	}
}

// TestMiddleware_UserRateLimitExceeded：用户超限（X-User-ID 头）返回 429 且不调用下游。
func TestMiddleware_UserRateLimitExceeded(t *testing.T) {
	t.Setenv("GLOBAL_RPS", "1000")
	t.Setenv("IP_RPS", "1000")
	t.Setenv("USER_RPS", "1") // burst = 2

	called := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++ })
	mw := Middleware()(handler)

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "5.6.7.8:9999"
		req.Header.Set("X-User-ID", "u-limit")
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, req)
		if i < 2 && rec.Code != http.StatusOK {
			t.Fatalf("第 %d 次请求应放行，got %d", i+1, rec.Code)
		}
		if i == 2 && rec.Code != http.StatusTooManyRequests {
			t.Fatalf("第 3 次请求应返回 429，got %d", rec.Code)
		}
	}
	if called != 2 {
		t.Fatalf("下游 handler 应只被调用 2 次，got %d", called)
	}
}

// TestMiddleware_PassesThrough：额度充足时请求透传到下游，且可见注入的上下文。
func TestMiddleware_PassesThrough(t *testing.T) {
	t.Setenv("GLOBAL_RPS", "1000")
	t.Setenv("IP_RPS", "1000")
	t.Setenv("USER_RPS", "1000")

	var gotUser string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = r.Header.Get("X-User-ID")
		w.WriteHeader(http.StatusOK)
	})
	mw := Middleware()(handler)

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "9.9.9.9:1234"
	req.Header.Set("X-User-ID", "alice")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("额度充足时应放行，got %d", rec.Code)
	}
	if gotUser != "alice" {
		t.Fatalf("请求应原样透传，X-User-ID = %q, want alice", gotUser)
	}
}

// TestMiddleware_InvalidEnvFallsBackToDefault：非法环境变量回退到默认值（不 panic）。
func TestMiddleware_InvalidEnvFallsBackToDefault(t *testing.T) {
	t.Setenv("GLOBAL_RPS", "not-a-number")
	t.Setenv("IP_RPS", "not-a-number")
	t.Setenv("USER_RPS", "not-a-number")

	// 仅验证配置解析回退默认值不 panic，且中间件可处理请求
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	mw := Middleware()(handler)
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "8.8.4.4:53"
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !called {
		t.Fatalf("默认配置应放行请求，code=%d called=%v", rec.Code, called)
	}
}

// TestGRPCInterceptor_UserRateLimitExceeded：用户超限时返回 ResourceExhausted，不调用 handler。
func TestGRPCInterceptor_UserRateLimitExceeded(t *testing.T) {
	t.Setenv("GLOBAL_RPS", "1000")
	t.Setenv("IP_RPS", "1000")
	t.Setenv("USER_RPS", "1") // burst = 2

	handlerCalled := 0
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		handlerCalled++
		return "ok", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/svc/Method"}
	ic := GRPCInterceptor()

	md := metadata.Pairs("x-user-id", "u-grpc")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	for i := 0; i < 3; i++ {
		resp, err := ic(ctx, nil, info, handler)
		if i < 2 {
			if err != nil {
				t.Fatalf("第 %d 次调用应放行，got err %v", i+1, err)
			}
			if resp != "ok" {
				t.Fatalf("resp = %v, want ok", resp)
			}
		} else {
			st, ok := status.FromError(err)
			if !ok || st.Code() != codes.ResourceExhausted {
				t.Fatalf("第 3 次调用应返回 ResourceExhausted，got %v", err)
			}
		}
	}
	if handlerCalled != 2 {
		t.Fatalf("下游 handler 应被调用 2 次，got %d", handlerCalled)
	}
}

// TestGRPCInterceptor_GlobalRateLimitExceeded：全局超限时返回 ResourceExhausted。
func TestGRPCInterceptor_GlobalRateLimitExceeded(t *testing.T) {
	t.Setenv("GLOBAL_RPS", "1") // burst = 2
	t.Setenv("IP_RPS", "1000")
	t.Setenv("USER_RPS", "1000")

	handler := func(ctx context.Context, req interface{}) (interface{}, error) { return "ok", nil }
	info := &grpc.UnaryServerInfo{FullMethod: "/svc/Method"}
	ic := GRPCInterceptor()

	for i := 0; i < 3; i++ {
		_, err := ic(context.Background(), nil, info, handler)
		if i < 2 && err != nil {
			t.Fatalf("第 %d 次调用应放行，got %v", i+1, err)
		}
		if i == 2 {
			st, ok := status.FromError(err)
			if !ok || st.Code() != codes.ResourceExhausted {
				t.Fatalf("全局超限应返回 ResourceExhausted，got %v", err)
			}
		}
	}
}

// TestGRPCInterceptor_PeerIPRateLimit：带 peer 信息的请求按 IP 限流。
func TestGRPCInterceptor_PeerIPRateLimit(t *testing.T) {
	t.Setenv("GLOBAL_RPS", "1000")
	t.Setenv("IP_RPS", "1") // burst = 2
	t.Setenv("USER_RPS", "1000")

	handler := func(ctx context.Context, req interface{}) (interface{}, error) { return "ok", nil }
	info := &grpc.UnaryServerInfo{FullMethod: "/svc/Method"}
	ic := GRPCInterceptor()

	p := &peer.Peer{Addr: &netAddr{"1.2.3.4:7777"}}
	ctx := peer.NewContext(context.Background(), p)

	for i := 0; i < 3; i++ {
		_, err := ic(ctx, nil, info, handler)
		if i < 2 && err != nil {
			t.Fatalf("第 %d 次调用应放行，got %v", i+1, err)
		}
		if i == 2 {
			st, ok := status.FromError(err)
			if !ok || st.Code() != codes.ResourceExhausted {
				t.Fatalf("同 IP 超限应返回 ResourceExhausted，got %v", err)
			}
		}
	}
}

// TestGRPCInterceptor_ExtractsUserID：metadata 中的 x-user-id 被用于限流键。
func TestGRPCInterceptor_ExtractsUserID(t *testing.T) {
	// userFromMetadata：有 metadata 时取 x-user-id
	md := metadata.Pairs("x-user-id", "u-1")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	if got := userFromMetadata(ctx); got != "u-1" {
		t.Fatalf("userFromMetadata = %q, want u-1", got)
	}
	// 有 metadata 但无 x-user-id：回退 anonymous
	ctx = metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-other", "v"))
	if got := userFromMetadata(ctx); got != "anonymous" {
		t.Fatalf("无 x-user-id 时应回退 anonymous，got %q", got)
	}
	// 空 metadata 值回退 anonymous
	ctx = metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-user-id", ""))
	if got := userFromMetadata(ctx); got != "anonymous" {
		t.Fatalf("空 x-user-id 应回退 anonymous，got %q", got)
	}
	// 无 metadata 的 context 回退 anonymous
	if got := userFromMetadata(context.Background()); got != "anonymous" {
		t.Fatalf("无 metadata 应回退 anonymous，got %q", got)
	}
}

// TestGetEnvInt：环境变量解析与默认值回退。
func TestGetEnvInt(t *testing.T) {
	t.Setenv("RL_TEST_INT", "42")
	if got := getEnvInt("RL_TEST_INT", 7); got != 42 {
		t.Fatalf("getEnvInt = %d, want 42", got)
	}
	if got := getEnvInt("RL_TEST_UNSET", 7); got != 7 {
		t.Fatalf("未设置时应返回默认值，got %d", got)
	}
	t.Setenv("RL_TEST_BAD", "abc")
	if got := getEnvInt("RL_TEST_BAD", 7); got != 7 {
		t.Fatalf("非法值时应返回默认值，got %d", got)
	}
	t.Setenv("RL_TEST_EMPTY", "")
	if got := getEnvInt("RL_TEST_EMPTY", 7); got != 7 {
		t.Fatalf("空值时应返回默认值，got %d", got)
	}
}

// netAddr 是满足 net.Addr 的最小实现（用于 gRPC peer 测试）。
type netAddr struct{ s string }

func (a *netAddr) Network() string { return "tcp" }
func (a *netAddr) String() string  { return a.s }
