package controlplane

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/metrics"
	"github.com/Levango7/OpsMesh/internal/store"
)

// 本文件补全 server_middleware.go 的单元测试。
// 覆盖：securityHeadersMiddleware、csrfOriginCheck、recoveryMiddleware、
// httpMetricsMiddleware、statusRecorder、normalizePath、isAllDigits。

// =============================================================================
// securityHeadersMiddleware
// =============================================================================

func TestSecurityHeadersMiddleware(t *testing.T) {
	st := store.NewMemoryStore()
	s := &Server{store: st, cfg: &config.Config{Demo: true}}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	s.securityHeadersMiddleware(inner).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing X-Content-Type-Options")
	}
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("missing X-Frame-Options")
	}
	if rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("missing Referrer-Policy")
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("missing CSP")
	}
	// 无 TLS 时不应有 HSTS
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Fatal("HSTS should not be set without TLS")
	}
}

func TestSecurityHeadersMiddleware_TLS(t *testing.T) {
	st := store.NewMemoryStore()
	s := &Server{store: st, cfg: &config.Config{Demo: true}, tlsCert: "cert.pem"}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	s.securityHeadersMiddleware(inner).ServeHTTP(rec, req)
	if rec.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("HSTS should be set with TLS")
	}
}

// =============================================================================
// csrfOriginCheck
// =============================================================================

func TestCSRFOriginCheck_DemoMode(t *testing.T) {
	st := store.NewMemoryStore()
	s := &Server{store: st, cfg: &config.Config{Demo: true, AdvertiseAddr: "http://example.com"}}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	// demo 模式 → 跳过校验
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Origin", "http://evil.com")
	rec := httptest.NewRecorder()
	s.csrfOriginCheck(inner).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("demo mode should skip CSRF: %d", rec.Code)
	}
}

func TestCSRFOriginCheck_GetMethod(t *testing.T) {
	st := store.NewMemoryStore()
	s := &Server{store: st, cfg: &config.Config{Demo: false, AdvertiseAddr: "http://example.com"}}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	// GET 方法 → 放行
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	s.csrfOriginCheck(inner).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET should pass: %d", rec.Code)
	}
}

func TestCSRFOriginCheck_NoAdvertiseAddr(t *testing.T) {
	st := store.NewMemoryStore()
	s := &Server{store: st, cfg: &config.Config{Demo: false}}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	// AdvertiseAddr 空 → 跳过校验
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Origin", "http://evil.com")
	rec := httptest.NewRecorder()
	s.csrfOriginCheck(inner).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("no advertise addr should skip: %d", rec.Code)
	}
}

func TestCSRFOriginCheck_EmptyOrigin(t *testing.T) {
	st := store.NewMemoryStore()
	s := &Server{store: st, cfg: &config.Config{Demo: false, AdvertiseAddr: "http://example.com"}}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	// Origin 空 → 放行
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	s.csrfOriginCheck(inner).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty origin should pass: %d", rec.Code)
	}
}

func TestCSRFOriginCheck_MatchingOrigin(t *testing.T) {
	st := store.NewMemoryStore()
	s := &Server{store: st, cfg: &config.Config{Demo: false, AdvertiseAddr: "http://example.com"}}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()
	s.csrfOriginCheck(inner).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("matching origin should pass: %d", rec.Code)
	}
}

func TestCSRFOriginCheck_MismatchedOrigin(t *testing.T) {
	st := store.NewMemoryStore()
	s := &Server{store: st, cfg: &config.Config{Demo: false, AdvertiseAddr: "http://example.com"}}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Origin", "http://evil.com")
	rec := httptest.NewRecorder()
	s.csrfOriginCheck(inner).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("mismatched origin should be rejected: %d", rec.Code)
	}
}

func TestCSRFOriginCheck_InvalidOrigin(t *testing.T) {
	st := store.NewMemoryStore()
	s := &Server{store: st, cfg: &config.Config{Demo: false, AdvertiseAddr: "http://example.com"}}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	// 非法 Origin URL
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Origin", "://invalid")
	rec := httptest.NewRecorder()
	s.csrfOriginCheck(inner).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("invalid origin should be rejected: %d", rec.Code)
	}
}

// =============================================================================
// recoveryMiddleware
// =============================================================================

func TestRecoveryMiddleware_NoPanic(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	recoveryMiddleware(inner).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestRecoveryMiddleware_Panic(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	recoveryMiddleware(inner).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic status = %d, want 500", rec.Code)
	}
}

// =============================================================================
// httpMetricsMiddleware / statusRecorder
// =============================================================================

func TestHTTPMetricsMiddleware(t *testing.T) {
	st := store.NewMemoryStore()
	s := &Server{store: st, cfg: &config.Config{Demo: true}, metrics: metrics.New()}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	rec := httptest.NewRecorder()
	s.httpMetricsMiddleware(inner).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestStatusRecorder_WriteHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: rec, status: http.StatusOK}
	sr.WriteHeader(http.StatusNotFound)
	if sr.status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", sr.status)
	}
}

func TestStatusRecorder_Flush(t *testing.T) {
	rec := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: rec, status: http.StatusOK}
	// httptest.ResponseRecorder 实现 http.Flusher
	sr.Flush()
	// 不 panic 即可
}

// =============================================================================
// normalizePath / isAllDigits
// =============================================================================

func TestNormalizePath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"/", "/"},
		{"/api/v1/devices", "/api/v1/devices"},
		{"/api/v1/devices/123", "/api/v1/devices/:id"},
		{"/api/v1/tasks/456/result", "/api/v1/tasks/:id/result"},
		{"/api/v1/users/u-abc-1", "/api/v1/users/u-abc-1"}, // 含字母不变
		{"/api/v1/tasks/batch", "/api/v1/tasks/batch"},
		{"/api/v1/devices/123/alerts/456", "/api/v1/devices/:id/alerts/:id"},
	}
	for _, c := range cases {
		if got := normalizePath(c.in); got != c.want {
			t.Errorf("normalizePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsAllDigits(t *testing.T) {
	if isAllDigits("") {
		t.Fatal("empty should be false")
	}
	if !isAllDigits("123") {
		t.Fatal("123 should be true")
	}
	if isAllDigits("12a") {
		t.Fatal("12a should be false")
	}
	if isAllDigits("abc") {
		t.Fatal("abc should be false")
	}
}

// 确保 strings 被使用
var _ = strings.HasPrefix

// TestNormalizePath_Tightened 验证路径归一化的收紧（P1-5）：
// 超长路径、超长段、含异常字符的段都折叠，防止任意路径各自占用一个 metrics 时序。
func TestNormalizePath_Tightened(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"超长整路径", "/" + strings.Repeat("a", maxMetricsPathLen+10), "/:overlong"},
		{"超长段", "/api/v1/devices/" + strings.Repeat("9", maxMetricsPathSegmentLen+1), "/api/v1/devices/:id"},
		{"路径恰好达长度上限不折叠", strings.Repeat("/aaaa", maxMetricsPathLen/5), strings.Repeat("/aaaa", maxMetricsPathLen/5)},
		{"含空格段", "/api/v1/devices/evil path", "/api/v1/devices/:id"},
		{"含尖括号段", "/api/v1/devices/<script>", "/api/v1/devices/:id"},
		{"含编码控制字符段", "/api/v1/devices/\x00\x01", "/api/v1/devices/:id"},
		{"非 ASCII 段", "/api/v1/devices/设备一", "/api/v1/devices/:id"},
		{"点分 IP 段保留", "/api/v1/devices/10.0.0.1", "/api/v1/devices/10.0.0.1"},
		{"连字符标识保留", "/api/v1/users/u-abc-1", "/api/v1/users/u-abc-1"},
		{"点号扩展名保留", "/enterprise/assets/index-B3xk9Q2z.js", "/enterprise/assets/index-B3xk9Q2z.js"},
		{"无数字普通路径不变", "/api/v1/tasks/batch", "/api/v1/tasks/batch"},
		{"下划线与波浪号保留", "/api/v1/x/a_b~c", "/api/v1/x/a_b~c"},
	}
	for _, c := range cases {
		if got := normalizePath(c.in); got != c.want {
			t.Errorf("%s: normalizePath(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// TestIsMetricsPathSegmentSafe 验证路径段字符白名单。
func TestIsMetricsPathSegmentSafe(t *testing.T) {
	for _, ok := range []string{"devices", "v1", "u-abc-1", "10.0.0.1", "a_b~c", "index-B3xk9Q2z.js"} {
		if !isMetricsPathSegmentSafe(ok) {
			t.Errorf("isMetricsPathSegmentSafe(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"evil path", "<script>", "a|b", "a/b", "中文", "a\x00b", "a:b", "a%b"} {
		if isMetricsPathSegmentSafe(bad) {
			t.Errorf("isMetricsPathSegmentSafe(%q) = true, want false", bad)
		}
	}
}

// TestHTTPMetricsMiddleware_RecordsNormalizedPath 验证中间件记录的是归一化后的路径，
// 且扫描器路径不会造成时序膨胀（P1-5 端到端：中间件 + metrics 基数熔断）。
func TestHTTPMetricsMiddleware_RecordsNormalizedPath(t *testing.T) {
	s := &Server{cfg: &config.Config{}, metrics: metrics.New()}
	h := s.httpMetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/devices/"+strings.Repeat("7", 80), nil))
	out := s.metrics.Render()
	if !strings.Contains(out, `path="/api/v1/devices/:id"`) {
		t.Fatalf("超长数字段应归一化为 :id\n---%s", out)
	}
	if strings.Contains(out, strings.Repeat("7", 80)) {
		t.Fatalf("原始超长段泄漏进 metrics 标签\n---%s", out)
	}
}
