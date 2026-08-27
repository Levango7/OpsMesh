package service

import (
	"context"
	"testing"

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

func TestLogin(t *testing.T) {
	svc := newTestService()
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

func TestLogout(t *testing.T) {
	svc := newTestService()
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
