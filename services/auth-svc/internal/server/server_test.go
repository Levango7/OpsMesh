package server

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	authv1 "github.com/Levango7/OpsMesh/services/auth-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/auth"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/store"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	eng := auth.NewEngine("test-secret", 15*time.Minute, 7*24*time.Hour)
	return NewServer(service.NewService(eng, store.NewMemoryStore()))
}

func requireCode(t *testing.T, err error, want codes.Code, what string) {
	t.Helper()
	if got := status.Code(err); got != want {
		t.Fatalf("%s: code = %v, want %v (err=%v)", what, got, want, err)
	}
}

// adminRole 从 ListRoles 里取种子 admin 角色，供 AssignRole 用例使用。
func adminRole(t *testing.T, srv *Server) *authv1.Role {
	t.Helper()
	roles, err := srv.ListRoles(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	for _, r := range roles.GetRoles() {
		if r.GetName() == "admin" {
			return r
		}
	}
	t.Fatal("种子数据里没有 admin 角色")
	return nil
}

func TestAuthServiceMapping(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	_, err := srv.Login(ctx, &authv1.LoginRequest{Username: "admin", Password: "wrong"})
	requireCode(t, err, codes.Unauthenticated, "Login(错误口令)")
	_, err = srv.Login(ctx, &authv1.LoginRequest{Username: "ghost", Password: "x"})
	requireCode(t, err, codes.Unauthenticated, "Login(未知用户)")

	// 种子 admin 是 must_change_password 状态：登录成功但只发 5min 改密专用令牌。
	resp, err := srv.Login(ctx, &authv1.LoginRequest{Username: "admin", Password: "admin123"})
	if err != nil {
		t.Fatalf("Login(admin): %v", err)
	}
	if !resp.GetMustChangePassword() || resp.GetChangePasswordToken() == "" {
		t.Fatalf("Login(admin) = must_change=%v token=%q, want 强制改密载体", resp.GetMustChangePassword(), resp.GetChangePasswordToken())
	}
	if resp.GetRefreshToken() != "" {
		t.Fatalf("首登强制改密不应签发 refresh token, got %q", resp.GetRefreshToken())
	}

	_, err = srv.RefreshToken(ctx, &authv1.RefreshTokenRequest{RefreshToken: "garbage"})
	requireCode(t, err, codes.Unauthenticated, "RefreshToken(非法)")
	_, err = srv.ValidateToken(ctx, &authv1.ValidateTokenRequest{Token: "garbage"})
	requireCode(t, err, codes.Unauthenticated, "ValidateToken(非法)")

	// Logout 对任意输入都应幂等成功（适配层直接透传空响应）。
	if _, err := srv.Logout(ctx, &authv1.LogoutRequest{}); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	// CheckPermission 对非法令牌走业务拒绝而不是 gRPC 错误。
	perm, err := srv.CheckPermission(ctx, &authv1.CheckPermissionRequest{Token: "garbage", Permission: "device:read"})
	if err != nil {
		t.Fatalf("CheckPermission 不应返回 gRPC 错误: %v", err)
	}
	if perm.GetAllowed() {
		t.Fatal("非法令牌不应被放行")
	}

	guard, err := srv.GetLoginGuardStatus(ctx, &authv1.GetLoginGuardStatusRequest{Username: "admin"})
	if err != nil {
		t.Fatalf("GetLoginGuardStatus: %v", err)
	}
	if guard.GetLocked() || guard.GetFailCount() != 0 {
		t.Fatalf("GetLoginGuardStatus = locked=%v fail=%d, want 未锁定/0", guard.GetLocked(), guard.GetFailCount())
	}
}

func TestUserServiceMapping(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	_, err := srv.CreateUser(ctx, &authv1.CreateUserRequest{Username: "admin", Password: "another"})
	requireCode(t, err, codes.AlreadyExists, "CreateUser(重名)")

	created, err := srv.CreateUser(ctx, &authv1.CreateUserRequest{
		Username: "alice", Password: "alice-pass", Email: "alice@example.com",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if created.GetId() == "" || created.GetUsername() != "alice" {
		t.Fatalf("CreateUser = %+v", created)
	}

	_, err = srv.GetUser(ctx, &authv1.GetUserRequest{Id: "missing"})
	requireCode(t, err, codes.NotFound, "GetUser(missing)")
	_, err = srv.UpdateUser(ctx, &authv1.UpdateUserRequest{Id: "missing"})
	requireCode(t, err, codes.NotFound, "UpdateUser(missing)")
	_, err = srv.DeleteUser(ctx, &authv1.DeleteUserRequest{Id: "missing"})
	requireCode(t, err, codes.NotFound, "DeleteUser(missing)")

	// 非强制改密用户直接拿到常规 at+rt，并可被 ValidateToken 验证。
	login, err := srv.Login(ctx, &authv1.LoginRequest{Username: "alice", Password: "alice-pass"})
	if err != nil {
		t.Fatalf("Login(alice): %v", err)
	}
	if login.GetAccessToken() == "" || login.GetRefreshToken() == "" {
		t.Fatalf("Login(alice) 未签发常规 at+rt: %+v", login)
	}
	validated, err := srv.ValidateToken(ctx, &authv1.ValidateTokenRequest{Token: login.GetAccessToken()})
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if !validated.GetValid() || validated.GetUsername() != "alice" {
		t.Fatalf("ValidateToken = valid=%v username=%q", validated.GetValid(), validated.GetUsername())
	}
	perm, err := srv.CheckPermission(ctx, &authv1.CheckPermissionRequest{
		Token: login.GetAccessToken(), Permission: "device:read",
	})
	if err != nil {
		t.Fatalf("CheckPermission: %v", err)
	}
	if perm.GetAllowed() {
		t.Fatal("无角色用户不应拿到 device:read 权限")
	}

	// 改密映射：缺用户 NotFound、旧口令不符 Unauthenticated、成功后旧口令失效。
	_, err = srv.ChangePassword(ctx, &authv1.ChangePasswordRequest{UserId: "missing", NewPassword: "x"})
	requireCode(t, err, codes.NotFound, "ChangePassword(missing)")
	_, err = srv.ChangePassword(ctx, &authv1.ChangePasswordRequest{
		UserId: created.GetId(), OldPassword: "wrong", NewPassword: "alice-pass-2",
	})
	requireCode(t, err, codes.Unauthenticated, "ChangePassword(旧口令不符)")
	if _, err := srv.ChangePassword(ctx, &authv1.ChangePasswordRequest{
		UserId: created.GetId(), OldPassword: "alice-pass", NewPassword: "alice-pass-2",
	}); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	_, err = srv.Login(ctx, &authv1.LoginRequest{Username: "alice", Password: "alice-pass"})
	requireCode(t, err, codes.Unauthenticated, "Login(旧口令已失效)")
	if _, err := srv.Login(ctx, &authv1.LoginRequest{Username: "alice", Password: "alice-pass-2"}); err != nil {
		t.Fatalf("Login(新口令): %v", err)
	}

	updated, err := srv.UpdateUser(ctx, &authv1.UpdateUserRequest{
		Id: created.GetId(), Email: "alice2@example.com", Status: "active",
	})
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if updated.GetEmail() != "alice2@example.com" {
		t.Fatalf("UpdateUser email = %q", updated.GetEmail())
	}

	users, err := srv.ListUsers(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users.GetUsers()) < 2 {
		t.Fatalf("ListUsers = %d 条, want >=2（种子 admin + alice）", len(users.GetUsers()))
	}

	if _, err := srv.DeleteUser(ctx, &authv1.DeleteUserRequest{Id: created.GetId()}); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	_, err = srv.GetUser(ctx, &authv1.GetUserRequest{Id: created.GetId()})
	requireCode(t, err, codes.NotFound, "GetUser(已删除)")
}

func TestRoleServiceMapping(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	role, err := srv.CreateRole(ctx, &authv1.CreateRoleRequest{
		Name: "auditor", Description: "read-only", Permissions: []string{"device:read"},
	})
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if role.GetId() == "" || role.GetName() != "auditor" {
		t.Fatalf("CreateRole = %+v", role)
	}

	_, err = srv.CreateRole(ctx, &authv1.CreateRoleRequest{Name: "auditor"})
	requireCode(t, err, codes.AlreadyExists, "CreateRole(重名)")
	_, err = srv.GetRole(ctx, &authv1.GetRoleRequest{Id: "missing"})
	requireCode(t, err, codes.NotFound, "GetRole(missing)")
	_, err = srv.UpdateRole(ctx, &authv1.UpdateRoleRequest{Id: "missing"})
	requireCode(t, err, codes.NotFound, "UpdateRole(missing)")
	_, err = srv.DeleteRole(ctx, &authv1.DeleteRoleRequest{Id: "missing"})
	requireCode(t, err, codes.NotFound, "DeleteRole(missing)")

	updated, err := srv.UpdateRole(ctx, &authv1.UpdateRoleRequest{
		Id: role.GetId(), Description: "read-only v2", Permissions: []string{"device:read", "alert:read"},
	})
	if err != nil {
		t.Fatalf("UpdateRole: %v", err)
	}
	if updated.GetDescription() != "read-only v2" || len(updated.GetPermissions()) != 2 {
		t.Fatalf("UpdateRole = %+v", updated)
	}

	user, err := srv.CreateUser(ctx, &authv1.CreateUserRequest{Username: "bob", Password: "bob-pass"})
	if err != nil {
		t.Fatalf("CreateUser(bob): %v", err)
	}

	_, err = srv.AssignRole(ctx, &authv1.AssignRoleRequest{UserId: "missing", RoleId: role.GetId()})
	requireCode(t, err, codes.NotFound, "AssignRole(用户不存在)")
	_, err = srv.AssignRole(ctx, &authv1.AssignRoleRequest{UserId: user.GetId(), RoleId: "missing"})
	requireCode(t, err, codes.NotFound, "AssignRole(角色不存在)")
	if _, err := srv.AssignRole(ctx, &authv1.AssignRoleRequest{UserId: user.GetId(), RoleId: role.GetId()}); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	got, err := srv.GetUser(ctx, &authv1.GetUserRequest{Id: user.GetId()})
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if len(got.GetRoleIds()) != 1 || got.GetRoleIds()[0] != role.GetId() {
		t.Fatalf("分配后 RoleIds = %v, want [%s]", got.GetRoleIds(), role.GetId())
	}

	roles, err := srv.ListRoles(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(roles.GetRoles()) < 3 {
		t.Fatalf("ListRoles = %d 条, want >=3（admin/operator/auditor）", len(roles.GetRoles()))
	}
	if adminRole(t, srv).GetId() == "" {
		t.Fatal("种子 admin 角色缺少 ID")
	}

	perms, err := srv.ListPermissions(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("ListPermissions: %v", err)
	}
	if len(perms.GetPermissions()) == 0 {
		t.Fatal("ListPermissions 为空，种子权限目录不应为空")
	}

	if _, err := srv.DeleteRole(ctx, &authv1.DeleteRoleRequest{Id: role.GetId()}); err != nil {
		t.Fatalf("DeleteRole: %v", err)
	}
	_, err = srv.GetRole(ctx, &authv1.GetRoleRequest{Id: role.GetId()})
	requireCode(t, err, codes.NotFound, "GetRole(已删除)")
}
