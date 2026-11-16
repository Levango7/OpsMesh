package controlplane

// security_headers_test.go — 安全响应头中间件验收。
// 覆盖：正常 JSON 响应、401/403/404、CSP 关闭、HSTS 的 TLS/明文/显式三种形态、
// 总开关与环境变量装配。

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// assertBaselineSecurityHeaders 校验与传输层无关的固定头。
func assertBaselineSecurityHeaders(t *testing.T, h http.Header) {
	t.Helper()
	want := map[string]string{
		"X-Content-Type-Options":     "nosniff",
		"X-Frame-Options":            "DENY",
		"Referrer-Policy":            "no-referrer",
		"Permissions-Policy":         "geolocation=(), microphone=(), camera=()",
		"Cross-Origin-Opener-Policy": "same-origin",
	}
	for name, value := range want {
		if got := h.Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
	csp := h.Get("Content-Security-Policy")
	for _, directive := range []string{
		"default-src 'self'",
		"base-uri 'self'",
		"frame-ancestors 'none'",
		"form-action 'self'",
		"object-src 'none'",
		"img-src 'self' data:",
		"style-src 'self' 'unsafe-inline'",
		"script-src 'self'",
		"connect-src 'self'",
	} {
		if !strings.Contains(csp, directive) {
			t.Errorf("CSP 缺少指令 %q：%q", directive, csp)
		}
	}
}

func TestSecurityHeaders_PresentOnNormalResponse(t *testing.T) {
	s := &Server{}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	rec := httptest.NewRecorder()
	s.securityHeadersMiddleware(inner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	assertBaselineSecurityHeaders(t, rec.Header())
}

func TestSecurityHeaders_PresentOnErrorResponses(t *testing.T) {
	cases := []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound}
	for _, status := range cases {
		t.Run(http.StatusText(status), func(t *testing.T) {
			s := &Server{}
			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
			})
			rec := httptest.NewRecorder()
			s.securityHeadersMiddleware(inner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/forbidden", nil))

			if rec.Code != status {
				t.Fatalf("status = %d, want %d", rec.Code, status)
			}
			assertBaselineSecurityHeaders(t, rec.Header())
		})
	}
}

func TestSecurityHeaders_CSPDisabledWhenPolicyEmpty(t *testing.T) {
	s := &Server{securityHeaders: &securityHeadersConfig{CSPPolicy: ""}}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	rec := httptest.NewRecorder()
	s.securityHeadersMiddleware(inner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if csp := rec.Header().Get("Content-Security-Policy"); csp != "" {
		t.Fatalf("空策略应关闭 CSP，实得 %q", csp)
	}
	// 其余安全头不受 CSP 开关影响。
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("CSP 关闭不应影响 X-Frame-Options")
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("CSP 关闭不应影响 X-Content-Type-Options")
	}
}

func TestSecurityHeaders_NoHSTSOnPlainHTTP(t *testing.T) {
	s := &Server{}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	s.securityHeadersMiddleware(inner).ServeHTTP(rec, req)

	if hsts := rec.Header().Get("Strict-Transport-Security"); hsts != "" {
		t.Fatalf("明文 HTTP 不应注入 HSTS，实得 %q", hsts)
	}
}

func TestSecurityHeaders_HSTSOnTLSRequest(t *testing.T) {
	s := &Server{}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	req.TLS = &tls.ConnectionState{}
	s.securityHeadersMiddleware(inner).ServeHTTP(rec, req)

	if got := rec.Header().Get("Strict-Transport-Security"); got != securityHSTSValue {
		t.Fatalf("TLS 请求 HSTS = %q, want %q", got, securityHSTSValue)
	}
}

func TestSecurityHeaders_HSTSExplicitlyEnabled(t *testing.T) {
	s := &Server{securityHeaders: &securityHeadersConfig{
		CSPPolicy:   defaultSecurityCSPPolicy,
		HSTSEnabled: true,
	}}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	rec := httptest.NewRecorder()
	s.securityHeadersMiddleware(inner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://example.com/", nil))

	if got := rec.Header().Get("Strict-Transport-Security"); got != securityHSTSValue {
		t.Fatalf("显式开启时明文请求也应注入 HSTS，实得 %q", got)
	}
}

func TestSecurityHeaders_Disabled(t *testing.T) {
	s := &Server{securityHeaders: &securityHeadersConfig{Disabled: true}}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	rec := httptest.NewRecorder()
	s.securityHeadersMiddleware(inner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	for _, name := range []string{
		"X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy",
		"Permissions-Policy", "Cross-Origin-Opener-Policy", "Content-Security-Policy",
	} {
		if got := rec.Header().Get(name); got != "" {
			t.Errorf("总开关关闭时 %s 应为空，实得 %q", name, got)
		}
	}
}

func TestSecurityHeadersFromEnv(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		// t.Setenv 先登记恢复，再 Unsetenv 模拟"未设置"（区别于显式置空）。
		t.Setenv("OPSMESH_SECURITY_HEADERS", "")
		t.Setenv("OPSMESH_CSP_POLICY", "")
		t.Setenv("OPSMESH_HSTS", "")
		for _, name := range []string{"OPSMESH_SECURITY_HEADERS", "OPSMESH_CSP_POLICY", "OPSMESH_HSTS"} {
			if err := os.Unsetenv(name); err != nil {
				t.Fatalf("unset %s: %v", name, err)
			}
		}

		cfg := securityHeadersFromEnv()
		if cfg.Disabled {
			t.Fatal("缺省应启用安全头")
		}
		if cfg.CSPPolicy != defaultSecurityCSPPolicy {
			t.Fatalf("缺省 CSP = %q, want %q", cfg.CSPPolicy, defaultSecurityCSPPolicy)
		}
		if cfg.HSTSEnabled {
			t.Fatal("缺省不应强制 HSTS")
		}
	})

	t.Run("overrides", func(t *testing.T) {
		t.Setenv("OPSMESH_SECURITY_HEADERS", "false")
		t.Setenv("OPSMESH_CSP_POLICY", "default-src 'none'")
		t.Setenv("OPSMESH_HSTS", "true")

		cfg := securityHeadersFromEnv()
		if !cfg.Disabled {
			t.Fatal("OPSMESH_SECURITY_HEADERS=false 应关闭总开关")
		}
		if cfg.CSPPolicy != "default-src 'none'" {
			t.Fatalf("CSPPolicy = %q", cfg.CSPPolicy)
		}
		if !cfg.HSTSEnabled {
			t.Fatal("OPSMESH_HSTS=true 应强制 HSTS")
		}
	})

	t.Run("empty CSP disables CSP", func(t *testing.T) {
		t.Setenv("OPSMESH_SECURITY_HEADERS", "")
		t.Setenv("OPSMESH_CSP_POLICY", "")
		t.Setenv("OPSMESH_HSTS", "")

		cfg := securityHeadersFromEnv()
		if cfg.CSPPolicy != "" {
			t.Fatalf("显式空 CSP 应保持为空，实得 %q", cfg.CSPPolicy)
		}
	})
}
