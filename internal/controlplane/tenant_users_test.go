// tenant_users_test.go 覆盖 P0-6 用户侧租户归属（问题 A 的修复）。
//
// 背景：修复前 store.User 无 TenantID，登录签发的 JWT 租户恒为 default，
// 且用户管理 API 允许任意调用者创建任意租户归属的账号——多租户形同虚设。
// 本文件锁死修复后语义：
//   - 用户归属租户，登录签发的 JWT 携带该租户（下游下发路径据此做归属校验）；
//   - 创建/更新用户的目标租户由 resolveUserTenant 统一裁决（仅平台租户可跨租户指派）；
//   - 用户列表按调用方租户过滤（平台租户保持全量视图）。
package controlplane

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Levango7/OpsMesh/internal/store"
)

// loginAsUser 用指定账号密码登录，返回 Authorization 头值。
func loginAsUser(t *testing.T, s *Server, username, password string) string {
	t.Helper()
	clearMustChangeFlag(s, username)
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleAuthLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s login = %d, want 200; body=%s", username, rec.Code, rec.Body.String())
	}
	var resp authResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode login resp: %v", err)
	}
	if resp.Token == "" {
		t.Fatalf("%s login token is empty", username)
	}
	return "Bearer " + resp.Token
}

// createTenantAdmin 直接在 store 建一个指定租户的 admin 角色用户并返回其登录凭证。
func createTenantAdmin(t *testing.T, s *Server, tenantID string) string {
	t.Helper()
	hash, err := hashPassword("Tenant123")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	u := &store.User{
		ID:           "user-tenant-admin-" + tenantID,
		Username:     "admin-" + tenantID,
		TenantID:     tenantID,
		PasswordHash: hash,
		Status:       "active",
		RoleIDs:      []string{"role-admin"},
	}
	if s.store.CreateUser(u) == nil {
		t.Fatalf("CreateUser(tenant=%s) failed", tenantID)
	}
	return loginAsUser(t, s, u.Username, "Tenant123")
}

// =============================================================================
// resolveUserTenant（auth_users.go）
// =============================================================================

func TestResolveUserTenant(t *testing.T) {
	platform := &store.User{ID: "u1", TenantID: store.DefaultTenantID}
	tenantA := &store.User{ID: "u2", TenantID: "acme"}

	// 未指定：归属调用方租户。
	if got, ok := resolveUserTenant(httptest.NewRecorder(), platform, ""); !ok || got != store.DefaultTenantID {
		t.Fatalf("平台调用者缺省租户 = %q/%v, want default/true", got, ok)
	}
	if got, ok := resolveUserTenant(httptest.NewRecorder(), tenantA, ""); !ok || got != "acme" {
		t.Fatalf("租户调用者缺省租户 = %q/%v, want acme/true", got, ok)
	}
	// 平台调用者显式指定他租户：放行。
	if got, ok := resolveUserTenant(httptest.NewRecorder(), platform, "acme"); !ok || got != "acme" {
		t.Fatalf("平台调用者指派 acme = %q/%v, want acme/true", got, ok)
	}
	// 非平台调用者指定他租户：403。
	rec := httptest.NewRecorder()
	if _, ok := resolveUserTenant(rec, tenantA, "other"); ok {
		t.Fatal("非平台调用者指派他租户应拒绝")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", rec.Code)
	}
	// 非法字符集：400（租户 id 流入 schema 名/路径过滤，必须白名单）。
	rec = httptest.NewRecorder()
	if _, ok := resolveUserTenant(rec, platform, "bad tenant"); ok {
		t.Fatal("非法租户 id 应拒绝")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

// =============================================================================
// handleCreateUser：租户指派
// =============================================================================

func TestCreateUser_PlatformAdminAssignsTenant(t *testing.T) {
	s := newAuthTestServer(t)
	auth := loginAsAdmin(t, s)
	req := doWithAuth(http.MethodPost, "/api/v1/users", auth, map[string]interface{}{
		"username": "acme-ops", "password": "Pass1234", "role_ids": []string{"role-viewer"},
		"tenantId": "acme",
	})
	rec := httptest.NewRecorder()
	s.handleUsers(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	created := s.store.GetUserByUsername("acme-ops")
	if created == nil || created.TenantID != "acme" {
		t.Fatalf("用户租户未落库: %+v", created)
	}
}

func TestCreateUser_InvalidTenantID(t *testing.T) {
	s := newAuthTestServer(t)
	auth := loginAsAdmin(t, s)
	req := doWithAuth(http.MethodPost, "/api/v1/users", auth, map[string]interface{}{
		"username": "bad-tenant-user", "password": "Pass1234", "tenantId": "bad/tenant",
	})
	rec := httptest.NewRecorder()
	s.handleUsers(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法租户 id status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateUser_TenantAdminCannotCrossAssign(t *testing.T) {
	s := newAuthTestServer(t)
	auth := createTenantAdmin(t, s, "acme")

	req := doWithAuth(http.MethodPost, "/api/v1/users", auth, map[string]interface{}{
		"username": "victim-user", "password": "Pass1234", "tenantId": "victim",
	})
	rec := httptest.NewRecorder()
	s.handleUsers(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("跨租户创建 status=%d, want 403; body=%s", rec.Code, rec.Body.String())
	}
	if s.store.GetUserByUsername("victim-user") != nil {
		t.Fatal("拒绝路径不得创建用户")
	}
}

// =============================================================================
// handleListUsers：按调用方租户过滤
// =============================================================================

func TestListUsers_TenantScoped(t *testing.T) {
	s := newAuthTestServer(t)
	auth := createTenantAdmin(t, s, "acme")
	// 另建一个他租户用户，确认不会出现在 acme 管理员的列表里。
	createTenantAdmin(t, s, "other")

	req := doWithAuth(http.MethodGet, "/api/v1/users", auth, nil)
	rec := httptest.NewRecorder()
	s.handleUsers(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list users = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Users []*store.User `json:"users"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Users) == 0 {
		t.Fatal("acme 管理员应至少看到自己")
	}
	for _, u := range resp.Users {
		if tenantOrDefault(u.TenantID) != "acme" {
			t.Fatalf("列表泄露他租户用户: %+v", u)
		}
	}
}

// =============================================================================
// 登录签发：JWT 携带用户所属租户
// =============================================================================

func TestLogin_JWTContainsUserTenant(t *testing.T) {
	s := newAuthTestServer(t)
	auth := createTenantAdmin(t, s, "acme")

	req := doWithAuth(http.MethodGet, "/api/v1/auth/me", auth, nil)
	rec := httptest.NewRecorder()
	s.handleAuthMe(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("me = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var me store.User
	if err := json.NewDecoder(rec.Body).Decode(&me); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if me.TenantID != "acme" {
		t.Fatalf("me 返回用户租户错误: %+v", me)
	}
	// 令牌侧：tenantFromBearer 应从 JWT 解出 acme（下游租户校验依赖此值）。
	if tenant, _ := s.tenantFromBearer(req); tenant != "acme" {
		t.Fatalf("JWT tenant = %q, want acme", tenant)
	}
}
