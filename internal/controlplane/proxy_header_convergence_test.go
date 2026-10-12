package controlplane

// proxy_header_convergence_test.go — 代理响应头收敛守卫（演练报告 F5）。
//
// 为什么值得守卫：重复不是某一侧的 bug，而是两层各设一遍同名头的结构性后果；且
// 后果超出"卫生"——两份 CSP 按**交集**强制执行（外层 nonce 与上游
// "default-src 'self'" 互不满足 ⇒ 内联 script 在交集下两条规则都放不过），两份
// 不同取值的 Access-Control-Allow-Origin 直接被浏览器判为非法 CORS 响应。
// 只改一边（例如只让对侧不设头）不能闭环：任一上游都可能自带安全中间件。
//
// 判据（两条转发路径各一层）：
//  1. 上游自设的每个"外层权威头"在最终响应里**有且仅有一份**，且值来自外层；
//  2. 上游独有的头与 Set-Cookie 原样透传——收敛只剥重复，不牵连上游信息；
//  3. 无 TLS 时 HSTS 一份都没有（外层未设；上游补的同样只该由外层决定），
//     有 TLS 时有且仅有一份。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// upstreamSecurityHeadersHandler 模拟"自带安全中间件的上游"（auth-svc 经
// pkg/security.SecurityHeaders）：自设一套安全头与 CORS，取值与外层冲突
// （CSP 无 nonce、Referrer-Policy 更宽松、X-Frame-Options 不同值、另给一个
// 上游来源的 ACAO），外加一个上游独有头用于证明"只剥重复、不牵连其余"。
// 只设头、不写体：响应体（与 Set-Cookie 等后续头）由各用例自己决定。
func upstreamSecurityHeadersHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Security-Policy", "default-src 'self'")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	w.Header().Set("Access-Control-Allow-Origin", "https://upstream-allowed.example")
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("X-Upstream-Marker", "keep-me")
	w.Header().Set("Content-Type", "application/json")
}

// assertConvergedSecurityHeaders 裁决"外层单一来源"：列内每个头有且仅有一份，
// 且值来自外层；ACAO 为空（外层未放行该来源）；上游独有头仍在。
func assertConvergedSecurityHeaders(t *testing.T, h http.Header) {
	t.Helper()
	for _, name := range []string{
		"Content-Security-Policy", "Referrer-Policy", "X-Frame-Options",
		"X-Content-Type-Options", "Permissions-Policy",
	} {
		if got := h.Values(name); len(got) != 1 {
			t.Errorf("%s 应有且仅有一份（外层单一来源），实得 %d 份：%v", name, len(got), got)
		}
	}
	if csp := h.Get("Content-Security-Policy"); !strings.Contains(csp, "nonce-") {
		t.Errorf("CSP 应保留外层 nonce 策略，实得 %q", csp)
	}
	if rp := h.Get("Referrer-Policy"); rp != "no-referrer" {
		t.Errorf("Referrer-Policy 应为外层值 no-referrer，实得 %q", rp)
	}
	if xfo := h.Get("X-Frame-Options"); xfo != "DENY" {
		t.Errorf("X-Frame-Options 应为外层值 DENY，实得 %q", xfo)
	}
	if got := h.Values("Access-Control-Allow-Origin"); len(got) != 0 {
		t.Errorf("上游 ACAO 不得穿透（外层 CORS 是唯一来源），实得 %v", got)
	}
	if got := h.Get("X-Upstream-Marker"); got != "keep-me" {
		t.Errorf("上游独有头应原样透传，实得 %q", got)
	}
}

// TestServiceProxyConvergesUpstreamSecurityHeaders 聚合代理路径（/api/v1/<domain>-svc/*）。
func TestServiceProxyConvergesUpstreamSecurityHeaders(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamSecurityHeadersHandler(w, r)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer backend.Close()
	t.Setenv("AUTOSCALER_SVC_URL", backend.URL)

	s := newServiceProxyTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/autoscaler/rules", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	// 外层中间件先设安全头再进代理——真实链路（Start() 组装）就是这个顺序，
	// 也正是重复头的复现形态。
	s.securityHeadersMiddleware(http.HandlerFunc(s.handleServiceProxy)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	assertConvergedSecurityHeaders(t, rec.Header())
	// 无 TLS：HSTS 一份都不该有（外层未设、上游补的也应剥掉）。
	if got := rec.Header().Values("Strict-Transport-Security"); len(got) != 0 {
		t.Errorf("无 TLS 时 HSTS 应为空，实得 %v", got)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"ok":true`) {
		t.Errorf("响应体应透传，实得 %q", body)
	}
}

// TestServiceProxyConvergesUpstreamHeadersWithTLS HSTS 在场时同样只有一份
// （外层值生效），证明剥除不依赖"外层此刻没设"。
func TestServiceProxyConvergesUpstreamHeadersWithTLS(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamSecurityHeadersHandler(w, r)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer backend.Close()
	t.Setenv("AUTOSCALER_SVC_URL", backend.URL)

	s := newServiceProxyTestServer()
	s.tlsCert = "cert.pem" // 有 TLS ⇒ 外层会设 HSTS
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/autoscaler/rules", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.securityHeadersMiddleware(http.HandlerFunc(s.handleServiceProxy)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	assertConvergedSecurityHeaders(t, rec.Header())
	if got := rec.Header().Values("Strict-Transport-Security"); len(got) != 1 {
		t.Errorf("TLS 下 HSTS 应有且仅有一份，实得 %d 份：%v", len(got), got)
	}
}

// TestCutoverForwardConvergesUpstreamSecurityHeaders 切流转发路径
// （forwardAuthToSvc，批次 0 演练实测复现处）：本地 /api/v1/auth/* 请求整体
// 转给 auth-svc，上游响应头同样必须收敛；Set-Cookie 仍须透传（切过去后
// 拿得到会话，auth_cutover_test.go 那条断言与此互为对照）。
func TestCutoverForwardConvergesUpstreamSecurityHeaders(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamSecurityHeadersHandler(w, r)
		http.SetCookie(w, &http.Cookie{Name: accessTokenCookieName, Value: "svc-issued-at", Path: "/"})
	}))
	defer backend.Close()
	t.Setenv("AUTH_SVC_PROXY_ENABLED", "true")
	t.Setenv("AUTH_SVC_URL", backend.URL)

	s := newServiceProxyTestServer()
	// 本地前缀路径：与生产一致（forwardAuthToSvc 只从 /api/v1/auth/* handler 进入，
	// 内部再改写到 auth-svc 前缀交给规则）。
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	rec := httptest.NewRecorder()
	// 直调转发路径：路由裁决在 cutoverForwardByToken，与本判据无关。
	s.securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.forwardAuthToSvc(w, r) {
			t.Error("forwardAuthToSvc 应完成转发（规则活跃、地址已配置）")
		}
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	assertConvergedSecurityHeaders(t, rec.Header())
	// Set-Cookie 必须仍透传（切流后客户端拿得到会话）。
	if !strings.Contains(rec.Header().Get("Set-Cookie"), "svc-issued-at") {
		t.Errorf("上游 Set-Cookie 应透传，实得 %q", rec.Header().Get("Set-Cookie"))
	}
}
