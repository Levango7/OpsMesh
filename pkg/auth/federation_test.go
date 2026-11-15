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

const fedTestSecret = "fed-test-secret-32-bytes-minimum!!"

func federationTestHeaders(method, path, ts, nonce, tenant, user, roles, secret string, body []byte) http.Header {
	h := make(http.Header)
	h.Set(FederationHeaderForwarded, "1")
	h.Set(FederationHeaderTS, ts)
	h.Set("X-Tenant-ID", tenant)
	h.Set("X-User-Id", user)
	h.Set("X-User-Roles", roles)
	var canonical string
	if nonce != "" {
		canonical = FederationCanonicalString(method, path, ts, nonce, tenant, user, roles, FederationBodySHA256(body))
		h.Set(FederationHeaderNonce, nonce)
	} else {
		canonical = FederationCanonicalStringLegacy(method, path, ts, tenant, user, roles, FederationBodySHA256(body))
	}
	h.Set(FederationHeaderSignature, SignFederation(secret, canonical))
	return h
}

func TestFederationCanonicalString_ExactBytes(t *testing.T) {
	sha := strings.Repeat("ab", 32)
	tests := []struct {
		name string
		got  string
		want string
	}{
		{
			name: "new_full",
			got:  FederationCanonicalString("GET", "/api/v1/devices", "1731670800", "n-001", "tenant-a", "user-a", "role-admin,role-viewer", sha),
			want: "GET|/api/v1/devices|1731670800|n-001|tenant-a|user-a|role-admin,role-viewer|" + sha,
		},
		{
			name: "new_empty_identity",
			got:  FederationCanonicalString("POST", "/api/v1/tasks", "1731670800", "n-002", "", "", "", sha),
			want: "POST|/api/v1/tasks|1731670800|n-002||||" + sha,
		},
		{
			name: "legacy_full",
			got:  FederationCanonicalStringLegacy("POST", "/api/v1/tasks", "1731670800", "tenant-a", "user-a", "role-admin", sha),
			want: "POST|/api/v1/tasks|1731670800|tenant-a|user-a|role-admin|" + sha,
		},
		{
			name: "legacy_empty_identity",
			got:  FederationCanonicalStringLegacy("GET", "/healthz", "1731670800", "", "", "", sha),
			want: "GET|/healthz|1731670800||||" + sha,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("canonical string\n got: %q\nwant: %q", tt.got, tt.want)
			}
		})
	}
}

func TestFederationBodySHA256_KnownDigest(t *testing.T) {
	if got, want := FederationBodySHA256(nil), "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"; got != want {
		t.Fatalf("FederationBodySHA256(nil) = %q, want %q", got, want)
	}
	if got, want := FederationBodySHA256([]byte("hello")), "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"; got != want {
		t.Fatalf("FederationBodySHA256(hello) = %q, want %q", got, want)
	}
}

func TestFederationSignAndVerify_RoundTrip(t *testing.T) {
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	ts := strconv.FormatInt(now.Unix(), 10)
	tests := []struct {
		name  string
		body  []byte
		nonce string
		cache *NonceCache
	}{
		{name: "GET_empty_body_no_cache", nonce: "n-get-1"},
		{name: "POST_with_body_no_cache", body: []byte(`{"agentID":"a1","command":"echo hi"}`), nonce: "n-post-1"},
		{name: "GET_with_cache", nonce: "n-get-2", cache: NewNonceCache(time.Minute)},
		{name: "POST_with_cache", body: []byte(`{"agentID":"a2"}`), nonce: "n-post-2", cache: NewNonceCache(time.Minute)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method, path := http.MethodGet, "/api/v1/devices"
			if tt.body != nil {
				method, path = http.MethodPost, "/api/v1/tasks"
			}
			h := federationTestHeaders(method, path, ts, tt.nonce, "t1", "u1", "role-admin", fedTestSecret, tt.body)
			if err := VerifyFederationSignature([]byte(fedTestSecret), DefaultFederationMaxSkew, method, path, h, tt.body, tt.cache, now); err != nil {
				t.Fatalf("valid signature rejected: %v", err)
			}
		})
	}
}

func TestFederationVerify_RejectsTampering(t *testing.T) {
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	ts := strconv.FormatInt(now.Unix(), 10)
	body := []byte(`{"agentID":"a1","command":"echo hi"}`)
	path := "/api/v1/tasks"
	headers := federationTestHeaders(http.MethodPost, path, ts, "n-tamper", "t1", "u1", "role-admin", fedTestSecret, body)

	tests := []struct {
		name   string
		secret string
		method string
		path   string
		body   []byte
		header http.Header
	}{
		{
			name:   "body_tampered",
			secret: fedTestSecret, method: http.MethodPost, path: path,
			body:   []byte(`{"agentID":"a1","command":"rm -rf /"}`),
			header: headers.Clone(),
		},
		{
			name:   "path_tampered",
			secret: fedTestSecret, method: http.MethodPost, path: "/api/v1/tasks/other",
			body:   body,
			header: headers.Clone(),
		},
		{
			name:   "method_tampered",
			secret: fedTestSecret, method: http.MethodGet, path: path,
			body:   body,
			header: headers.Clone(),
		},
		{
			name:   "identity_header_tampered",
			secret: fedTestSecret, method: http.MethodPost, path: path,
			body: body,
			header: func() http.Header {
				h := headers.Clone()
				h.Set("X-Tenant-ID", "t2")
				return h
			}(),
		},
		{
			name:   "wrong_secret",
			secret: "another-secret", method: http.MethodPost, path: path,
			body:   body,
			header: headers.Clone(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := VerifyFederationSignature([]byte(tt.secret), DefaultFederationMaxSkew, tt.method, tt.path, tt.header, tt.body, nil, now)
			if !errors.Is(err, ErrFederationSignature) {
				t.Fatalf("got err=%v, want ErrFederationSignature", err)
			}
		})
	}
}

func TestFederationVerify_SkewWindow(t *testing.T) {
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		ts      string
		wantErr error
	}{
		{name: "past_within_window", ts: strconv.FormatInt(now.Add(-4*time.Minute).Unix(), 10)},
		{name: "future_within_window", ts: strconv.FormatInt(now.Add(4*time.Minute).Unix(), 10)},
		{name: "past_exact_boundary", ts: strconv.FormatInt(now.Add(-DefaultFederationMaxSkew).Unix(), 10)},
		{name: "future_exact_boundary", ts: strconv.FormatInt(now.Add(DefaultFederationMaxSkew).Unix(), 10)},
		{name: "past_beyond_window", ts: strconv.FormatInt(now.Add(-DefaultFederationMaxSkew-time.Second).Unix(), 10), wantErr: ErrFederationSkew},
		{name: "future_beyond_window", ts: strconv.FormatInt(now.Add(DefaultFederationMaxSkew+time.Second).Unix(), 10), wantErr: ErrFederationSkew},
		{name: "rfc3339_within_window", ts: now.Add(-time.Minute).Format(time.RFC3339)},
		{name: "rfc3339_beyond_window", ts: now.Add(-DefaultFederationMaxSkew - time.Minute).Format(time.RFC3339), wantErr: ErrFederationSkew},
		{name: "invalid_timestamp", ts: "not-a-timestamp", wantErr: ErrFederationMalformed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const path = "/api/v1/devices"
			h := federationTestHeaders(http.MethodGet, path, tt.ts, "n-skew", "t1", "", "", fedTestSecret, nil)
			err := VerifyFederationSignature([]byte(fedTestSecret), DefaultFederationMaxSkew, http.MethodGet, path, h, nil, nil, now)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("timestamp %q rejected: %v", tt.ts, err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("timestamp %q: got err=%v, want %v", tt.ts, err, tt.wantErr)
			}
		})
	}
}

func TestFederationVerify_NonceReplay(t *testing.T) {
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	ts := strconv.FormatInt(now.Unix(), 10)
	const path = "/api/v1/devices"
	cache := NewNonceCache(time.Minute)

	h1 := federationTestHeaders(http.MethodGet, path, ts, "replay-1", "t1", "u1", "role-admin", fedTestSecret, nil)
	if err := VerifyFederationSignature([]byte(fedTestSecret), DefaultFederationMaxSkew, http.MethodGet, path, h1, nil, cache, now); err != nil {
		t.Fatalf("first request rejected: %v", err)
	}
	if err := VerifyFederationSignature([]byte(fedTestSecret), DefaultFederationMaxSkew, http.MethodGet, path, h1.Clone(), nil, cache, now); !errors.Is(err, ErrFederationReplay) {
		t.Fatalf("replay: got err=%v, want ErrFederationReplay", err)
	}

	h2 := federationTestHeaders(http.MethodGet, path, ts, "replay-2", "t1", "u1", "role-admin", fedTestSecret, nil)
	if err := VerifyFederationSignature([]byte(fedTestSecret), DefaultFederationMaxSkew, http.MethodGet, path, h2, nil, cache, now); err != nil {
		t.Fatalf("distinct nonce rejected: %v", err)
	}
}

func TestFederationVerify_LegacyCompatibility(t *testing.T) {
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	ts := strconv.FormatInt(now.Unix(), 10)
	const path = "/api/v1/devices"
	legacy := federationTestHeaders(http.MethodGet, path, ts, "", "t1", "u1", "role-admin", fedTestSecret, nil)
	if got := legacy.Get(FederationHeaderNonce); got != "" {
		t.Fatalf("legacy headers unexpectedly contain nonce %q", got)
	}

	t.Run("accepted_without_nonce_cache", func(t *testing.T) {
		if err := VerifyFederationSignature([]byte(fedTestSecret), DefaultFederationMaxSkew, http.MethodGet, path, legacy.Clone(), nil, nil, now); err != nil {
			t.Fatalf("legacy request rejected when replay protection disabled: %v", err)
		}
	})

	t.Run("rejected_when_nonce_cache_enabled", func(t *testing.T) {
		cache := NewNonceCache(time.Minute)
		err := VerifyFederationSignature([]byte(fedTestSecret), DefaultFederationMaxSkew, http.MethodGet, path, legacy.Clone(), nil, cache, now)
		if !errors.Is(err, ErrFederationNonceRequired) {
			t.Fatalf("legacy replay-capable path: got err=%v, want ErrFederationNonceRequired", err)
		}
		// 重放同一旧请求同样被拒（不会因为旧格式而静默通过）。
		if err := VerifyFederationSignature([]byte(fedTestSecret), DefaultFederationMaxSkew, http.MethodGet, path, legacy.Clone(), nil, cache, now); !errors.Is(err, ErrFederationNonceRequired) {
			t.Fatalf("legacy replay: got err=%v, want ErrFederationNonceRequired", err)
		}
	})
}

func TestFederationVerify_RequiresNonceWhenCacheEnabled(t *testing.T) {
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	ts := strconv.FormatInt(now.Unix(), 10)
	const path = "/api/v1/devices"
	cache := NewNonceCache(time.Minute)

	// 显式空 nonce 头（header 存在但值为空）：即使签名按 legacy canonical 正确也必须拒绝。
	h := federationTestHeaders(http.MethodGet, path, ts, "", "t1", "u1", "role-admin", fedTestSecret, nil)
	h.Set(FederationHeaderNonce, "")
	if err := VerifyFederationSignature([]byte(fedTestSecret), DefaultFederationMaxSkew, http.MethodGet, path, h, nil, cache, now); !errors.Is(err, ErrFederationNonceRequired) {
		t.Fatalf("empty nonce with cache: got err=%v, want ErrFederationNonceRequired", err)
	}

	// 无 Forwarded 标记：一律拒绝。
	h2 := federationTestHeaders(http.MethodGet, path, ts, "n-x", "t1", "u1", "role-admin", fedTestSecret, nil)
	h2.Del(FederationHeaderForwarded)
	if err := VerifyFederationSignature([]byte(fedTestSecret), DefaultFederationMaxSkew, http.MethodGet, path, h2, nil, cache, now); !errors.Is(err, ErrFederationMalformed) {
		t.Fatalf("missing forwarded header: got err=%v, want ErrFederationMalformed", err)
	}

	// 空密钥：拒绝。
	h3 := federationTestHeaders(http.MethodGet, path, ts, "n-y", "t1", "u1", "role-admin", fedTestSecret, nil)
	if err := VerifyFederationSignature(nil, DefaultFederationMaxSkew, http.MethodGet, path, h3, nil, cache, now); !errors.Is(err, ErrFederationSecretMissing) {
		t.Fatalf("empty secret: got err=%v, want ErrFederationSecretMissing", err)
	}
}

func TestFederationNonceCache_Expiry(t *testing.T) {
	t0 := time.Unix(1700000000, 0)
	cache := NewNonceCache(time.Minute)

	if !cache.CheckAndAdd("n-exp", t0) {
		t.Fatal("first use rejected")
	}
	if cache.CheckAndAdd("n-exp", t0.Add(59*time.Second)) {
		t.Fatal("nonce accepted before TTL expiry")
	}
	if !cache.CheckAndAdd("n-exp", t0.Add(time.Minute)) {
		t.Fatal("nonce not reusable at exact TTL expiry")
	}
	if cache.CheckAndAdd("n-exp", t0.Add(time.Minute+30*time.Second)) {
		t.Fatal("reused nonce accepted before refreshed TTL expiry")
	}
	if !cache.CheckAndAdd("n-exp", t0.Add(3*time.Minute)) {
		t.Fatal("nonce not reusable after refreshed TTL expiry")
	}
}

func TestFederationNonceCache_SizeCap(t *testing.T) {
	t0 := time.Unix(1700000000, 0)
	cache := &NonceCache{ttl: time.Hour, maxEntries: 2, entries: make(map[string]time.Time)}

	if !cache.CheckAndAdd("a", t0) || !cache.CheckAndAdd("b", t0) {
		t.Fatal("entries below cap rejected")
	}
	if cache.CheckAndAdd("c", t0) {
		t.Fatal("new nonce accepted while cache full and unexpired")
	}
	if !cache.CheckAndAdd("c", t0.Add(2*time.Hour)) {
		t.Fatal("new nonce rejected after expired entries should be pruned")
	}
	if !cache.CheckAndAdd("a", t0.Add(3*time.Hour)) {
		t.Fatal("old nonce not reusable after eviction by size-pressure prune")
	}
}

func TestFederationNonceCache_NilAndEmpty(t *testing.T) {
	now := time.Unix(1700000000, 0)
	var nilCache *NonceCache
	if !nilCache.CheckAndAdd("anything", now) {
		t.Fatal("nil receiver should accept (optional use)")
	}
	cache := NewNonceCache(time.Minute)
	if cache.CheckAndAdd("", now) {
		t.Fatal("empty nonce accepted")
	}

	var zero NonceCache
	if !zero.CheckAndAdd("z", now) {
		t.Fatal("zero-value NonceCache rejected first nonce")
	}
	if zero.CheckAndAdd("z", now) {
		t.Fatal("zero-value NonceCache accepted replay")
	}
}

func TestFederationNonceCache_ConcurrentDistinct(t *testing.T) {
	cache := NewNonceCache(time.Minute)
	now := time.Unix(1700000000, 0)
	var wg sync.WaitGroup
	var accepted int64
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if cache.CheckAndAdd("n-"+strconv.Itoa(i), now) {
				atomic.AddInt64(&accepted, 1)
			}
		}(i)
	}
	wg.Wait()
	if accepted != 64 {
		t.Fatalf("accepted=%d distinct nonces, want 64", accepted)
	}
}
