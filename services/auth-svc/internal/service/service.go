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

// Login authenticates a user and returns tokens.
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
	return s.issueTokens(u)
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
func (s *Service) RefreshToken(ctx context.Context, req *authv1.RefreshTokenRequest) (*authv1.TokenResponse, error) {
	tokenHash := auth.HashRefreshToken(req.RefreshToken)
	rt, ok := s.store.ConsumeRefreshToken(tokenHash)
	if !ok {
		return nil, ErrTokenInvalid
	}
	if time.Now().After(rt.ExpiresAt) {
		return nil, ErrTokenInvalid
	}
	u := s.store.GetUser(rt.UserID)
	if u == nil || u.Status != "active" {
		return nil, ErrTokenInvalid
	}
	return s.issueTokens(u)
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
	_, err := s.store.CreateUser(&store.User{
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
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}
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
func (s *Service) issueTokens(u *store.User) (*authv1.TokenResponse, error) {
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
