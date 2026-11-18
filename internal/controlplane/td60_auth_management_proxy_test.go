// td60_auth_management_proxy_test.go 钉住 TD-60 A-1 裁决：
// auth 管理面（users/roles/permissions）在 v1 保持 controlplane 本地，
// /api/v1/auth-svc/* 前缀只改写会话/认证端点。
package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Levango7/OpsMesh/internal/controlplane/svcproxy"
)

// TestAuthManagementProxyPathsStayLocal 守护「管理路径不转发且返回明确 JSON 错误」。
// 此前的 blanket 改写把这些路径变成 /api/v1/auth/users 等，由 auth-svc 回裸 404。
func TestAuthManagementProxyPathsStayLocal(t *testing.T) {
	paths := []string{
		"/api/v1/auth-svc/users",
		"/api/v1/auth-svc/users/user-1",
		"/api/v1/auth-svc/users/user-1/approve",
		"/api/v1/auth-svc/roles",
		"/api/v1/auth-svc/roles/role-1",
		"/api/v1/auth-svc/permissions",
	}
	for _, p := range paths {
		if !svcproxy.AuthManagementPath(p) {
			t.Errorf("%s 应被识别为管理面路径", p)
		}
		rule := svcproxy.Lookup(p)
		if rule == nil {
			t.Fatalf("%s 应命中 auth 前缀规则（由 handler 拦截后返回明确错误）", p)
		}
		if got := rule.RewriteProxyPath(p); got != "" {
			t.Errorf("管理路径 %s 不应被改写，got %q", p, got)
		}

		s := newServiceProxyTestServer()
		req := httptest.NewRequest(http.MethodGet, p, nil)
		w := httptest.NewRecorder()
		s.handleServiceProxy(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s 应返回 404，got %d body=%s", p, w.Code, w.Body.String())
			continue
		}
		var body map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Errorf("%s 响应不是 JSON: %v body=%s", p, err, w.Body.String())
			continue
		}
		if body["error"] != "management plane is local" {
			t.Errorf("%s 错误文案 = %q, want %q", p, body["error"], "management plane is local")
		}
	}
}

// TestAuthSessionProxyPathsStillRewritten 守护收窄改写后会话端点仍按原规则转发。
func TestAuthSessionProxyPathsStillRewritten(t *testing.T) {
	cases := map[string]string{
		"/api/v1/auth-svc/login":           "/api/v1/auth/login",
		"/api/v1/auth-svc/logout":          "/api/v1/auth/logout",
		"/api/v1/auth-svc/refresh":         "/api/v1/auth/refresh",
		"/api/v1/auth-svc/register":        "/api/v1/auth/register",
		"/api/v1/auth-svc/change-password": "/api/v1/auth/change-password",
		"/api/v1/auth-svc/me":              "/api/v1/auth/me",
	}
	for p, want := range cases {
		rule := svcproxy.Lookup(p)
		if rule == nil {
			t.Fatalf("%s 应命中 auth 前缀规则", p)
		}
		if got := rule.RewriteProxyPath(p); got != want {
			t.Errorf("rewrite(%s) = %q, want %q", p, got, want)
		}
	}
	// 非管理/非会话子路径同样不转发（避免未知路径被盲目改写）。
	if got := svcproxy.Lookup("/api/v1/auth-svc/no-such").RewriteProxyPath("/api/v1/auth-svc/no-such"); got != "" {
		t.Errorf("未知 auth 子路径不应改写，got %q", got)
	}
}
