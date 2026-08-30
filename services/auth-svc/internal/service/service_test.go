package service

import (
	"context"
	"testing"
	"time"

	authv1 "github.com/Levango7/OpsMesh/services/auth-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/auth"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/store"
	"google.golang.org/protobuf/types/known/emptypb"
)

func newTestService() *Service {
	eng := auth.NewEngine("test-secret", 15*60*1000000000, 7*24*60*60*1000000000)
	st := store.NewMemoryStore()
	return NewService(eng, st)
}

// mustClearChangePassword 模拟"admin 已完成首登改密"的常规用户状态：
// 传入的 Service 内置 store 的 seed admin 带 MustChangePassword=true（安全基线），
// 常规登录/刷新流程测试须先把该标记清掉（经 ChangePassword 正规路径改密），
// 与生产语义一致（强制改密用户不走常规 at+rt 流程）。
func mustClearChangePassword(t *testing.T, svc *Service) {
	t.Helper()
	hash, err := auth.HashPassword("admin123")
	if err != nil {
		t.Fatalf("hash password failed: %v", err)
	}
	if err := svc.store.ChangePassword("user-admin", hash); err != nil {
		t.Fatalf("clear must-change-password failed: %v", err)
	}
}

func TestLogin(t *testing.T) {
	svc := newTestService()
	mustClearChangePassword(t, svc)
	ctx := context.Background()

	resp, err := svc.Login(ctx, &authv1.LoginRequest{
		Username: "admin",
		Password: "admin123",
	})
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if resp.AccessToken == "" {
		t.Error("expected access token to be set")
	}
	if resp.RefreshToken == "" {
		t.Error("expected refresh token to be set")
	}
	if resp.User == nil {
		t.Error("expected user to be set")
	}
}

// TestLogin_MustChangePassword 验证首登强制改密语义（安全基线回归）：
// mustChangePassword=true 用户密码校验通过后【不】签发常规全量 at+rt，
// 仅返回 5min 改密专用 token（MustChangePassword=true 标记），
// 改密成功后重新 Login 才签发正式 token（对齐 internal 轨 auth_login.go）。
func TestLogin_MustChangePassword(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	resp, err := svc.Login(ctx, &authv1.LoginRequest{
		Username: "admin",
		Password: "admin123",
	})
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if !resp.MustChangePassword {
		t.Error("expected MustChangePassword to be true for seeded admin")
	}
	if resp.ChangePasswordToken == "" {
		t.Error("expected change password token to be set")
	}
	if resp.RefreshToken != "" {
		t.Error("mustChangePassword login must not issue refresh token (got one)")
	}
	// 改密专用 token 应为短时效（5min = 300s），非常规 access TTL（900s）。
	if resp.ExpiresIn > 300 {
		t.Errorf("change-password token TTL should be <= 300s (5min), got %d", resp.ExpiresIn)
	}
	// 改密成功后重新 Login 应签发常规全量 token。
	if _, err := svc.ChangePassword(ctx, &authv1.ChangePasswordRequest{
		UserId:      "user-admin",
		OldPassword: "admin123",
		NewPassword: "new-strong-pass-123",
	}); err != nil {
		t.Fatalf("ChangePassword failed: %v", err)
	}
	resp2, err := svc.Login(ctx, &authv1.LoginRequest{
		Username: "admin",
		Password: "new-strong-pass-123",
	})
	if err != nil {
		t.Fatalf("Login after change password failed: %v", err)
	}
	if resp2.MustChangePassword {
		t.Error("MustChangePassword should be cleared after password change")
	}
	if resp2.AccessToken == "" || resp2.RefreshToken == "" {
		t.Error("expected full token set after password change")
	}
}

func TestLoginInvalidPassword(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.Login(ctx, &authv1.LoginRequest{
		Username: "admin",
		Password: "wrongpassword",
	})
	if err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got: %v", err)
	}
}

func TestLoginNonExistentUser(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.Login(ctx, &authv1.LoginRequest{
		Username: "nonexistent",
		Password: "password",
	})
	if err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got: %v", err)
	}
}

func TestValidateToken(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	resp, err := svc.Login(ctx, &authv1.LoginRequest{
		Username: "admin",
		Password: "admin123",
	})
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	vresp, err := svc.ValidateToken(ctx, &authv1.ValidateTokenRequest{
		Token: resp.AccessToken,
	})
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}
	if !vresp.Valid {
		t.Error("expected token to be valid")
	}
	if vresp.Username != "admin" {
		t.Errorf("expected username admin, got %s", vresp.Username)
	}
}

func TestValidateTokenInvalid(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.ValidateToken(ctx, &authv1.ValidateTokenRequest{
		Token: "invalid-token",
	})
	if err != ErrTokenInvalid {
		t.Fatalf("expected ErrTokenInvalid, got: %v", err)
	}
}

func TestCheckPermission(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	resp, err := svc.Login(ctx, &authv1.LoginRequest{
		Username: "admin",
		Password: "admin123",
	})
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	cresp, err := svc.CheckPermission(ctx, &authv1.CheckPermissionRequest{
		Token:      resp.AccessToken,
		Permission: "user:read",
	})
	if err != nil {
		t.Fatalf("CheckPermission failed: %v", err)
	}
	if !cresp.Allowed {
		t.Error("expected permission to be granted for admin")
	}
}

func TestCheckPermissionDenied(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	resp, err := svc.Login(ctx, &authv1.LoginRequest{
		Username: "admin",
		Password: "admin123",
	})
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	cresp, err := svc.CheckPermission(ctx, &authv1.CheckPermissionRequest{
		Token:      resp.AccessToken,
		Permission: "nonexistent:permission",
	})
	if err != nil {
		t.Fatalf("CheckPermission failed: %v", err)
	}
	if cresp.Allowed {
		t.Error("expected permission to be denied")
	}
}

func TestCreateUser(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	user, err := svc.CreateUser(ctx, &authv1.CreateUserRequest{
		Username: "testuser",
		Email:    "test@example.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if user.Username != "testuser" {
		t.Errorf("expected username testuser, got %s", user.Username)
	}
	if user.Email != "test@example.com" {
		t.Errorf("expected email test@example.com, got %s", user.Email)
	}
}

func TestCreateUserDuplicate(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.CreateUser(ctx, &authv1.CreateUserRequest{
		Username: "testuser",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("First CreateUser failed: %v", err)
	}

	_, err = svc.CreateUser(ctx, &authv1.CreateUserRequest{
		Username: "testuser",
		Password: "password456",
	})
	if err != ErrUserExists {
		t.Fatalf("expected ErrUserExists, got: %v", err)
	}
}

func TestGetUser(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.CreateUser(ctx, &authv1.CreateUserRequest{
		Username: "testuser2",
		Email:    "test2@example.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	user, err := svc.GetUser(ctx, &authv1.GetUserRequest{Id: created.Id})
	if err != nil {
		t.Fatalf("GetUser failed: %v", err)
	}
	if user.Username != "testuser2" {
		t.Errorf("expected username testuser2, got %s", user.Username)
	}
}

func TestGetUserNotFound(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.GetUser(ctx, &authv1.GetUserRequest{Id: "nonexistent"})
	if err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound, got: %v", err)
	}
}

func TestListUsers(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := svc.CreateUser(ctx, &authv1.CreateUserRequest{
			Username: "listuser",
			Password: "password123",
		})
		if err != nil {
			// Skip duplicates from previous tests
			continue
		}
	}

	resp, err := svc.ListUsers(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("ListUsers failed: %v", err)
	}
	if len(resp.Users) < 1 {
		t.Error("expected at least 1 user")
	}
}

func TestChangePassword(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.CreateUser(ctx, &authv1.CreateUserRequest{
		Username: "pwduser",
		Password: "oldpassword",
	})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	_, err = svc.ChangePassword(ctx, &authv1.ChangePasswordRequest{
		UserId:      created.Id,
		OldPassword: "oldpassword",
		NewPassword: "newpassword",
	})
	if err != nil {
		t.Fatalf("ChangePassword failed: %v", err)
	}
}

func TestChangePasswordWrongOld(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.CreateUser(ctx, &authv1.CreateUserRequest{
		Username: "pwduser2",
		Password: "oldpassword",
	})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	_, err = svc.ChangePassword(ctx, &authv1.ChangePasswordRequest{
		UserId:      created.Id,
		OldPassword: "wrongpassword",
		NewPassword: "newpassword",
	})
	if err != ErrPasswordMismatch {
		t.Fatalf("expected ErrPasswordMismatch, got: %v", err)
	}
}

func TestCreateRole(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	role, err := svc.CreateRole(ctx, &authv1.CreateRoleRequest{
		Name:        "editor",
		Description: "Editor role",
		Permissions: []string{"user:read", "role:read"},
	})
	if err != nil {
		t.Fatalf("CreateRole failed: %v", err)
	}
	if role.Name != "editor" {
		t.Errorf("expected role name editor, got %s", role.Name)
	}
	if len(role.Permissions) != 2 {
		t.Errorf("expected 2 permissions, got %d", len(role.Permissions))
	}
}

func TestAssignRole(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.CreateUser(ctx, &authv1.CreateUserRequest{
		Username: "roleuser",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	role, err := svc.CreateRole(ctx, &authv1.CreateRoleRequest{
		Name:        "viewer",
		Description: "Viewer role",
		Permissions: []string{"user:read"},
	})
	if err != nil {
		t.Fatalf("CreateRole failed: %v", err)
	}

	_, err = svc.AssignRole(ctx, &authv1.AssignRoleRequest{
		UserId: created.Id,
		RoleId: role.Id,
	})
	if err != nil {
		t.Fatalf("AssignRole failed: %v", err)
	}

	user, err := svc.GetUser(ctx, &authv1.GetUserRequest{Id: created.Id})
	if err != nil {
		t.Fatalf("GetUser failed: %v", err)
	}
	found := false
	for _, rid := range user.RoleIds {
		if rid == role.Id {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected role to be assigned to user")
	}
}

func TestRefreshToken(t *testing.T) {
	svc := newTestService()
	mustClearChangePassword(t, svc)
	ctx := context.Background()

	resp, err := svc.Login(ctx, &authv1.LoginRequest{
		Username: "admin",
		Password: "admin123",
	})
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	newResp, err := svc.RefreshToken(ctx, &authv1.RefreshTokenRequest{
		RefreshToken: resp.RefreshToken,
	})
	if err != nil {
		t.Fatalf("RefreshToken failed: %v", err)
	}
	if newResp.AccessToken == "" {
		t.Error("expected new access token")
	}
	if newResp.RefreshToken == "" {
		t.Error("expected new refresh token")
	}
}

// TestRefreshToken_MustChangePasswordBlocked 验证强制改密用户不能经刷新通道
// 换取常规全量 token（防绕过首登强制改密；mustChangePassword 用户本就不持有
// refresh token，此处防御性验证：即使构造出有效 rt 也拒绝刷新）。
func TestRefreshToken_MustChangePasswordBlocked(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	// mustChangePassword=true 用户登录不签 rt，故手工保存一个 rt 到 store 模拟攻击者
	// 持有该用户 refresh token 的场景（防御性验证 RefreshToken 不放行）。
	refreshToken, err := svc.jwtEngine.IssueRefreshToken()
	if err != nil {
		t.Fatalf("issue refresh token failed: %v", err)
	}
	svc.store.SaveRefreshToken(&store.RefreshToken{
		TokenHash: auth.HashRefreshToken(refreshToken),
		UserID:    "user-admin",
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
	})

	_, err = svc.RefreshToken(ctx, &authv1.RefreshTokenRequest{
		RefreshToken: refreshToken,
	})
	if err != ErrTokenInvalid {
		t.Fatalf("mustChangePassword user refresh should be rejected with ErrTokenInvalid, got: %v", err)
	}
}

func TestLogout(t *testing.T) {
	svc := newTestService()
	mustClearChangePassword(t, svc)
	ctx := context.Background()

	resp, err := svc.Login(ctx, &authv1.LoginRequest{
		Username: "admin",
		Password: "admin123",
	})
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	_, err = svc.Logout(ctx, &authv1.LogoutRequest{
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
	})
	if err != nil {
		t.Fatalf("Logout failed: %v", err)
	}

	_, err = svc.ValidateToken(ctx, &authv1.ValidateTokenRequest{
		Token: resp.AccessToken,
	})
	if err != ErrTokenRevoked {
		t.Fatalf("expected ErrTokenRevoked after logout, got: %v", err)
	}
}
