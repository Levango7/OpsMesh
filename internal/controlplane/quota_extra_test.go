package controlplane

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opsmesh/internal/authctx"
	"opsmesh/internal/config"
	"opsmesh/internal/store"
)

// 本文件补全 quota.go 的单元测试（配额管理 API）。
// 覆盖：handleQuotas/listQuotas、handleQuotaRouting/getQuota/setQuota/deleteQuota、isAdmin。
//
// 注意：quota:read/quota:write 不在预置 rbacPermSpecs 中，admin 角色经
// getRolePermCache（store.RolePermissions）无此权限。故需在 store 中更新
// admin 角色权限 + 用 JWT 路径（requirePermission 从 store.GetRole 动态读取）。

// newQuotaTestServer 构造带 quotaMgr 的测试 Server。
func newQuotaTestServer() *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	s := &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3, Demo: true},
		quotaMgr:     NewQuotaManager(st, true, &store.QuotaConfig{MaxDevices: 100, MaxTasks: 1000, MaxAlerts: 500}),
		sessionStore: ss,
		jwtSecret:    []byte("test-jwt-secret-for-quota-test-32b!"),
	}
	// 给 admin 角色添加 quota:read/quota:write 权限（requirePermission 从 store.GetRole 动态读取）
	if r := st.GetRole("role-admin"); r != nil {
		r.Permissions = append(r.Permissions, "quota:read", "quota:write")
		st.UpdateRole(r)
	}
	return s
}

// quotaAdminAuth 构造一个带 admin 角色 + quota 权限的 JWT，返回 Authorization 头值。
func quotaAdminAuth(s *Server) string {
	u := s.store.GetUserByUsername("admin")
	if u == nil {
		return ""
	}
	// 确保用户有 admin 角色
	u.RoleIDs = []string{"role-admin"}
	s.store.UpdateUser(u)
	// 清除改密标记
	s.store.ChangePassword(u.ID, u.PasswordHash)
	token, _ := s.issueUserToken(u)
	return "Bearer " + token
}

// =============================================================================
// handleQuotas / listQuotas
// =============================================================================

func TestHandleQuotas_List(t *testing.T) {
	s := newQuotaTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/quotas", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleQuotas(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleQuotas_MethodNotAllowed(t *testing.T) {
	s := newQuotaTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/quotas", nil)
	rec := httptest.NewRecorder()
	s.handleQuotas(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post = %d, want 405", rec.Code)
	}
}

func TestHandleQuotas_NilQuotaMgr(t *testing.T) {
	s := newQuotaTestServer()
	s.quotaMgr = nil
	req := httptest.NewRequest(http.MethodGet, "/api/v1/quotas", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleQuotas(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("nil mgr list = %d, want 200", rec.Code)
	}
}

// =============================================================================
// handleQuotaRouting / getQuota / setQuota / deleteQuota
// =============================================================================

func TestHandleQuotaRouting_EmptyTenant(t *testing.T) {
	s := newQuotaTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/quotas/", nil)
	rec := httptest.NewRecorder()
	s.handleQuotaRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty tenant = %d, want 400", rec.Code)
	}
}

func TestHandleQuotaRouting_Get(t *testing.T) {
	s := newQuotaTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/quotas/t1", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleQuotaRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleQuotaRouting_GetAsAdmin(t *testing.T) {
	s := newQuotaTestServer()
	auth := quotaAdminAuth(s)
	// admin 查看其他租户配额（用 JWT + X-User-Roles 头）
	// requireProd 优先走 JWT 路径（requirePermission 从 store.GetRole 读权限）
	// isAdmin 从 X-User-Roles 获取角色
	req := httptest.NewRequest(http.MethodGet, "/api/v1/quotas/t2", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-User-Roles", "role-admin")
	rec := httptest.NewRecorder()
	s.handleQuotaRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin get = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleQuotaRouting_GetTenantMismatch(t *testing.T) {
	s := newQuotaTestServer()
	// 非 admin（demo 模式无角色）查看他租户 → 403
	req := httptest.NewRequest(http.MethodGet, "/api/v1/quotas/t2", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleQuotaRouting(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant get = %d, want 403", rec.Code)
	}
}

func TestHandleQuotaRouting_Set(t *testing.T) {
	s := newQuotaTestServer()
	auth := quotaAdminAuth(s)
	body := strings.NewReader(`{"maxDevices":50,"maxTasks":200,"maxAlerts":100}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/quotas/t1", body)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-User-Roles", "role-admin")
	rec := httptest.NewRecorder()
	s.handleQuotaRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("set = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleQuotaRouting_SetNonAdmin(t *testing.T) {
	s := newQuotaTestServer()
	body := strings.NewReader(`{"maxDevices":50}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/quotas/t1", body)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleQuotaRouting(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin set = %d, want 403", rec.Code)
	}
}

func TestHandleQuotaRouting_SetNegative(t *testing.T) {
	s := newQuotaTestServer()
	auth := quotaAdminAuth(s)
	body := strings.NewReader(`{"maxDevices":-1}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/quotas/t1", body)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-User-Roles", "role-admin")
	rec := httptest.NewRecorder()
	s.handleQuotaRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("negative = %d, want 400", rec.Code)
	}
}

func TestHandleQuotaRouting_SetBadJSON(t *testing.T) {
	s := newQuotaTestServer()
	auth := quotaAdminAuth(s)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/quotas/t1", strings.NewReader("{bad"))
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-User-Roles", "role-admin")
	rec := httptest.NewRecorder()
	s.handleQuotaRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d, want 400", rec.Code)
	}
}

func TestHandleQuotaRouting_SetNilMgr(t *testing.T) {
	s := newQuotaTestServer()
	s.quotaMgr = nil
	auth := quotaAdminAuth(s)
	body := strings.NewReader(`{"maxDevices":50}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/quotas/t1", body)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-User-Roles", "role-admin")
	rec := httptest.NewRecorder()
	s.handleQuotaRouting(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil mgr set = %d, want 503", rec.Code)
	}
}

func TestHandleQuotaRouting_Delete(t *testing.T) {
	s := newQuotaTestServer()
	auth := quotaAdminAuth(s)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/quotas/t1", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-User-Roles", "role-admin")
	rec := httptest.NewRecorder()
	s.handleQuotaRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleQuotaRouting_DeleteNonAdmin(t *testing.T) {
	s := newQuotaTestServer()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/quotas/t1", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleQuotaRouting(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin delete = %d, want 403", rec.Code)
	}
}

func TestHandleQuotaRouting_MethodNotAllowed(t *testing.T) {
	s := newQuotaTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/quotas/t1", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleQuotaRouting(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post = %d, want 405", rec.Code)
	}
}

// =============================================================================
// isAdmin
// =============================================================================

func TestIsAdmin(t *testing.T) {
	s := newQuotaTestServer()
	// 无角色 → false
	if s.isAdmin(authctx.Context{}) {
		t.Fatal("no roles should be false")
	}
	// admin 角色 → true
	if !s.isAdmin(authctx.Context{TenantID: "t1", UserID: "u1", Roles: []string{"admin"}}) {
		t.Fatal("admin should be true")
	}
	// role-admin → true
	if !s.isAdmin(authctx.Context{TenantID: "t1", UserID: "u1", Roles: []string{"role-admin"}}) {
		t.Fatal("role-admin should be true")
	}
	// 其他角色 → false
	if s.isAdmin(authctx.Context{TenantID: "t1", UserID: "u1", Roles: []string{"viewer"}}) {
		t.Fatal("viewer should be false")
	}
}
