// cross_tenant_isolation_test.go 跨租户隔离矩阵（M10）。
//
// 验证 admin token（携带 tenant_id=default）带 X-Tenant-ID:other-tenant 访问
// marketplace/apikey/billing/tenant 四域的租户隔离行为：
//   - apikey/billing(subscriptions,invoices)：requireTenantContext 交叉校验
//     token tenant ≠ header tenant → 403 Forbidden（防绕过网关伪造租户头）；
//   - marketplace：全局插件市场，不做租户校验，跨租户访问放行是设计行为（断言 200 文档化现状）；
//   - tenant：平台级管理（admin 管理所有租户），不做租户校验，跨租户访问放行是设计行为（断言 200）。
//
// 设计说明：marketplace/tenant 不做租户隔离是当前架构的有意设计（插件市场为全局共享，
// 租户管理为平台级超管操作），而非漏洞。本矩阵锁定四域的跨租户行为基线，防止后续
// 误改 handler 引入租户校验导致回归，也为未来若决定给 marketplace 加租户隔离留测试锚点。
package controlplane

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCrossTenantIsolationMatrix 跨租户隔离矩阵：t1（admin token tenant_id=default）
// 带 X-Tenant-ID:t2 访问四域，断言租户隔离行为基线。
func TestCrossTenantIsolationMatrix(t *testing.T) {
	s := newAPIKeyTestServer()
	auth := loginAsAdmin(t, s)
	const crossTenant = "other-tenant"

	// apikey 域：handleAPIKeys → handleListAPIKeys 调用 k8sTenantFromRequest
	// → requireTenantContext 交叉校验 token tenant(default) ≠ header(other-tenant) → 403。
	t.Run("apikey_list_403", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/apikeys", nil)
		req.Header.Set("Authorization", auth)
		req.Header.Set("X-Tenant-ID", crossTenant)
		w := httptest.NewRecorder()
		s.handleAPIKeys(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status=%d, want 403; body=%s", w.Code, w.Body.String())
		}
	})

	// apikey 域子路径：handleAPIKeyRouting → handleGetAPIKey 同样调用 k8sTenantFromRequest → 403。
	t.Run("apikey_get_403", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/apikeys/some-id", nil)
		req.Header.Set("Authorization", auth)
		req.Header.Set("X-Tenant-ID", crossTenant)
		w := httptest.NewRecorder()
		s.handleAPIKeyRouting(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status=%d, want 403; body=%s", w.Code, w.Body.String())
		}
	})

	// billing 域 subscriptions：handleBillingSubscriptions → handleListSubscriptions
	// 调用 k8sTenantFromRequest → 403。
	t.Run("billing_subscriptions_list_403", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/billing/subscriptions", nil)
		req.Header.Set("Authorization", auth)
		req.Header.Set("X-Tenant-ID", crossTenant)
		w := httptest.NewRecorder()
		s.handleBillingSubscriptions(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status=%d, want 403; body=%s", w.Code, w.Body.String())
		}
	})

	// billing 域 invoices：handleBillingInvoices 调用 k8sTenantFromRequest → 403。
	t.Run("billing_invoices_list_403", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/billing/invoices", nil)
		req.Header.Set("Authorization", auth)
		req.Header.Set("X-Tenant-ID", crossTenant)
		w := httptest.NewRecorder()
		s.handleBillingInvoices(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status=%d, want 403; body=%s", w.Code, w.Body.String())
		}
	})

	// marketplace 域：handleMarketplacePlugins → handleListPlugins 用 requirePermission
	// （不调用 k8sTenantFromRequest），全局插件市场无租户隔离 → 200。
	// 断言 200 锁定当前设计基线：若未来给 marketplace 加租户校验，此测试会失败提示更新断言。
	t.Run("marketplace_list_200_global_no_tenant_check", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/marketplace/plugins", nil)
		req.Header.Set("Authorization", auth)
		req.Header.Set("X-Tenant-ID", crossTenant)
		w := httptest.NewRecorder()
		s.handleMarketplacePlugins(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200 (marketplace 为全局插件市场，不做租户校验是设计行为); body=%s", w.Code, w.Body.String())
		}
	})

	// tenant 域：handleTenants → handleListTenants 用 requirePermission
	// （不调用 k8sTenantFromRequest），平台级超管操作无租户隔离 → 200。
	// 断言 200 锁定当前设计基线：admin 可管理所有租户，跨租户头不影响列表。
	t.Run("tenant_list_200_platform_level_no_tenant_check", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
		req.Header.Set("Authorization", auth)
		req.Header.Set("X-Tenant-ID", crossTenant)
		w := httptest.NewRecorder()
		s.handleTenants(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200 (tenant 为平台级管理，不做租户校验是设计行为); body=%s", w.Code, w.Body.String())
		}
	})
}
