// auth_test.go 测试用户中心 HTTP handler：注册/登录/me + 用户/角色/权限 CRUD + 预定义数据。
//
// 测试策略：
//   - 每个用例构造独立的 MemoryStore + Server（jwtSecret 固定为测试密钥，避免随机性）；
//   - 用 httptest.NewRequest + Server.handler 直接调用 handler，不走完整 HTTP server；
//   - 预定义数据（admin/operator/viewer + 权限/角色）由 NewMemoryStore.seedRBAC() 自动填充；
//   - 鉴权用例通过 admin 登录获取 token，携带 Authorization: Bearer <token> 调用管理 API。
package controlplane

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"opsmesh/internal/config"
	"opsmesh/internal/store"
)

// newAuthTestServer 构造测试用 Server：memory store + 固定 jwtSecret。
// 固定 jwtSecret 避免随机性导致测试不稳定。
func newAuthTestServer(t *testing.T) *Server {
	t.Helper()
	st := store.NewMemoryStore()
	return &Server{
		store:     st,
		cfg:       &config.Config{TaskMaxRetries: 3},
		jwtSecret: []byte("test-jwt-secret-for-auth-test-32bytes!"),
	}
}

// loginAsAdmin 用预定义 admin 账号登录，返回 Authorization 头值。
// 用于需要鉴权的用户管理 API 测试。
func loginAsAdmin(t *testing.T, s *Server) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "admin123"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleAuthLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin login = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp authResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode login resp: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("login token is empty")
	}
	return "Bearer " + resp.Token
}

// doWithAuth 构造携带 Authorization 头的请求。
func doWithAuth(method, path, auth string, body interface{}) *http.Request {
	var r bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = *bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, &r)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	return req
}

// ----------------------------------------------------------------------------
// 预定义数据
// ----------------------------------------------------------------------------

// TestPredefinedData 验证预定义权限/角色/用户存在。
func TestPredefinedData(t *testing.T) {
	s := newAuthTestServer(t)

	// 预定义权限：应有 25 个（device 3 + task 3 + alert 3 + cmdb 2 + deploy 2 + workflow 2 + log 1 + audit 1 + user 3 + role 3 + federation 2）。
	perms := s.store.ListPermissions()
	if len(perms) != 25 {
		t.Fatalf("permissions count = %d, want 25", len(perms))
	}
	// 检查关键权限存在。
	permNames := make(map[string]bool)
	for _, p := range perms {
		permNames[p.Name] = true
	}
	for _, want := range []string{"device:read", "device:write", "device:delete", "task:read", "task:write", "task:cancel", "alert:read", "alert:ack", "alert:silence", "cmdb:read", "cmdb:write", "deploy:read", "deploy:write", "workflow:read", "workflow:write", "log:read", "audit:read", "user:read", "user:write", "user:delete", "role:read", "role:write", "role:delete", "federation:read", "federation:write"} {
		if !permNames[want] {
			t.Fatalf("missing permission %q", want)
		}
	}

	// 预定义角色：admin / operator / viewer。
	roles := s.store.ListRoles()
	if len(roles) != 3 {
		t.Fatalf("roles count = %d, want 3", len(roles))
	}
	roleByName := make(map[string]*store.Role)
	for _, r := range roles {
		roleByName[r.Name] = r
	}
	if admin := roleByName["admin"]; admin == nil || len(admin.Permissions) != 25 {
		t.Fatalf("admin role missing or permissions = %d, want 25", len(admin.Permissions))
	}
	if viewer := roleByName["viewer"]; viewer == nil {
		t.Fatal("viewer role missing")
	}
	if op := roleByName["operator"]; op == nil {
		t.Fatal("operator role missing")
	}

	// 预定义用户：admin / operator / viewer。
	for _, name := range []string{"admin", "operator", "viewer"} {
		u := s.store.GetUserByUsername(name)
		if u == nil {
			t.Fatalf("predefined user %q missing", name)
		}
		if u.Status != "active" {
			t.Fatalf("user %q status = %q, want active", name, u.Status)
		}
		if len(u.RoleIDs) != 1 {
			t.Fatalf("user %q should have 1 role, got %d", name, len(u.RoleIDs))
		}
	}
}

// ----------------------------------------------------------------------------
// 注册
// ----------------------------------------------------------------------------

// TestAuthRegister 注册新用户 → 201 + token。
func TestAuthRegister(t *testing.T) {
	s := newAuthTestServer(t)
	body, _ := json.Marshal(map[string]string{"username": "newuser", "password": "pass123", "email": "new@test.com"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleAuthRegister(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("register = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp authResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("token is empty")
	}
	if resp.User == nil || resp.User.Username != "newuser" {
		t.Fatalf("user = %+v, want username=newuser", resp.User)
	}
	// PasswordHash 不应在 JSON 响应中泄露（json:"-"）。
	if resp.User.PasswordHash != "" {
		t.Fatal("PasswordHash leaked in response")
	}
}

// TestAuthRegisterDuplicate 重复用户名 → 409。
func TestAuthRegisterDuplicate(t *testing.T) {
	s := newAuthTestServer(t)
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "pass123"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleAuthRegister(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate register = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
}

// TestAuthRegisterShortPassword 密码过短 → 400。
func TestAuthRegisterShortPassword(t *testing.T) {
	s := newAuthTestServer(t)
	body, _ := json.Marshal(map[string]string{"username": "shortpw", "password": "123"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleAuthRegister(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("short password = %d, want 400", rec.Code)
	}
}

// ----------------------------------------------------------------------------
// 登录
// ----------------------------------------------------------------------------

// TestAuthLogin 正确密码 → 200 + token。
func TestAuthLogin(t *testing.T) {
	s := newAuthTestServer(t)
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "admin123"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleAuthLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp authResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("token empty")
	}
	if resp.User.Username != "admin" {
		t.Fatalf("username = %q, want admin", resp.User.Username)
	}
}

// TestAuthLoginWrongPassword 错误密码 → 401。
func TestAuthLoginWrongPassword(t *testing.T) {
	s := newAuthTestServer(t)
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "wrongpassword"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleAuthLogin(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password = %d, want 401", rec.Code)
	}
}

// TestAuthLoginNonexistent 用户不存在 → 401。
func TestAuthLoginNonexistent(t *testing.T) {
	s := newAuthTestServer(t)
	body, _ := json.Marshal(map[string]string{"username": "ghost", "password": "whatever"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleAuthLogin(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("nonexistent user = %d, want 401", rec.Code)
	}
}

// ----------------------------------------------------------------------------
// /api/v1/auth/me
// ----------------------------------------------------------------------------

// TestAuthMe 携带 token → 200 + user。
func TestAuthMe(t *testing.T) {
	s := newAuthTestServer(t)
	auth := loginAsAdmin(t, s)
	req := doWithAuth(http.MethodGet, "/api/v1/auth/me", auth, nil)
	rec := httptest.NewRecorder()
	s.handleAuthMe(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("me = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var u store.User
	if err := json.NewDecoder(rec.Body).Decode(&u); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if u.Username != "admin" {
		t.Fatalf("username = %q, want admin", u.Username)
	}
}

// TestAuthMeNoToken 无 token → 401。
func TestAuthMeNoToken(t *testing.T) {
	s := newAuthTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	rec := httptest.NewRecorder()
	s.handleAuthMe(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("me without token = %d, want 401", rec.Code)
	}
}

// TestAuthMeInvalidToken 无效 token → 401。
func TestAuthMeInvalidToken(t *testing.T) {
	s := newAuthTestServer(t)
	req := doWithAuth(http.MethodGet, "/api/v1/auth/me", "Bearer invalid.token.here", nil)
	rec := httptest.NewRecorder()
	s.handleAuthMe(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("me with invalid token = %d, want 401", rec.Code)
	}
}

// ----------------------------------------------------------------------------
// 用户管理 API
// ----------------------------------------------------------------------------

// TestListUsers 列表 → 200。
func TestListUsers(t *testing.T) {
	s := newAuthTestServer(t)
	auth := loginAsAdmin(t, s)
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
	if len(resp.Users) < 3 {
		t.Fatalf("users count = %d, want >=3 (predefined)", len(resp.Users))
	}
}

// TestListUsersNoPermission 无权限用户（operator，无 user:read）→ 403。
func TestListUsersNoPermission(t *testing.T) {
	s := newAuthTestServer(t)
	// operator 登录（operator 组不含 user/role，故无 user:read 权限）。
	body, _ := json.Marshal(map[string]string{"username": "operator", "password": "operator123"})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	loginRec := httptest.NewRecorder()
	s.handleAuthLogin(loginRec, loginReq)
	var loginResp authResponse
	_ = json.NewDecoder(loginRec.Body).Decode(&loginResp)
	auth := "Bearer " + loginResp.Token

	req := doWithAuth(http.MethodGet, "/api/v1/users", auth, nil)
	rec := httptest.NewRecorder()
	s.handleUsers(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("operator list users = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

// TestCreateUser 创建用户 → 201。
func TestCreateUser(t *testing.T) {
	s := newAuthTestServer(t)
	auth := loginAsAdmin(t, s)
	req := doWithAuth(http.MethodPost, "/api/v1/users", auth, map[string]interface{}{
		"username": "createduser",
		"password": "pass123",
		"email":    "created@test.com",
		"role_ids": []string{"role-viewer"},
	})
	rec := httptest.NewRecorder()
	s.handleUsers(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var u store.User
	if err := json.NewDecoder(rec.Body).Decode(&u); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if u.Username != "createduser" {
		t.Fatalf("username = %q, want createduser", u.Username)
	}
}

// TestUpdateUser 更新用户 → 200。
func TestUpdateUser(t *testing.T) {
	s := newAuthTestServer(t)
	auth := loginAsAdmin(t, s)
	// 先创建一个用户。
	createReq := doWithAuth(http.MethodPost, "/api/v1/users", auth, map[string]interface{}{
		"username": "toupdate",
		"password": "pass123",
		"role_ids": []string{"role-viewer"},
	})
	createRec := httptest.NewRecorder()
	s.handleUsers(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("setup create failed: %d %s", createRec.Code, createRec.Body.String())
	}
	var created store.User
	_ = json.NewDecoder(createRec.Body).Decode(&created)

	// 更新 email 和 status。
	req := doWithAuth(http.MethodPut, "/api/v1/users/"+created.ID, auth, map[string]interface{}{
		"email":    "updated@test.com",
		"status":   "disabled",
		"role_ids": []string{"role-operator"},
	})
	rec := httptest.NewRecorder()
	s.handleUserRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update user = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var u store.User
	if err := json.NewDecoder(rec.Body).Decode(&u); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if u.Email != "updated@test.com" {
		t.Fatalf("email = %q, want updated@test.com", u.Email)
	}
	if u.Status != "disabled" {
		t.Fatalf("status = %q, want disabled", u.Status)
	}
}

// TestDeleteUser 删除用户 → 204。
func TestDeleteUser(t *testing.T) {
	s := newAuthTestServer(t)
	auth := loginAsAdmin(t, s)
	// 先创建一个用户。
	createReq := doWithAuth(http.MethodPost, "/api/v1/users", auth, map[string]interface{}{
		"username": "todelete",
		"password": "pass123",
	})
	createRec := httptest.NewRecorder()
	s.handleUsers(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("setup create failed: %d %s", createRec.Code, createRec.Body.String())
	}
	var created store.User
	_ = json.NewDecoder(createRec.Body).Decode(&created)

	// 删除。
	req := doWithAuth(http.MethodDelete, "/api/v1/users/"+created.ID, auth, nil)
	rec := httptest.NewRecorder()
	s.handleUserRouting(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete user = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	// 验证已删除。
	if s.store.GetUser(created.ID) != nil {
		t.Fatal("user still exists after delete")
	}
}

// ----------------------------------------------------------------------------
// 角色管理 API
// ----------------------------------------------------------------------------

// TestListRoles 列表 → 200。
func TestListRoles(t *testing.T) {
	s := newAuthTestServer(t)
	auth := loginAsAdmin(t, s)
	req := doWithAuth(http.MethodGet, "/api/v1/roles", auth, nil)
	rec := httptest.NewRecorder()
	s.handleRoles(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list roles = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Roles []*store.Role `json:"roles"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Roles) < 3 {
		t.Fatalf("roles count = %d, want >=3", len(resp.Roles))
	}
}

// TestCreateRole 创建角色 → 201。
func TestCreateRole(t *testing.T) {
	s := newAuthTestServer(t)
	auth := loginAsAdmin(t, s)
	req := doWithAuth(http.MethodPost, "/api/v1/roles", auth, map[string]interface{}{
		"name":        "customrole",
		"description": "自定义角色",
		"permissions": []string{"device:read", "task:read"},
	})
	rec := httptest.NewRecorder()
	s.handleRoles(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create role = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var r store.Role
	if err := json.NewDecoder(rec.Body).Decode(&r); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if r.Name != "customrole" {
		t.Fatalf("name = %q, want customrole", r.Name)
	}
}

// TestUpdateRole 更新角色 → 200。
func TestUpdateRole(t *testing.T) {
	s := newAuthTestServer(t)
	auth := loginAsAdmin(t, s)
	// 先创建角色。
	createReq := doWithAuth(http.MethodPost, "/api/v1/roles", auth, map[string]interface{}{
		"name":        "roleToUpdate",
		"description": "初始描述",
		"permissions": []string{"device:read"},
	})
	createRec := httptest.NewRecorder()
	s.handleRoles(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("setup create failed: %d %s", createRec.Code, createRec.Body.String())
	}
	var created store.Role
	_ = json.NewDecoder(createRec.Body).Decode(&created)

	// 更新。
	req := doWithAuth(http.MethodPut, "/api/v1/roles/"+created.ID, auth, map[string]interface{}{
		"description": "更新后描述",
		"permissions": []string{"device:read", "device:write"},
	})
	rec := httptest.NewRecorder()
	s.handleRoleRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update role = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// TestDeleteRole 删除角色 → 204。
func TestDeleteRole(t *testing.T) {
	s := newAuthTestServer(t)
	auth := loginAsAdmin(t, s)
	// 先创建角色。
	createReq := doWithAuth(http.MethodPost, "/api/v1/roles", auth, map[string]interface{}{
		"name":        "roleToDelete",
		"description": "待删除",
	})
	createRec := httptest.NewRecorder()
	s.handleRoles(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("setup create failed: %d %s", createRec.Code, createRec.Body.String())
	}
	var created store.Role
	_ = json.NewDecoder(createRec.Body).Decode(&created)

	req := doWithAuth(http.MethodDelete, "/api/v1/roles/"+created.ID, auth, nil)
	rec := httptest.NewRecorder()
	s.handleRoleRouting(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete role = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
}

// ----------------------------------------------------------------------------
// 权限查询 API
// ----------------------------------------------------------------------------

// TestListPermissions 列表 → 200。
func TestListPermissions(t *testing.T) {
	s := newAuthTestServer(t)
	auth := loginAsAdmin(t, s)
	req := doWithAuth(http.MethodGet, "/api/v1/permissions", auth, nil)
	rec := httptest.NewRecorder()
	s.handlePermissions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list permissions = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Permissions []*store.Permission `json:"permissions"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Permissions) != 25 {
		t.Fatalf("permissions count = %d, want 25", len(resp.Permissions))
	}
}

// TestListPermissionsNoToken 无 token → 401。
func TestListPermissionsNoToken(t *testing.T) {
	s := newAuthTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/permissions", nil)
	rec := httptest.NewRecorder()
	s.handlePermissions(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("permissions without token = %d, want 401", rec.Code)
	}
}
