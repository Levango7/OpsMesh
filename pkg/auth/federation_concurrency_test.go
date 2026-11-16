// federation_concurrency_test.go — 联邦验签的并发与异常输入硬化测试（批次30-32 验收）。
//
// 覆盖：
//   - NonceCache 同一 nonce 并发竞争恰好一个 true；
//   - 大量不同 nonce 并发全部登记成功且互不干扰；
//   - 并发过期/机会式清理不会 panic、不会丢失未过期 nonce；
//   - VerifyFederationSignature 的异常头（缺失/畸形时间戳/空 nonce/超长 nonce/
//     篡改 body/错误密钥/签名路径不符）全部可判定错误，绝不 panic；
//   - 相同签名请求并发重放：恰好一个成功，其余 ErrFederationReplay。
package auth

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestNonceCacheConcurrency_SameNonceExactlyOne 验证 N 个 goroutine 同时登记同一 nonce，
// 恰好一个返回 true（其余按重放拒绝）。
func TestNonceCacheConcurrency_SameNonceExactlyOne(t *testing.T) {
	cache := NewNonceCache(time.Minute)
	now := time.Unix(1700000000, 0)
	const workers = 64

	var accepted int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if cache.CheckAndAdd("same-nonce", now) {
				atomic.AddInt64(&accepted, 1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := atomic.LoadInt64(&accepted); got != 1 {
		t.Fatalf("same nonce accepted %d times, want exactly 1", got)
	}
	if cache.CheckAndAdd("same-nonce", now) {
		t.Fatal("nonce accepted again after concurrent registration")
	}
}

// TestNonceCacheConcurrency_DistinctNonces 验证大量不同 nonce 并发登记全部成功，
// 且后续重放全部被拒（登记结果不会相互覆盖）。
func TestNonceCacheConcurrency_DistinctNonces(t *testing.T) {
	cache := NewNonceCache(time.Minute)
	now := time.Unix(1700000000, 0)
	const workers = 512

	var accepted int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			if cache.CheckAndAdd("distinct-"+strconv.Itoa(i), now) {
				atomic.AddInt64(&accepted, 1)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if got := atomic.LoadInt64(&accepted); got != workers {
		t.Fatalf("distinct nonces accepted = %d, want %d", got, workers)
	}
	for i := 0; i < workers; i += 37 {
		if cache.CheckAndAdd("distinct-"+strconv.Itoa(i), now) {
			t.Fatalf("distinct nonce %d accepted as replay", i)
		}
	}
}

// TestNonceCacheConcurrency_ExpiryPruneKeepsUnrelated 验证并发写入过期 nonce（触发机会式
// 清理）时不会 panic，也不会清掉仍在有效期内的无关 nonce。
func TestNonceCacheConcurrency_ExpiryPruneKeepsUnrelated(t *testing.T) {
	cache := &NonceCache{ttl: time.Minute, maxEntries: 8192, entries: make(map[string]time.Time)}
	t0 := time.Unix(1700000000, 0)
	// anchor 在 t0+2h 登记，过期时刻 t0+3h；后续并发清理的基准时间是 t0 或 t0+2h，
	// 都早于其过期时刻，必须原样保留。
	if !cache.CheckAndAdd("anchor", t0.Add(2*time.Hour)) {
		t.Fatal("anchor nonce rejected")
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	for w := 0; w < 8; w++ {
		// 过期项：登记时刻 t0，清理基准 t0+2h 时全部过期。
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			<-start
			for i := 0; i < 400; i++ {
				cache.CheckAndAdd("expired-"+strconv.Itoa(w)+"-"+strconv.Itoa(i), t0)
			}
		}(w)
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			<-start
			for i := 0; i < 400; i++ {
				cache.CheckAndAdd("live-"+strconv.Itoa(w)+"-"+strconv.Itoa(i), t0.Add(2*time.Hour))
			}
		}(w)
	}
	close(start)
	wg.Wait()

	if !cache.CheckAndAdd("anchor", t0.Add(2*time.Hour+30*time.Minute)) {
		t.Fatal("unrelated non-expired nonce was lost during concurrent prune")
	}
	if !cache.CheckAndAdd("live-0-0", t0.Add(2*time.Hour+30*time.Minute)) {
		t.Fatal("live nonce was lost during concurrent prune")
	}
}

// TestVerifyFederationSignature_AbnormalInputs 验证异常入站头全部返回可判定错误，
// 不 panic、不进入未知状态。
func TestVerifyFederationSignature_AbnormalInputs(t *testing.T) {
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	ts := strconv.FormatInt(now.Unix(), 10)
	const path = "/api/v1/federation/forward/device-op"
	body := []byte(`{"op":"metrics"}`)
	base := federationTestHeaders(http.MethodPost, path, ts, "abnormal-nonce-1", "t1", "u1", "role-admin", fedTestSecret, body)

	tests := []struct {
		name   string
		header http.Header
		body   []byte
		path   string
		cache  *NonceCache
	}{
		{
			name: "missing_forwarded",
			header: func() http.Header {
				h := base.Clone()
				h.Del(FederationHeaderForwarded)
				return h
			}(),
			body: body, path: path,
		},
		{
			name: "missing_timestamp",
			header: func() http.Header {
				h := base.Clone()
				h.Del(FederationHeaderTS)
				return h
			}(),
			body: body, path: path,
		},
		{
			name: "missing_signature",
			header: func() http.Header {
				h := base.Clone()
				h.Del(FederationHeaderSignature)
				return h
			}(),
			body: body, path: path,
		},
		{
			name: "missing_nonce_with_cache",
			header: func() http.Header {
				h := federationTestHeaders(http.MethodPost, path, ts, "", "t1", "u1", "role-admin", fedTestSecret, body)
				return h
			}(),
			body: body, path: path, cache: NewNonceCache(time.Minute),
		},
		{
			name:   "non_numeric_timestamp",
			header: federationTestHeaders(http.MethodPost, path, "not-a-timestamp", "abnormal-nonce-2", "t1", "u1", "role-admin", fedTestSecret, body),
			body:   body, path: path,
		},
		{
			name:   "rfc3339_garbage",
			header: federationTestHeaders(http.MethodPost, path, "2026-13-45T99:99:99Z", "abnormal-nonce-3", "t1", "u1", "role-admin", fedTestSecret, body),
			body:   body, path: path,
		},
		{
			name:   "oversized_nonce",
			header: federationTestHeaders(http.MethodPost, path, ts, strings.Repeat("a", 4096), "t1", "u1", "role-admin", fedTestSecret, body),
			body:   body, path: path,
		},
		{
			name:   "nonce_with_space",
			header: federationTestHeaders(http.MethodPost, path, ts, "bad nonce value", "t1", "u1", "role-admin", fedTestSecret, body),
			body:   body, path: path,
		},
		{
			name:   "nonce_with_control_char",
			header: federationTestHeaders(http.MethodPost, path, ts, "bad\x01nonce", "t1", "u1", "role-admin", fedTestSecret, body),
			body:   body, path: path,
		},
		{
			name:   "wrong_secret",
			header: federationTestHeaders(http.MethodPost, path, ts, "abnormal-nonce-4", "t1", "u1", "role-admin", "wrong-secret", body),
			body:   body, path: path,
		},
		{
			name:   "signature_over_different_path",
			header: federationTestHeaders(http.MethodPost, "/api/v1/federation/forward/task", ts, "abnormal-nonce-5", "t1", "u1", "role-admin", fedTestSecret, body),
			body:   body, path: path,
		},
		{
			name:   "truncated_body",
			header: federationTestHeaders(http.MethodPost, path, ts, "abnormal-nonce-6", "t1", "u1", "role-admin", fedTestSecret, body),
			body:   body[:len(body)-4], path: path,
		},
		{
			name:   "tampered_body",
			header: federationTestHeaders(http.MethodPost, path, ts, "abnormal-nonce-7", "t1", "u1", "role-admin", fedTestSecret, body),
			body:   []byte(`{"op":"retire"}`), path: path,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("VerifyFederationSignature panicked: %v", r)
					}
				}()
				err = VerifyFederationSignature([]byte(fedTestSecret), DefaultFederationMaxSkew, http.MethodPost, tt.path, tt.header, tt.body, tt.cache, now)
			}()
			if err == nil {
				t.Fatal("abnormal federation input accepted, want error")
			}
			if tt.name == "oversized_nonce" || tt.name == "nonce_with_space" || tt.name == "nonce_with_control_char" {
				if !errors.Is(err, ErrFederationMalformed) {
					t.Fatalf("got err=%v, want ErrFederationMalformed", err)
				}
			}
		})
	}
}

// TestVerifyFederationSignature_ParallelIdenticalReplay 验证同一份签名请求并发提交时
// 恰好一个通过验签，其余全部判定为重放。
func TestVerifyFederationSignature_ParallelIdenticalReplay(t *testing.T) {
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	ts := strconv.FormatInt(now.Unix(), 10)
	const path = "/api/v1/federation/forward/device-op"
	body := []byte(`{"op":"metrics","payload":{"deviceID":"d1"}}`)
	header := federationTestHeaders(http.MethodPost, path, ts, "parallel-replay-nonce-1", "t1", "u1", "role-admin", fedTestSecret, body)
	cache := NewNonceCache(time.Minute)

	const workers = 32
	var ok, replay, other int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := VerifyFederationSignature([]byte(fedTestSecret), DefaultFederationMaxSkew, http.MethodPost, path, header.Clone(), body, cache, now)
			switch {
			case err == nil:
				atomic.AddInt64(&ok, 1)
			case errors.Is(err, ErrFederationReplay):
				atomic.AddInt64(&replay, 1)
			default:
				atomic.AddInt64(&other, 1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := atomic.LoadInt64(&ok); got != 1 {
		t.Fatalf("accepted = %d, want exactly 1", got)
	}
	if got := atomic.LoadInt64(&replay); got != workers-1 {
		t.Fatalf("replay = %d, want %d", got, workers-1)
	}
	if got := atomic.LoadInt64(&other); got != 0 {
		t.Fatalf("unexpected error class = %d, want 0", got)
	}
}
