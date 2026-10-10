// service_proxy_auth_test.go auth 域代理的端到端行为守卫（TD-60 A-2 阻断级缺陷的回归测试）。
//
// # 缺陷形态（2026-10-10 端到端冒烟实测，非推断）
//
// `AUTH_SVC_PROXY_ENABLED=true` 打开后，持合法 cookie/token 打 `/api/v1/auth-svc/me`
// 恒返 `403 permission denied: auth:read`——auth 代理规则要求 `auth:read`/`auth:write`，
// 而控制面权限目录没有 `auth:` 组 ⇒ 「用户权限集 ∋ required」对**任何身份（含 admin）**恒假。
// 静态守护当时抓不到它（原先只对账手工维护的 handler 权限点清单，代理的权限点来自规则数据）。
//
// 本文件把「修好之后的行为」钉住：
//  1. 已认证身份走代理前缀 ⇒ **转发到后端**（断言 200 + 后端收到的改写后路径），不再 403；
//  2. 未认证请求 ⇒ 仍被聚合层挡住（不得放行）；
//  3. 非 auth 域规则不受影响（权限闸仍在）——由 TestRulePermsExistInCatalog /
//     TestAuthRuleIsAuthenticatedOnly（svcproxy 包）与既有代理用例共同守着。
package controlplane

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAuthProxyAllowsAuthenticatedWithoutPermCheck 已认证身份经代理前缀访问 auth 自服务端点
// 应被转发（本地同端点也只做 token 校验、不查权限点）。
func TestAuthProxyAllowsAuthenticatedWithoutPermCheck(t *testing.T) {
	var gotPath, gotAuth string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"user":{"username":"admin"}}`))
	}))
	defer backend.Close()

	// 双轨开关打开 + 指向测试后端（两条都在规则/处理器路径上真实生效）。
	t.Setenv("AUTH_SVC_PROXY_ENABLED", "true")
	t.Setenv("AUTH_SVC_URL", backend.URL)

	s := newServiceProxyTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth-svc/me", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleServiceProxy(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200（缺陷形态：目录外权限点 auth:read 导致含 admin 在内恒 403）; body=%s",
			w.Code, w.Body.String())
	}
	if gotPath != "/api/v1/auth/me" {
		t.Errorf("后端收到路径 %s, want /api/v1/auth/me（域前缀应剥除）", gotPath)
	}
	if gotAuth == "" {
		t.Error("后端未收到 Authorization 头——代理应透传凭证")
	}
}

// TestAuthProxyForwardsSessionCookie 会话凭证投递（TD-60 §9.3 方向 A，2026-10-10 裁决）：
// auth 域**透传**客户端会话 Cookie——auth-svc 网关是自验 token 模型（bearerOrCookie →
// ValidateToken），聚合层剥 Cookie 会让到达它的每个请求无凭无据恒 401（§9.2 实测，
// 错误信息出自 auth-svc 即穿闸证据）。
//
// 与 device 域既有断言（service_proxy_test.go 的 TestDeviceProxyForwardWithTenantHeader：
// 「后端不应收到 Cookie」）互为对照，共同钉住「凭证只投递给签发方」：
// 常规域剥除 + 注入身份头，auth 域透传凭证。
func TestAuthProxyForwardsSessionCookie(t *testing.T) {
	var gotPath, gotCookie, gotTenant, gotUser string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotCookie = r.Header.Get("Cookie")
		gotTenant = r.Header.Get("X-Tenant-ID")
		gotUser = r.Header.Get("X-User-Id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"user":{"username":"admin"}}`))
	}))
	defer backend.Close()

	t.Setenv("AUTH_SVC_PROXY_ENABLED", "true")
	t.Setenv("AUTH_SVC_URL", backend.URL)

	s := newServiceProxyTestServer()
	token := strings.TrimPrefix(loginAsAdmin(t, s), "Bearer ")

	// 模拟浏览器路径：只带 HttpOnly Cookie（无 Authorization 头、无裸租户头）——
	// 聚合层从 Cookie 提取租户身份（tenantFromBearer 的 Cookie 回退分支）。
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth-svc/me", nil)
	req.AddCookie(&http.Cookie{Name: accessTokenCookieName, Value: token})
	w := httptest.NewRecorder()
	s.handleServiceProxy(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200（带 Cookie 的自服务请求应穿闸并抵达后端）; body=%s", w.Code, w.Body.String())
	}
	if gotPath != "/api/v1/auth/me" {
		t.Errorf("后端收到路径 %s, want /api/v1/auth/me（域前缀应剥除）", gotPath)
	}
	// 只报字节数，不回显凭证本身（CI 日志不落 token；断言仍逐字节等价）。
	if wantCookie := accessTokenCookieName + "=" + token; gotCookie != wantCookie {
		t.Errorf("后端 Cookie %d 字节, want %d 字节——auth 域必须透传会话 Cookie（§9.3 方向 A）；"+
			"0 字节即被剥除，是 §9.2 恒 401 缺陷的复发形态", len(gotCookie), len(wantCookie))
	}
	// 身份头照旧剥离重注入（来自同一枚 Cookie 的 JWT 声明）——auth 域在身份治理上与其他域一致。
	if gotTenant != "default" {
		t.Errorf("后端 X-Tenant-ID = %q, want default（Cookie JWT 的租户应被注入转发）", gotTenant)
	}
	if want := s.store.GetUserByUsername("admin").ID; gotUser != want {
		t.Errorf("后端 X-User-Id = %q, want %q（Cookie JWT 的用户应被注入转发）", gotUser, want)
	}
}

// TestAuthProxyStillRequiresAuthentication 自服务端点的「仅认证」不是「不要认证」：
// 在**生产语义**（RequireAuth=true，网关注入租户头 + 凭证）下，无凭证请求必须被聚合层挡住，
// 不得因为跳过权限闸而变成匿名可达。
//
// 注：RequireAuth=false 的开放模式（开发降级）本就放行裸租户头——那是站内一致的全局配置语义，
// 不是本处引入的放宽；故本用例显式采用生产语义断言，避免把开放模式的既定行为当缺陷。
func TestAuthProxyStillRequiresAuthentication(t *testing.T) {
	backendCalled := false
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendCalled = true
		_, _ = w.Write([]byte(`{}`))
	}))
	defer backend.Close()

	t.Setenv("AUTH_SVC_PROXY_ENABLED", "true")
	t.Setenv("AUTH_SVC_URL", backend.URL)

	s := newServiceProxyTestServer()
	// 生产语义：凭证缺失即拒。注意 requireTenantContext 读的是 Server 的 requireAuth 字段
	// （NewServer 从 cfg 装载），故两处都置位——只置 cfg 不会生效（本用例初版踩过）。
	s.cfg.RequireAuth = true
	s.requireAuth = true

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth-svc/me", nil)
	req.Header.Set("X-Tenant-ID", "default") // 只有租户头、无凭证
	w := httptest.NewRecorder()
	s.handleServiceProxy(w, req)

	if w.Code == http.StatusOK {
		t.Fatalf("无凭证请求被放行（status 200）——仅认证语义退化成匿名可达；body=%s", w.Body.String())
	}
	if backendCalled {
		t.Fatalf("无凭证请求被转发到后端（status=%d）——聚合层鉴权失效", w.Code)
	}
}
