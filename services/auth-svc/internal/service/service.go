package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	authv1 "github.com/Levango7/OpsMesh/services/auth-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/auth"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/store"
)

// Errors returned by the service.
var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserNotFound       = errors.New("user not found")
	ErrUserExists         = errors.New("user already exists")
	ErrRoleNotFound       = errors.New("role not found")
	ErrRoleExists         = errors.New("role already exists")
	ErrTokenInvalid       = errors.New("token is invalid or expired")
	ErrTokenRevoked       = errors.New("token has been revoked")
	ErrPasswordMismatch   = errors.New("old password does not match")
)

// Service implements the auth service business logic.
type Service struct {
	jwtEngine *auth.Engine
	store     store.Store
}

// NewService creates a new Service.
func NewService(engine *auth.Engine, st store.Store) *Service {
	return &Service{
		jwtEngine: engine,
		store:     st,
	}
}

// Store 暴露内部 store 供 HTTP 网关使用（注册审批的 pending 覆盖/用户详情直读）。
// 网关只做读+状态字段写，业务逻辑仍走 service 方法——暴露 store 是受控妥协
// （审批流需要 UpdateUser 细粒度字段控制，proto 未覆盖该语义）。
func (s *Service) Store() store.Store {
	return s.store
}

// changePasswordTokenTTL is the TTL for the short-lived change-password token.
// mustChangePassword=true 用户登录时不签发常规全量 token，仅签发此短时效 token（5min），
// 语义对齐 internal/controlplane/auth_login.go 的 internal 轨实现（changePasswordTokenExpiry）。
const changePasswordTokenTTL = 5 * time.Minute

// Login authenticates a user and returns tokens.
//
// 安全语义（Critical 修复，双轨安全漂移消除）：mustChangePassword=true 的用户
// （如 admin/admin123 首登）密码校验通过后【不】签发常规全量 token——否则弱口令
// 用户持有效 access token 可直接访问全部受保护 API，与 internal 轨
// （internal/controlplane/auth_login.go）的强制改密语义漂移。
// 改为签发 5min 短时效改密专用 token（IssueTokenWithTTL），响应标记
// MustChangePassword=true 并附 ChangePasswordToken；改密成功（须重新 Login）
// 后才签发正式 at+rt。
func (s *Service) Login(ctx context.Context, req *authv1.LoginRequest) (*authv1.TokenResponse, error) {
	u := s.store.GetUserByUsername(req.Username)
	if u == nil {
		return nil, ErrInvalidCredentials
	}
	if u.Status != "active" {
		return nil, ErrInvalidCredentials
	}
	if !auth.VerifyPassword(u.PasswordHash, req.Password) {
		return nil, ErrInvalidCredentials
	}
	// 首登强制改密：仅签 5min 改密专用 token，不签常规 at+rt（防弱口令直取全量权限）。
	if u.MustChangePassword {
		return s.issueChangePasswordTokens(u)
	}
	return s.issueTokens(u, "")
}

// LoginWithFP 带设备指纹的登录（A1 HTTP 网关专用——gRPC proto 不含 FP 字段，
// HTTP 层从 X-Device-FP 头读取后走本方法；语义与 Login 一致 + FP 绑定 rt）。
func (s *Service) LoginWithFP(ctx context.Context, req *authv1.LoginRequest, deviceFP string) (*authv1.TokenResponse, error) {
	u := s.store.GetUserByUsername(req.Username)
	if u == nil {
		return nil, ErrInvalidCredentials
	}
	if u.Status != "active" {
		return nil, ErrInvalidCredentials
	}
	if !auth.VerifyPassword(u.PasswordHash, req.Password) {
		return nil, ErrInvalidCredentials
	}
	if u.MustChangePassword {
		return s.issueChangePasswordTokens(u)
	}
	return s.issueTokens(u, deviceFP)
}

// issueChangePasswordTokens issues a short-lived change-password-only token set
// for users with MustChangePassword=true. AccessToken 为 5min 短时效 token
// （ExpiresIn 为其剩余秒数），不附 RefreshToken（改密专用会话不可刷新）；
// 客户端应据 MustChangePassword=true 走 ChangePassword 流程后重新 Login。
func (s *Service) issueChangePasswordTokens(u *store.User) (*authv1.TokenResponse, error) {
	permissions := s.expandPermissions(u)
	accessToken, expiresIn, err := s.jwtEngine.IssueTokenWithTTL(u.ID, u.Username, u.RoleIDs, permissions, changePasswordTokenTTL)
	if err != nil {
		return nil, fmt.Errorf("failed to issue change-password token: %w", err)
	}
	return &authv1.TokenResponse{
		AccessToken:         accessToken,
		ExpiresIn:           expiresIn,
		User:                toProtoUser(u),
		MustChangePassword:  true,
		ChangePasswordToken: accessToken,
	}, nil
}

// Logout revokes tokens.
func (s *Service) Logout(ctx context.Context, req *authv1.LogoutRequest) (*emptypb.Empty, error) {
	if req.RefreshToken != "" {
		s.store.DeleteRefreshToken(auth.HashRefreshToken(req.RefreshToken))
	}
	if req.AccessToken != "" {
		claims, err := s.jwtEngine.ValidateToken(req.AccessToken)
		if err == nil && claims.JTI != "" {
			ttl := time.Until(claims.ExpiresAt)
			if ttl > 0 {
				s.store.BlacklistJTI(claims.JTI, ttl)
			}
		}
	}
	return &emptypb.Empty{}, nil
}

// RefreshToken issues a new access token using a refresh token.
// mustChangePassword=true 的用户即使持有效 refresh token 也不签发常规全量 token
// （与 Login 同语义，防止经刷新通道绕过首登强制改密）。
func (s *Service) RefreshToken(ctx context.Context, req *authv1.RefreshTokenRequest) (*authv1.TokenResponse, error) {
	return s.refreshTokenFP(ctx, req.RefreshToken, "")
}

// RefreshTokenWithFP 带设备指纹的刷新（A1 HTTP 网关专用）：FP 非空且与签发时不匹配
// → 拒绝（防 rt 被盗后跨设备重放；空 FP 兼容旧客户端，controlplane 同语义）。
func (s *Service) RefreshTokenWithFP(ctx context.Context, refreshToken, deviceFP string) (*authv1.TokenResponse, error) {
	return s.refreshTokenFP(ctx, refreshToken, deviceFP)
}

func (s *Service) refreshTokenFP(_ context.Context, refreshToken, deviceFP string) (*authv1.TokenResponse, error) {
	tokenHash := auth.HashRefreshToken(refreshToken)
	rt, ok := s.store.ConsumeRefreshToken(tokenHash)
	if !ok {
		return nil, ErrTokenInvalid
	}
	if time.Now().After(rt.ExpiresAt) {
		return nil, ErrTokenInvalid
	}
	// 设备指纹校验（A1）：签发时绑定了非空 FP 且本次携带的 FP 不匹配 → 视为跨设备重放拒绝。
	// 签发时 FP 为空（旧客户端）不校验——与 controlplane 的向后兼容语义一致。
	if rt.DeviceFP != "" && deviceFP != rt.DeviceFP {
		return nil, ErrTokenInvalid
	}
	u := s.store.GetUser(rt.UserID)
	if u == nil || u.Status != "active" {
		return nil, ErrTokenInvalid
	}
	if u.MustChangePassword {
		return nil, ErrTokenInvalid
	}
	return s.issueTokens(u, deviceFP)
}

// ValidateToken validates an access token.
func (s *Service) ValidateToken(ctx context.Context, req *authv1.ValidateTokenRequest) (*authv1.ValidateTokenResponse, error) {
	claims, err := s.jwtEngine.ValidateToken(req.Token)
	if err != nil {
		return nil, ErrTokenInvalid
	}
	if s.store.IsBlacklisted(claims.JTI) {
		return nil, ErrTokenRevoked
	}
	return &authv1.ValidateTokenResponse{
		Valid:       true,
		UserId:      claims.UserID,
		Username:    claims.Username,
		Roles:       claims.Roles,
		Permissions: claims.Permissions,
		TenantId:    claims.TenantID,
		ExpiresAt:   claims.ExpiresAt.Unix(),
	}, nil
}

// CheckPermission checks if the token has the required permission.
func (s *Service) CheckPermission(ctx context.Context, req *authv1.CheckPermissionRequest) (*authv1.CheckPermissionResponse, error) {
	claims, err := s.jwtEngine.ValidateToken(req.Token)
	if err != nil {
		return &authv1.CheckPermissionResponse{Allowed: false}, nil
	}
	if s.store.IsBlacklisted(claims.JTI) {
		return &authv1.CheckPermissionResponse{Allowed: false}, nil
	}
	for _, p := range claims.Permissions {
		if p == req.Permission {
			return &authv1.CheckPermissionResponse{Allowed: true}, nil
		}
	}
	return &authv1.CheckPermissionResponse{Allowed: false}, nil
}

// CreateUser creates a new user.
func (s *Service) CreateUser(ctx context.Context, req *authv1.CreateUserRequest) (*authv1.User, error) {
	if req.Username == "" || req.Password == "" {
		return nil, errors.New("username and password are required")
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}
	_, err = s.store.CreateUser(&store.User{
		Username: req.Username,
		Email:    req.Email,
		RoleIDs:  req.RoleIds,
	})
	if err != nil {
		if err.Error() == "username already exists" {
			return nil, ErrUserExists
		}
		return nil, err
	}
	u := s.store.GetUserByUsername(req.Username)
	// store.CreateUser 不落 PasswordHash（建用户与密码解耦），密码经
	// ChangePassword 二次写入——hash 已在上方算好，此处直接复用。
	s.store.ChangePassword(u.ID, hash)
	return toProtoUser(u), nil
}

// GetUser retrieves a user by ID.
func (s *Service) GetUser(ctx context.Context, req *authv1.GetUserRequest) (*authv1.User, error) {
	u := s.store.GetUser(req.Id)
	if u == nil {
		return nil, ErrUserNotFound
	}
	return toProtoUser(u), nil
}

// UpdateUser updates a user.
func (s *Service) UpdateUser(ctx context.Context, req *authv1.UpdateUserRequest) (*authv1.User, error) {
	err := s.store.UpdateUser(&store.User{
		ID:      req.Id,
		Email:   req.Email,
		Status:  req.Status,
		RoleIDs: req.RoleIds,
	})
	if err != nil {
		return nil, ErrUserNotFound
	}
	u := s.store.GetUser(req.Id)
	return toProtoUser(u), nil
}

// DeleteUser deletes a user.
func (s *Service) DeleteUser(ctx context.Context, req *authv1.DeleteUserRequest) (*emptypb.Empty, error) {
	err := s.store.DeleteUser(req.Id)
	if err != nil {
		return nil, ErrUserNotFound
	}
	return &emptypb.Empty{}, nil
}

// ListUsers lists all users.
func (s *Service) ListUsers(ctx context.Context, _ *emptypb.Empty) (*authv1.ListUsersResponse, error) {
	users := s.store.ListUsers()
	out := make([]*authv1.User, 0, len(users))
	for _, u := range users {
		out = append(out, toProtoUser(u))
	}
	return &authv1.ListUsersResponse{Users: out}, nil
}

// ChangePassword changes a user's password.
func (s *Service) ChangePassword(ctx context.Context, req *authv1.ChangePasswordRequest) (*emptypb.Empty, error) {
	u := s.store.GetUser(req.UserId)
	if u == nil {
		return nil, ErrUserNotFound
	}
	if !auth.VerifyPassword(u.PasswordHash, req.OldPassword) {
		return nil, ErrPasswordMismatch
	}
	newHash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}
	err = s.store.ChangePassword(req.UserId, newHash)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// CreateRole creates a new role.
func (s *Service) CreateRole(ctx context.Context, req *authv1.CreateRoleRequest) (*authv1.Role, error) {
	if req.Name == "" {
		return nil, errors.New("role name is required")
	}
	_, err := s.store.CreateRole(&store.Role{
		Name:        req.Name,
		Description: req.Description,
		Permissions: req.Permissions,
	})
	if err != nil {
		if err.Error() == "role name already exists" {
			return nil, ErrRoleExists
		}
		return nil, err
	}
	r := s.store.GetRoleByName(req.Name)
	return &authv1.Role{
		Id:          r.ID,
		Name:        r.Name,
		Description: r.Description,
		Permissions: r.Permissions,
		CreatedAt:   timestamppb.New(r.CreatedAt),
	}, nil
}

// GetRole retrieves a role by ID.
func (s *Service) GetRole(ctx context.Context, req *authv1.GetRoleRequest) (*authv1.Role, error) {
	r := s.store.GetRole(req.Id)
	if r == nil {
		return nil, ErrRoleNotFound
	}
	return &authv1.Role{
		Id:          r.ID,
		Name:        r.Name,
		Description: r.Description,
		Permissions: r.Permissions,
		CreatedAt:   timestamppb.New(r.CreatedAt),
	}, nil
}

// UpdateRole updates a role.
func (s *Service) UpdateRole(ctx context.Context, req *authv1.UpdateRoleRequest) (*authv1.Role, error) {
	err := s.store.UpdateRole(&store.Role{
		ID:          req.Id,
		Description: req.Description,
		Permissions: req.Permissions,
	})
	if err != nil {
		return nil, ErrRoleNotFound
	}
	r := s.store.GetRole(req.Id)
	return &authv1.Role{
		Id:          r.ID,
		Name:        r.Name,
		Description: r.Description,
		Permissions: r.Permissions,
		CreatedAt:   timestamppb.New(r.CreatedAt),
	}, nil
}

// DeleteRole deletes a role.
func (s *Service) DeleteRole(ctx context.Context, req *authv1.DeleteRoleRequest) (*emptypb.Empty, error) {
	err := s.store.DeleteRole(req.Id)
	if err != nil {
		return nil, ErrRoleNotFound
	}
	return &emptypb.Empty{}, nil
}

// ListRoles lists all roles.
func (s *Service) ListRoles(ctx context.Context, _ *emptypb.Empty) (*authv1.ListRolesResponse, error) {
	roles := s.store.ListRoles()
	out := make([]*authv1.Role, 0, len(roles))
	for _, r := range roles {
		out = append(out, &authv1.Role{
			Id:          r.ID,
			Name:        r.Name,
			Description: r.Description,
			Permissions: r.Permissions,
			CreatedAt:   timestamppb.New(r.CreatedAt),
		})
	}
	return &authv1.ListRolesResponse{Roles: out}, nil
}

// AssignRole assigns a role to a user.
func (s *Service) AssignRole(ctx context.Context, req *authv1.AssignRoleRequest) (*emptypb.Empty, error) {
	u := s.store.GetUser(req.UserId)
	if u == nil {
		return nil, ErrUserNotFound
	}
	r := s.store.GetRole(req.RoleId)
	if r == nil {
		return nil, ErrRoleNotFound
	}
	found := false
	for _, rid := range u.RoleIDs {
		if rid == req.RoleId {
			found = true
			break
		}
	}
	if !found {
		u.RoleIDs = append(u.RoleIDs, req.RoleId)
	}
	err := s.store.UpdateUser(u)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// ListPermissions lists all permissions.
func (s *Service) ListPermissions(ctx context.Context, _ *emptypb.Empty) (*authv1.ListPermissionsResponse, error) {
	perms := s.store.ListPermissions()
	out := make([]*authv1.Permission, 0, len(perms))
	for _, p := range perms {
		out = append(out, &authv1.Permission{
			Id:          p.ID,
			Name:        p.Name,
			Description: p.Description,
			Group:       p.Group,
		})
	}
	return &authv1.ListPermissionsResponse{Permissions: out}, nil
}

// issueTokens issues access and refresh tokens for a user.
// deviceFP 为设备指纹（A1 安全对齐 controlplane）：签发 rt 时绑定——非空时刷新必须
// 匹配（防 token 被盗后跨设备重放）；空=旧客户端兼容不校验（controlplane 同语义）。
func (s *Service) issueTokens(u *store.User, deviceFP string) (*authv1.TokenResponse, error) {
	permissions := s.expandPermissions(u)
	accessToken, expiresIn, err := s.jwtEngine.IssueToken(u.ID, u.Username, u.RoleIDs, permissions)
	if err != nil {
		return nil, fmt.Errorf("failed to issue token: %w", err)
	}
	refreshToken, err := s.jwtEngine.IssueRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("failed to issue refresh token: %w", err)
	}
	s.store.SaveRefreshToken(&store.RefreshToken{
		TokenHash: auth.HashRefreshToken(refreshToken),
		UserID:    u.ID,
		TenantID:  "default",
		DeviceFP:  deviceFP,
		ExpiresAt: time.Now().Add(s.jwtEngine.RefreshTokenTTL()),
		CreatedAt: time.Now(),
	})
	return &authv1.TokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    expiresIn,
		User:         toProtoUser(u),
	}, nil
}

// expandPermissions expands role IDs to permission strings.
func (s *Service) expandPermissions(u *store.User) []string {
	seen := make(map[string]bool)
	var out []string
	for _, rid := range u.RoleIDs {
		r := s.store.GetRole(rid)
		if r == nil {
			continue
		}
		for _, p := range r.Permissions {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// toProtoUser converts a store user to proto user.
func toProtoUser(u *store.User) *authv1.User {
	if u == nil {
		return nil
	}
	return &authv1.User{
		Id:                 u.ID,
		Username:           u.Username,
		Email:              u.Email,
		Status:             u.Status,
		RoleIds:            u.RoleIDs,
		CreatedAt:          timestamppb.New(u.CreatedAt),
		MustChangePassword: u.MustChangePassword,
	}
}
