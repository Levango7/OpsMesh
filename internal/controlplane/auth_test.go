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
	"time"

	"opsmesh/internal/config"
	"opsmesh/internal/store"
)

// newAuthTestServer 构造测试用 Server：memory store + 固定 jwtSecret。
// 固定 jwtSecret 避免随机性导致测试不稳定。
// P1-7 注册安全：测试默认启用 demo 模式 + AllowPublicRegister=true（注册即激活 + 立即签发 token），
// 保持原有测试行为不变；pending 审批行为由独立测试用例覆盖（TestAuthRegisterDemoPending/TestAuthRegisterPending）。
func newAuthTestServer(t *testing.T) *Server {
	t.Helper()
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3, Demo: true, PublicRegister: true, AllowPublicRegister: true},
		jwtSecret:    []byte("test-jwt-secret-for-auth-test-32bytes!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

// loginAsAdmin 用预定义 admin 账号登录，返回 Authorization 头值。
// 用于需要鉴权的用户管理 API 测试。
//
// 安全债 85 + 任务 96 回归修复：预置 admin 带 MustChangePassword=true，登录时
// auth.go 不签发 access token（仅签发一次性 changePasswordToken），导致 resp.Token
// 为空。此 helper 代表"已完成首登改密的管理员"，故在登录前先经 store 直接清除
// MustChangePassword 标记（传入原 PasswordHash 保持密码不变，仅清标），使登录
// 走正常签发路径。首登改密前的拦截行为（不下发 at、受保护 API 403）由
// TestLoginReturnsMustChangePassword / TestMustChangePasswordBlocksProtectedAPI
// 等专门用例覆盖，不走此 helper。
func loginAsAdmin(t *testing.T, s *Server) string {
	t.Helper()
	// 先清除 MustChangePassword 标记（密码哈希不变），模拟"已改密"状态。
	clearMustChangeFlag(s, "admin")
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

// clearMustChangeFlag 清除指定用户的 MustChangePassword 标记（密码哈希不变），
// 用于需要"已改密"状态的测试用例。安全债 85 + 任务 96 回归适配。
func clearMustChangeFlag(s *Server, username string) {
	if u := s.store.GetUserByUsername(username); u != nil && u.MustChangePassword {
		s.store.ChangePassword(u.ID, u.PasswordHash)
	}
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

	// 预定义权限：应有 34 个（新增 provision/k8s/middleware 等 RBAC 权限，task 96）。
	perms := s.store.ListPermissions()
	if len(perms) != 34 {
		t.Fatalf("permissions count = %d, want 34", len(perms))
	}
	// 检查关键权限存在。
	permNames := make(map[string]bool)
	for _, p := range perms {
		permNames[p.Name] = true
	}
	for _, want := range []string{"device:read", "device:write", "device:delete", "task:read", "task:write", "task:cancel", "alert:read", "alert:ack", "alert:silence", "cmdb:read", "cmdb:write", "deploy:read", "deploy:write", "workflow:read", "workflow:write", "log:read", "audit:read", "user:read", "user:write", "user:delete", "user:approve", "role:read", "role:write", "role:delete", "federation:read", "federation:write"} {
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
	if admin := roleByName["admin"]; admin == nil || len(admin.Permissions) != 34 {
		t.Fatalf("admin role missing or permissions = %d, want 34", len(admin.Permissions))
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
	body, _ := json.Marshal(map[string]string{"username": "newuser", "password": "Pass1234", "email": "new@test.com"})
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
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "Pass1234"})
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
// 安全债 85 + 任务 96：预置 admin 带 mustChangePassword=true，登录不签发 access token。
// 此用例验证"已改密用户正确密码 → 200 + token"，先清标模拟改密后状态。
// 未改密用户的登录响应由 TestLoginReturnsMustChangePassword 覆盖。
func TestAuthLogin(t *testing.T) {
	s := newAuthTestServer(t)
	clearMustChangeFlag(s, "admin")
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
	clearMustChangeFlag(s, "operator")
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
		"password": "Pass1234",
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
		"password": "Pass1234",
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
		"password": "Pass1234",
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
	if len(resp.Permissions) != 34 {
		t.Fatalf("permissions count = %d, want 34", len(resp.Permissions))
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

// ----------------------------------------------------------------------------
// P1-7 注册安全：公开注册开关 + pending 审批流程
// ----------------------------------------------------------------------------

// newAuthTestServerNonDemo 构造非 demo 模式测试 Server（PublicRegister=true 但新用户 pending）。
func newAuthTestServerNonDemo(t *testing.T) *Server {
	t.Helper()
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3, Demo: false, PublicRegister: true},
		jwtSecret:    []byte("test-jwt-secret-for-auth-test-32bytes!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

// TestAuthRegisterPending 非 demo 模式注册 → 201 + 待审批提示（无 token）。
func TestAuthRegisterPending(t *testing.T) {
	s := newAuthTestServerNonDemo(t)
	body, _ := json.Marshal(map[string]string{"username": "pendinguser", "password": "Pass1234", "email": "p@test.com"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleAuthRegister(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Message string `json:"message"`
		UserID  string `json:"userId"`
		Status  string `json:"status"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if resp.Status != "pending" {
		t.Fatalf("status = %q, want pending", resp.Status)
	}
	if resp.Message == "" {
		t.Fatal("message is empty")
	}
	// 验证用户确实处于 pending 状态。
	u := s.store.GetUserByUsername("pendinguser")
	if u == nil || u.Status != "pending" {
		t.Fatalf("user status = %v, want pending", u)
	}
}

// TestAuthRegisterDemoPending demo 模式但 AllowPublicRegister=false → 201 + 待审批提示（无 token）。
// 验证 P1-7 注册安全修复：demo 模式不再隐式免审批，须显式 --allow-public-register=true 才免审批。
func TestAuthRegisterDemoPending(t *testing.T) {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	s := &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3, Demo: true, PublicRegister: true, AllowPublicRegister: false},
		jwtSecret:    []byte("test-jwt-secret-for-auth-test-32bytes!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
	body, _ := json.Marshal(map[string]string{"username": "demopending", "password": "Pass1234", "email": "dp@test.com"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleAuthRegister(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Message string `json:"message"`
		UserID  string `json:"userId"`
		Status  string `json:"status"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if resp.Status != "pending" {
		t.Fatalf("status = %q, want pending", resp.Status)
	}
	if resp.Message == "" {
		t.Fatal("message is empty")
	}
	// 验证用户确实处于 pending 状态。
	u := s.store.GetUserByUsername("demopending")
	if u == nil || u.Status != "pending" {
		t.Fatalf("user status = %v, want pending", u)
	}
}

// TestAuthRegisterNonDemoAllowPublicRegister 非 demo 模式但 AllowPublicRegister=true → 201 + active + token。
// 验证 AllowPublicRegister 是免审批的唯一控制（与 demo 模式解耦）。
func TestAuthRegisterNonDemoAllowPublicRegister(t *testing.T) {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	s := &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3, Demo: false, PublicRegister: true, AllowPublicRegister: true},
		jwtSecret:    []byte("test-jwt-secret-for-auth-test-32bytes!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
	body, _ := json.Marshal(map[string]string{"username": "allowreg", "password": "Pass1234", "email": "ar@test.com"})
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
	if resp.User == nil || resp.User.Username != "allowreg" {
		t.Fatalf("user = %+v, want username=allowreg", resp.User)
	}
	if resp.User.Status != "active" {
		t.Fatalf("status = %q, want active", resp.User.Status)
	}
}

// TestAuthRegisterPublicDisabled PublicRegister=false → 403。
func TestAuthRegisterPublicDisabled(t *testing.T) {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	s := &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3, Demo: false, PublicRegister: false},
		jwtSecret:    []byte("test-jwt-secret-for-auth-test-32bytes!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
	body, _ := json.Marshal(map[string]string{"username": "newuser", "password": "Pass1234"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleAuthRegister(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("register with public disabled = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

// TestAuthLoginPending pending 用户登录 → 403 待审批提示。
func TestAuthLoginPending(t *testing.T) {
	s := newAuthTestServerNonDemo(t)
	// 注册一个 pending 用户。
	regBody, _ := json.Marshal(map[string]string{"username": "pendinglogin", "password": "Pass1234"})
	regReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(regBody))
	regRec := httptest.NewRecorder()
	s.handleAuthRegister(regRec, regReq)
	if regRec.Code != http.StatusCreated {
		t.Fatalf("setup register failed: %d %s", regRec.Code, regRec.Body.String())
	}
	// pending 用户尝试登录 → 403。
	loginBody, _ := json.Marshal(map[string]string{"username": "pendinglogin", "password": "Pass1234"})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(loginBody))
	loginRec := httptest.NewRecorder()
	s.handleAuthLogin(loginRec, loginReq)
	if loginRec.Code != http.StatusForbidden {
		t.Fatalf("pending login = %d, want 403; body=%s", loginRec.Code, loginRec.Body.String())
	}
}

// TestApproveUser 管理员审批 pending 用户 → 200 + active。
func TestApproveUser(t *testing.T) {
	s := newAuthTestServerNonDemo(t)
	auth := loginAsAdmin(t, s)
	// 注册一个 pending 用户。
	regBody, _ := json.Marshal(map[string]string{"username": "toapprove", "password": "Pass1234"})
	regReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(regBody))
	regRec := httptest.NewRecorder()
	s.handleAuthRegister(regRec, regReq)
	if regRec.Code != http.StatusCreated {
		t.Fatalf("setup register failed: %d %s", regRec.Code, regRec.Body.String())
	}
	pendingUser := s.store.GetUserByUsername("toapprove")
	if pendingUser == nil {
		t.Fatal("pending user not found")
	}
	// 管理员审批。
	req := doWithAuth(http.MethodPost, "/api/v1/users/"+pendingUser.ID+"/approve", auth, nil)
	rec := httptest.NewRecorder()
	s.handleUserRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var u store.User
	if err := json.NewDecoder(rec.Body).Decode(&u); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if u.Status != "active" {
		t.Fatalf("status = %q, want active", u.Status)
	}
	// 审批后应能登录。
	loginBody, _ := json.Marshal(map[string]string{"username": "toapprove", "password": "Pass1234"})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(loginBody))
	loginRec := httptest.NewRecorder()
	s.handleAuthLogin(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login after approve = %d, want 200; body=%s", loginRec.Code, loginRec.Body.String())
	}
}

// TestRejectUser 管理员拒绝 pending 用户 → 200 + rejected。
func TestRejectUser(t *testing.T) {
	s := newAuthTestServerNonDemo(t)
	auth := loginAsAdmin(t, s)
	// 注册一个 pending 用户。
	regBody, _ := json.Marshal(map[string]string{"username": "toreject", "password": "Pass1234"})
	regReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(regBody))
	regRec := httptest.NewRecorder()
	s.handleAuthRegister(regRec, regReq)
	if regRec.Code != http.StatusCreated {
		t.Fatalf("setup register failed: %d %s", regRec.Code, regRec.Body.String())
	}
	pendingUser := s.store.GetUserByUsername("toreject")
	if pendingUser == nil {
		t.Fatal("pending user not found")
	}
	// 管理员拒绝。
	req := doWithAuth(http.MethodPost, "/api/v1/users/"+pendingUser.ID+"/reject", auth, map[string]string{"reason": "测试拒绝"})
	rec := httptest.NewRecorder()
	s.handleUserRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("reject = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var u store.User
	if err := json.NewDecoder(rec.Body).Decode(&u); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if u.Status != "rejected" {
		t.Fatalf("status = %q, want rejected", u.Status)
	}
	// 拒绝后登录 → 403。
	loginBody, _ := json.Marshal(map[string]string{"username": "toreject", "password": "Pass1234"})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(loginBody))
	loginRec := httptest.NewRecorder()
	s.handleAuthLogin(loginRec, loginReq)
	if loginRec.Code != http.StatusForbidden {
		t.Fatalf("login after reject = %d, want 403; body=%s", loginRec.Code, loginRec.Body.String())
	}
}

// TestApproveUserNoPermission 无 user:approve 权限 → 403。
func TestApproveUserNoPermission(t *testing.T) {
	s := newAuthTestServerNonDemo(t)
	// operator 登录（无 user:approve 权限）。
	clearMustChangeFlag(s, "operator")
	body, _ := json.Marshal(map[string]string{"username": "operator", "password": "operator123"})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	loginRec := httptest.NewRecorder()
	s.handleAuthLogin(loginRec, loginReq)
	var loginResp authResponse
	_ = json.NewDecoder(loginRec.Body).Decode(&loginResp)
	auth := "Bearer " + loginResp.Token
	// 任意用户 ID 尝试审批。
	req := doWithAuth(http.MethodPost, "/api/v1/users/user-admin/approve", auth, nil)
	rec := httptest.NewRecorder()
	s.handleUserRouting(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("operator approve = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

// ----------------------------------------------------------------------------
// 安全债 85：预置弱口令强制改密（mustChangePassword 标记 + change-password API）
// ----------------------------------------------------------------------------

// TestPresetUsersMustChangePassword 验证预置 admin/operator/viewer 都带 mustChangePassword=true。
func TestPresetUsersMustChangePassword(t *testing.T) {
	s := newAuthTestServer(t)
	for _, username := range []string{"admin", "operator", "viewer"} {
		u := s.store.GetUserByUsername(username)
		if u == nil {
			t.Fatalf("preset user %s not found", username)
		}
		if !u.MustChangePassword {
			t.Fatalf("preset user %s mustChangePassword = false, want true", username)
		}
	}
}

// TestLoginReturnsMustChangePassword 验证登录响应中返回 mustChangePassword=true。
func TestLoginReturnsMustChangePassword(t *testing.T) {
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
		t.Fatalf("decode login resp: %v", err)
	}
	if !resp.MustChangePassword {
		t.Fatal("login response mustChangePassword = false, want true")
	}
}

// TestChangePasswordSuccess 验证改密成功后 mustChangePassword 标记被清除。
func TestChangePasswordSuccess(t *testing.T) {
	s := newAuthTestServer(t)
	auth := loginAsAdmin(t, s)
	// 改密：admin123 → Admin@2025（满足强度：8 字符 + 大小写 + 数字）。
	req := doWithAuth(http.MethodPost, "/api/v1/auth/change-password", auth, map[string]string{"oldPassword": "admin123", "newPassword": "Admin@2025"})
	rec := httptest.NewRecorder()
	s.handleAuthChangePassword(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("change password = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	// 验证标记已清除。
	u := s.store.GetUserByUsername("admin")
	if u.MustChangePassword {
		t.Fatal("after change password, mustChangePassword still true, want false")
	}
	// 验证新密码可登录且响应中 mustChangePassword=false。
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "Admin@2025"})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	s.handleAuthLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login with new password = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp authResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.MustChangePassword {
		t.Fatal("after change password, login response mustChangePassword still true, want false")
	}
}

// TestChangePasswordWrongOld 验证旧密码错误 → 401。
func TestChangePasswordWrongOld(t *testing.T) {
	s := newAuthTestServer(t)
	auth := loginAsAdmin(t, s)
	req := doWithAuth(http.MethodPost, "/api/v1/auth/change-password", auth, map[string]string{"oldPassword": "wrong-old-pwd", "newPassword": "Admin@2025"})
	rec := httptest.NewRecorder()
	s.handleAuthChangePassword(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("change password with wrong old = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

// TestChangePasswordWeakNew 验证新密码强度不足 → 400。
func TestChangePasswordWeakNew(t *testing.T) {
	s := newAuthTestServer(t)
	auth := loginAsAdmin(t, s)
	cases := []struct {
		name, newPwd string
	}{
		{"too short", "Ab1"},
		{"no uppercase", "admin2025"},
		{"no lowercase", "ADMIN2025"},
		{"no digit", "AdminPassword"},
		{"same as old", "admin123"},
	}
	for _, c := range cases {
		s.loginGuard = newLoginGuard(s.sessionStore) // 重置限流器，避免连续请求被 IP 令牌桶限流（429）掩盖业务校验结果
		req := doWithAuth(http.MethodPost, "/api/v1/auth/change-password", auth, map[string]string{"oldPassword": "admin123", "newPassword": c.newPwd})
		rec := httptest.NewRecorder()
		s.handleAuthChangePassword(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("change password weak new [%s] = %d, want 400; body=%s", c.name, rec.Code, rec.Body.String())
		}
	}
}

// TestChangePasswordNoToken 验证无 token → 401。
func TestChangePasswordNoToken(t *testing.T) {
	s := newAuthTestServer(t)
	body, _ := json.Marshal(map[string]string{"oldPassword": "admin123", "newPassword": "Admin@2025"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/change-password", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleAuthChangePassword(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("change password without token = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

// ----------------------------------------------------------------------------
// P1 服务端强制改密（requirePermission 拦截未改密用户）
// ----------------------------------------------------------------------------

// TestMustChangePasswordBlocksProtectedAPI 验证未改密用户（mustChangePassword=true）登录时
// 不签发 access token（任务 96 安全核心：弱口令用户无法持有效 at 访问受保护 API）。
// 仅签发一次性 changePasswordToken，受保护 API 因无 at 自然被 401 拦截。
func TestMustChangePasswordBlocksProtectedAPI(t *testing.T) {
	s := newAuthTestServer(t)
	// 直接登录预设 admin（不改密，mustChangePassword=true）；不走 loginAsAdmin（后者会清标）。
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "admin123"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleAuthLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp authResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	// 任务 96：mustChangePassword=true 时不签发 access token。
	if resp.Token != "" {
		t.Fatalf("must-change user should not receive access token; got token=%q", resp.Token)
	}
	if resp.ChangePasswordToken == "" {
		t.Fatal("must-change user should receive changePasswordToken")
	}
	// 无 access token 访问受保护 API → 401（empty bearer token），天然拦截。
	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	listRec := httptest.NewRecorder()
	s.handleListUsers(listRec, listReq)
	if listRec.Code != http.StatusUnauthorized {
		t.Fatalf("protected API without token = %d, want 401; body=%s", listRec.Code, listRec.Body.String())
	}
}

// TestMustChangePasswordCanChangeThenAccess 验证未改密用户改密后受保护 API 立即可用。
// 流程（任务 96）：登录获取 changePasswordToken → 用 cpt 改密 → 改密响应返回正式 at → 用 at 访问受保护 API。
func TestMustChangePasswordCanChangeThenAccess(t *testing.T) {
	s := newAuthTestServer(t)
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "admin123"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleAuthLogin(rec, req)
	var resp authResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	// 改密（用 changePasswordToken 鉴权，old=admin123, new=Admin@2025 满足强口令）。
	cpReq := doWithAuth(http.MethodPost, "/api/v1/auth/change-password", "", map[string]string{
		"oldPassword":         "admin123",
		"newPassword":         "Admin@2025",
		"changePasswordToken": resp.ChangePasswordToken,
	})
	cpRec := httptest.NewRecorder()
	s.handleAuthChangePassword(cpRec, cpReq)
	if cpRec.Code != http.StatusOK {
		t.Fatalf("change password = %d, want 200; body=%s", cpRec.Code, cpRec.Body.String())
	}
	// 改密成功后签发正式 access token。
	var cpResp authResponse
	if err := json.NewDecoder(cpRec.Body).Decode(&cpResp); err != nil {
		t.Fatalf("decode change-password resp: %v", err)
	}
	if cpResp.Token == "" {
		t.Fatal("change password response token empty")
	}
	auth := "Bearer " + cpResp.Token
	// 改密后受保护 API 可用（401 → 200）。
	listReq := doWithAuth(http.MethodGet, "/api/v1/users", auth, nil)
	listRec := httptest.NewRecorder()
	s.handleListUsers(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("protected API after change = %d, want 200; body=%s", listRec.Code, listRec.Body.String())
	}
}

// TestUserFromTokenRevokesNonActive 验证被禁用用户的既有有效签名 token 立即失效（P1 吊销）：
// 管理员禁用账号后无需等待 24h 过期即收回访问。
func TestUserFromTokenRevokesNonActive(t *testing.T) {
	s := newAuthTestServer(t)
	// 取预置 viewer（active），置为 disabled 后签发 token，再经 userFromToken 验签应被拒。
	v := s.store.GetUserByUsername("viewer")
	if v == nil {
		t.Fatal("preset viewer not found")
	}
	v.Status = "disabled"
	if !s.store.UpdateUser(v) {
		t.Fatal("update viewer status failed")
	}
	token, err := s.issueUserToken(v)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if _, uerr := s.userFromToken(req); uerr == nil {
		t.Fatal("disabled user token should be rejected by userFromToken")
	}
}

// =============================================================================
// task 94：HttpOnly Cookie 会话（JWT 存储加固）
// =============================================================================

// TestAuthLogin_SetsHttpOnlyCookie 验证登录成功时下发 HttpOnly Cookie（opsmesh_token）。
// 安全债 85 + 任务 96：预置 admin 带 mustChangePassword=true 时不签发 token/Cookie。
// 此用例验证"已改密用户登录下发 Cookie"，先清标模拟改密后状态。
func TestAuthLogin_SetsHttpOnlyCookie(t *testing.T) {
	s := newAuthTestServer(t)
	clearMustChangeFlag(s, "admin")
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "admin123"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleAuthLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	var ck *http.Cookie
	for _, c := range cookies {
		if c.Name == accessTokenCookieName {
			ck = c
			break
		}
	}
	if ck == nil {
		t.Fatalf("登录响应应包含 %s Cookie；实际 cookies=%+v", accessTokenCookieName, cookies)
	}
	if !ck.HttpOnly {
		t.Error("会话 Cookie 必须是 HttpOnly（防 XSS 窃取）")
	}
	if ck.Value == "" {
		t.Error("会话 Cookie 值不应为空")
	}
}

// TestAuthMe_CookieFallback 验证无 Authorization 头时靠 Cookie 恢复会话（刷新场景）。
func TestAuthMe_CookieFallback(t *testing.T) {
	s := newAuthTestServer(t)
	auth := loginAsAdmin(t, s) // "Bearer <token>"
	token := auth[len("Bearer "):]
	// 仅携带 Cookie，不带 Authorization 头。
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: accessTokenCookieName, Value: token})
	rec := httptest.NewRecorder()
	s.handleAuthMe(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("仅 Cookie 的 /auth/me = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// TestAuthMe_NoCookieNoHeader 验证既无头也无 Cookie 时返回 401。
func TestAuthMe_NoCookieNoHeader(t *testing.T) {
	s := newAuthTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	rec := httptest.NewRecorder()
	s.handleAuthMe(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("无凭据 /auth/me = %d, want 401", rec.Code)
	}
}

// TestAuthLogout_ClearsCookie 验证登出清除会话 Cookie。
func TestAuthLogout_ClearsCookie(t *testing.T) {
	s := newAuthTestServer(t)
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", auth)
	rec := httptest.NewRecorder()
	s.handleAuthLogout(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var ck *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == accessTokenCookieName {
			ck = c
			break
		}
	}
	if ck == nil {
		t.Fatalf("登出响应应包含清除 %s Cookie 的 Set-Cookie", accessTokenCookieName)
	}
	if ck.MaxAge != -1 && ck.MaxAge != 0 {
		t.Errorf("登出 Cookie MaxAge 应为负值（立即过期），得到 %d", ck.MaxAge)
	}
}

// ============================================================================
// C-4 DeviceFP deadline 测试：超过 deadline 后签发的 refresh token 必须绑定 DeviceFP（非空）。
// ============================================================================

// TestConsumeRefreshToken_DeviceFPDeadlineNotEnforced 验证 deadline 零值时不强制 DeviceFP（向后兼容）。
func TestConsumeRefreshToken_DeviceFPDeadlineNotEnforced(t *testing.T) {
	s := newAuthTestServer(t)
	// deviceFPDeadline 零值（默认）：不强制 DeviceFP。

	// 创建不带 DeviceFP 的 refresh token（模拟旧客户端）。
	rt, err := s.createRefreshToken("user-1", "")
	if err != nil {
		t.Fatalf("创建 refresh token 失败: %v", err)
	}

	// 消费应成功（deadline 零值时不强制）。
	if _, ok := s.consumeRefreshToken(rt, ""); !ok {
		t.Fatal("deadline 零值时，DeviceFP 为空的 token 应可消费（向后兼容）")
	}
}

// TestConsumeRefreshToken_DeviceFPDeadlineEnforced 验证超过 deadline 后 DeviceFP 为空被拒绝。
func TestConsumeRefreshToken_DeviceFPDeadlineEnforced(t *testing.T) {
	s := newAuthTestServer(t)
	// 设置 deadline 为过去时间：所有新签发的 token 都受 deadline 约束。
	s.deviceFPDeadline = time.Now().Add(-time.Hour)

	// 创建不带 DeviceFP 的 refresh token（模拟旧客户端，但 deadline 已过）。
	rt, err := s.createRefreshToken("user-1", "")
	if err != nil {
		t.Fatalf("创建 refresh token 失败: %v", err)
	}

	// 消费应失败（deadline 后签发的 token 必须 DeviceFP 非空）。
	if _, ok := s.consumeRefreshToken(rt, ""); ok {
		t.Fatal("deadline 后签发的 token DeviceFP 为空应被拒绝")
	}
}

// TestConsumeRefreshToken_DeviceFPDeadlineWithFP 验证超过 deadline 后带 DeviceFP 的 token 正常工作。
func TestConsumeRefreshToken_DeviceFPDeadlineWithFP(t *testing.T) {
	s := newAuthTestServer(t)
	// 设置 deadline 为过去时间：所有新签发的 token 都受 deadline 约束。
	s.deviceFPDeadline = time.Now().Add(-time.Hour)

	// 创建带 DeviceFP 的 refresh token（新客户端）。
	rt, err := s.createRefreshToken("user-1", "device-fp-123")
	if err != nil {
		t.Fatalf("创建 refresh token 失败: %v", err)
	}

	// 消费应成功（DeviceFP 非空且匹配）。
	if sess, ok := s.consumeRefreshToken(rt, "device-fp-123"); !ok || sess == nil {
		t.Fatal("deadline 后签发的 token DeviceFP 非空且匹配应可消费")
	}
}

// TestConsumeRefreshToken_DeviceFPDeadlineBefore 验证 deadline 前签发的 token 不受约束。
func TestConsumeRefreshToken_DeviceFPDeadlineBefore(t *testing.T) {
	s := newAuthTestServer(t)

	// 先创建不带 DeviceFP 的 refresh token（模拟 deadline 前签发的旧 token）。
	rt, err := s.createRefreshToken("user-1", "")
	if err != nil {
		t.Fatalf("创建 refresh token 失败: %v", err)
	}

	// 然后设置 deadline 为未来时间（token 的 CreatedAt 在 deadline 之前）。
	s.deviceFPDeadline = time.Now().Add(time.Hour)

	// 消费应成功（token 在 deadline 前签发，不受约束）。
	if _, ok := s.consumeRefreshToken(rt, ""); !ok {
		t.Fatal("deadline 前签发的 token 应不受 deadline 约束（向后兼容）")
	}
}

// ============================================================================
// B-6 SessionStore 集成测试：验证 Server 通过 SessionStore 接口操作黑名单/改密令牌。
// ============================================================================

// TestAuthLogout_TokenRevokedViaSessionStore 验证登出后 token 经 SessionStore 黑名单拒绝。
func TestAuthLogout_TokenRevokedViaSessionStore(t *testing.T) {
	s := newAuthTestServer(t)
	auth := loginAsAdmin(t, s)

	// 登出。
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", auth)
	rec := httptest.NewRecorder()
	s.handleAuthLogout(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// 登出后用同一 token 访问 /auth/me 应被拒绝（token has been revoked）。
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req2.Header.Set("Authorization", auth)
	rec2 := httptest.NewRecorder()
	s.handleAuthMe(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("登出后 token 应被拒绝，得到 %d，want 401; body=%s", rec2.Code, rec2.Body.String())
	}
}

// TestChangePasswordTokenViaSessionStore 验证改密令牌经 SessionStore 一次性消费。
func TestChangePasswordTokenViaSessionStore(t *testing.T) {
	s := newAuthTestServer(t)

	// 创建改密令牌。
	cpt, err := s.createChangePasswordToken("user-1")
	if err != nil {
		t.Fatalf("创建改密令牌失败: %v", err)
	}

	// 首次消费应成功。
	userID, ok := s.consumeChangePasswordToken(cpt)
	if !ok || userID != "user-1" {
		t.Fatalf("首次消费应返回 (user-1, true)，得到 (%s, %v)", userID, ok)
	}

	// 重复消费应失败（一次性）。
	if _, ok := s.consumeChangePasswordToken(cpt); ok {
		t.Fatal("改密令牌不应被重复消费")
	}
}
