package server

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	authv1 "github.com/Levango7/OpsMesh/services/auth-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/service"
)

// Server implements the auth gRPC services.
type Server struct {
	authv1.UnimplementedAuthServiceServer
	authv1.UnimplementedUserServiceServer
	authv1.UnimplementedRoleServiceServer
	authv1.UnimplementedPermissionServiceServer
	svc *service.Service
}

// NewServer creates a new gRPC server.
func NewServer(svc *service.Service) *Server {
	return &Server{svc: svc}
}

// --- AuthService ---

// Login authenticates a user.
func (s *Server) Login(ctx context.Context, req *authv1.LoginRequest) (*authv1.TokenResponse, error) {
	resp, err := s.svc.Login(ctx, req)
	if err != nil {
		if err == service.ErrInvalidCredentials {
			return nil, status.Error(codes.Unauthenticated, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return resp, nil
}

// Logout revokes tokens.
func (s *Server) Logout(ctx context.Context, req *authv1.LogoutRequest) (*emptypb.Empty, error) {
	return s.svc.Logout(ctx, req)
}

// RefreshToken refreshes tokens.
func (s *Server) RefreshToken(ctx context.Context, req *authv1.RefreshTokenRequest) (*authv1.TokenResponse, error) {
	resp, err := s.svc.RefreshToken(ctx, req)
	if err != nil {
		if err == service.ErrTokenInvalid {
			return nil, status.Error(codes.Unauthenticated, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return resp, nil
}

// ValidateToken validates a token.
func (s *Server) ValidateToken(ctx context.Context, req *authv1.ValidateTokenRequest) (*authv1.ValidateTokenResponse, error) {
	resp, err := s.svc.ValidateToken(ctx, req)
	if err != nil {
		if err == service.ErrTokenInvalid || err == service.ErrTokenRevoked {
			return nil, status.Error(codes.Unauthenticated, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return resp, nil
}

// CheckPermission checks a permission.
func (s *Server) CheckPermission(ctx context.Context, req *authv1.CheckPermissionRequest) (*authv1.CheckPermissionResponse, error) {
	return s.svc.CheckPermission(ctx, req)
}

// --- UserService ---

// CreateUser creates a user.
func (s *Server) CreateUser(ctx context.Context, req *authv1.CreateUserRequest) (*authv1.User, error) {
	user, err := s.svc.CreateUser(ctx, req)
	if err != nil {
		if err == service.ErrUserExists {
			return nil, status.Error(codes.AlreadyExists, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return user, nil
}

// GetUser retrieves a user.
func (s *Server) GetUser(ctx context.Context, req *authv1.GetUserRequest) (*authv1.User, error) {
	user, err := s.svc.GetUser(ctx, req)
	if err != nil {
		if err == service.ErrUserNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return user, nil
}

// UpdateUser updates a user.
func (s *Server) UpdateUser(ctx context.Context, req *authv1.UpdateUserRequest) (*authv1.User, error) {
	user, err := s.svc.UpdateUser(ctx, req)
	if err != nil {
		if err == service.ErrUserNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return user, nil
}

// DeleteUser deletes a user.
func (s *Server) DeleteUser(ctx context.Context, req *authv1.DeleteUserRequest) (*emptypb.Empty, error) {
	_, err := s.svc.DeleteUser(ctx, req)
	if err != nil {
		if err == service.ErrUserNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &emptypb.Empty{}, nil
}

// ListUsers lists users.
func (s *Server) ListUsers(ctx context.Context, _ *emptypb.Empty) (*authv1.ListUsersResponse, error) {
	return s.svc.ListUsers(ctx, &emptypb.Empty{})
}

// ChangePassword changes a password.
func (s *Server) ChangePassword(ctx context.Context, req *authv1.ChangePasswordRequest) (*emptypb.Empty, error) {
	_, err := s.svc.ChangePassword(ctx, req)
	if err != nil {
		if err == service.ErrUserNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if err == service.ErrPasswordMismatch {
			return nil, status.Error(codes.Unauthenticated, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &emptypb.Empty{}, nil
}

// --- RoleService ---

// CreateRole creates a role.
func (s *Server) CreateRole(ctx context.Context, req *authv1.CreateRoleRequest) (*authv1.Role, error) {
	role, err := s.svc.CreateRole(ctx, req)
	if err != nil {
		if err == service.ErrRoleExists {
			return nil, status.Error(codes.AlreadyExists, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return role, nil
}

// GetRole retrieves a role.
func (s *Server) GetRole(ctx context.Context, req *authv1.GetRoleRequest) (*authv1.Role, error) {
	role, err := s.svc.GetRole(ctx, req)
	if err != nil {
		if err == service.ErrRoleNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return role, nil
}

// UpdateRole updates a role.
func (s *Server) UpdateRole(ctx context.Context, req *authv1.UpdateRoleRequest) (*authv1.Role, error) {
	role, err := s.svc.UpdateRole(ctx, req)
	if err != nil {
		if err == service.ErrRoleNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return role, nil
}

// DeleteRole deletes a role.
func (s *Server) DeleteRole(ctx context.Context, req *authv1.DeleteRoleRequest) (*emptypb.Empty, error) {
	_, err := s.svc.DeleteRole(ctx, req)
	if err != nil {
		if err == service.ErrRoleNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &emptypb.Empty{}, nil
}

// ListRoles lists roles.
func (s *Server) ListRoles(ctx context.Context, _ *emptypb.Empty) (*authv1.ListRolesResponse, error) {
	return s.svc.ListRoles(ctx, &emptypb.Empty{})
}

// AssignRole assigns a role.
func (s *Server) AssignRole(ctx context.Context, req *authv1.AssignRoleRequest) (*emptypb.Empty, error) {
	_, err := s.svc.AssignRole(ctx, req)
	if err != nil {
		if err == service.ErrUserNotFound || err == service.ErrRoleNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &emptypb.Empty{}, nil
}

// --- PermissionService ---

// ListPermissions lists permissions.
func (s *Server) ListPermissions(ctx context.Context, _ *emptypb.Empty) (*authv1.ListPermissionsResponse, error) {
	return s.svc.ListPermissions(ctx, &emptypb.Empty{})
}
