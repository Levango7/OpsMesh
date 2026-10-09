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
