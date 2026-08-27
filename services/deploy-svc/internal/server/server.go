package server

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	deployv1 "github.com/Levango7/OpsMesh/services/deploy-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/deploy-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/deploy-svc/internal/service"
)

// Server implements the gRPC services for deploy-svc.
type Server struct {
	deployv1.UnimplementedDeploymentServiceServer
	deployv1.UnimplementedTemplateServiceServer
	deployv1.UnimplementedStrategyServiceServer
	deployv1.UnimplementedCanaryServiceServer
	svc *service.Service
}

// NewServer creates a new gRPC server.
func NewServer(svc *service.Service) *Server {
	return &Server{svc: svc}
}

// DeploymentService implementation

func (s *Server) CreateDeployment(ctx context.Context, req *deployv1.CreateDeploymentRequest) (*deployv1.Deployment, error) {
	d, err := s.svc.CreateDeployment(ctx, req.TenantId, req.Name, req.Type, req.RepoUrl, req.Content, req.Path, req.TargetIds, req.Strategy, int(req.CanaryWeight), req.AutoRollback, req.CreatedBy)
	if err != nil {
		if errors.Is(err, service.ErrDeploymentInvalid) {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return toProtoDeployment(d), nil
}

func (s *Server) GetDeployment(ctx context.Context, req *deployv1.GetDeploymentRequest) (*deployv1.Deployment, error) {
	d, err := s.svc.GetDeployment(ctx, req.Id, req.TenantId)
	if err != nil {
		if errors.Is(err, service.ErrDeploymentNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if errors.Is(err, service.ErrTenantMismatch) {
			return nil, status.Error(codes.PermissionDenied, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return toProtoDeployment(d), nil
}

func (s *Server) ListDeployments(ctx context.Context, req *deployv1.ListDeploymentsRequest) (*deployv1.ListDeploymentsResponse, error) {
	deployments, err := s.svc.ListDeployments(ctx, req.TenantId, req.Status)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	out := make([]*deployv1.Deployment, 0, len(deployments))
	for _, d := range deployments {
		out = append(out, toProtoDeployment(d))
	}
	return &deployv1.ListDeploymentsResponse{Deployments: out}, nil
}

func (s *Server) RollbackDeployment(ctx context.Context, req *deployv1.RollbackDeploymentRequest) (*deployv1.Deployment, error) {
	d, err := s.svc.RollbackDeployment(ctx, req.Id, req.TenantId)
	if err != nil {
		if errors.Is(err, service.ErrDeploymentNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if errors.Is(err, service.ErrTenantMismatch) {
			return nil, status.Error(codes.PermissionDenied, err.Error())
		}
		if errors.Is(err, service.ErrDeploymentInvalid) {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return toProtoDeployment(d), nil
}

func (s *Server) CancelDeployment(ctx context.Context, req *deployv1.CancelDeploymentRequest) (*deployv1.Deployment, error) {
	d, err := s.svc.CancelDeployment(ctx, req.Id, req.TenantId)
	if err != nil {
		if errors.Is(err, service.ErrDeploymentNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if errors.Is(err, service.ErrTenantMismatch) {
			return nil, status.Error(codes.PermissionDenied, err.Error())
		}
		if errors.Is(err, service.ErrDeploymentInvalid) {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return toProtoDeployment(d), nil
}

func (s *Server) GetDeploymentStatus(ctx context.Context, req *deployv1.GetDeploymentStatusRequest) (*deployv1.DeploymentStatus, error) {
	d, err := s.svc.GetDeploymentStatus(ctx, req.Id, req.TenantId)
	if err != nil {
		if errors.Is(err, service.ErrDeploymentNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if errors.Is(err, service.ErrTenantMismatch) {
			return nil, status.Error(codes.PermissionDenied, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &deployv1.DeploymentStatus{
		DeploymentId: d.ID,
		Status:       d.Status,
		TotalTargets: int32(len(d.TargetIDs)),
		UpdatedAt:    timestamppb.New(d.UpdatedAt),
		ErrorMessage: d.ErrorMessage,
	}, nil
}

// TemplateService implementation

func (s *Server) CreateTemplate(ctx context.Context, req *deployv1.CreateTemplateRequest) (*deployv1.Template, error) {
	t, err := s.svc.CreateTemplate(ctx, req.TenantId, req.Name, req.Description, req.Type, req.RepoUrl, req.Content, req.Path, req.Parameters, req.CreatedBy)
	if err != nil {
		if errors.Is(err, service.ErrTemplateInvalid) {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return toProtoTemplate(t), nil
}

func (s *Server) GetTemplate(ctx context.Context, req *deployv1.GetTemplateRequest) (*deployv1.Template, error) {
	t, err := s.svc.GetTemplate(ctx, req.Id, req.TenantId)
	if err != nil {
		if errors.Is(err, service.ErrTemplateNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if errors.Is(err, service.ErrTenantMismatch) {
			return nil, status.Error(codes.PermissionDenied, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return toProtoTemplate(t), nil
}

func (s *Server) UpdateTemplate(ctx context.Context, req *deployv1.UpdateTemplateRequest) (*deployv1.Template, error) {
	if req.Template == nil {
		return nil, status.Error(codes.InvalidArgument, "template required")
	}
	t, err := s.svc.UpdateTemplate(ctx, toModelTemplate(req.Template))
	if err != nil {
		if errors.Is(err, service.ErrTemplateNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if errors.Is(err, service.ErrTenantMismatch) {
			return nil, status.Error(codes.PermissionDenied, err.Error())
		}
		if errors.Is(err, service.ErrTemplateInvalid) {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return toProtoTemplate(t), nil
}

func (s *Server) DeleteTemplate(ctx context.Context, req *deployv1.DeleteTemplateRequest) (*emptypb.Empty, error) {
	if err := s.svc.DeleteTemplate(ctx, req.Id, req.TenantId); err != nil {
		if errors.Is(err, service.ErrTemplateNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if errors.Is(err, service.ErrTenantMismatch) {
			return nil, status.Error(codes.PermissionDenied, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &emptypb.Empty{}, nil
}

func (s *Server) ListTemplates(ctx context.Context, req *deployv1.ListTemplatesRequest) (*deployv1.ListTemplatesResponse, error) {
	templates, err := s.svc.ListTemplates(ctx, req.TenantId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	out := make([]*deployv1.Template, 0, len(templates))
	for _, t := range templates {
		out = append(out, toProtoTemplate(t))
	}
	return &deployv1.ListTemplatesResponse{Templates: out}, nil
}

// StrategyService implementation

func (s *Server) CreateStrategy(ctx context.Context, req *deployv1.CreateStrategyRequest) (*deployv1.Strategy, error) {
	st, err := s.svc.CreateStrategy(ctx, req.TenantId, req.Name, req.Description, req.Type, int(req.CanaryWeight), int(req.MaxUnavailable), int(req.MaxSurge), req.AutoRollback, int(req.TimeoutSeconds), req.CreatedBy)
	if err != nil {
		if errors.Is(err, service.ErrStrategyInvalid) {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return toProtoStrategy(st), nil
}

func (s *Server) GetStrategy(ctx context.Context, req *deployv1.GetStrategyRequest) (*deployv1.Strategy, error) {
	st, err := s.svc.GetStrategy(ctx, req.Id, req.TenantId)
	if err != nil {
		if errors.Is(err, service.ErrStrategyNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if errors.Is(err, service.ErrTenantMismatch) {
			return nil, status.Error(codes.PermissionDenied, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return toProtoStrategy(st), nil
}

func (s *Server) UpdateStrategy(ctx context.Context, req *deployv1.UpdateStrategyRequest) (*deployv1.Strategy, error) {
	if req.Strategy == nil {
		return nil, status.Error(codes.InvalidArgument, "strategy required")
	}
	st, err := s.svc.UpdateStrategy(ctx, toModelStrategy(req.Strategy))
	if err != nil {
		if errors.Is(err, service.ErrStrategyNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if errors.Is(err, service.ErrTenantMismatch) {
			return nil, status.Error(codes.PermissionDenied, err.Error())
		}
		if errors.Is(err, service.ErrStrategyInvalid) {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return toProtoStrategy(st), nil
}

func (s *Server) DeleteStrategy(ctx context.Context, req *deployv1.DeleteStrategyRequest) (*emptypb.Empty, error) {
	if err := s.svc.DeleteStrategy(ctx, req.Id, req.TenantId); err != nil {
		if errors.Is(err, service.ErrStrategyNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if errors.Is(err, service.ErrTenantMismatch) {
			return nil, status.Error(codes.PermissionDenied, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &emptypb.Empty{}, nil
}

func (s *Server) ListStrategies(ctx context.Context, req *deployv1.ListStrategiesRequest) (*deployv1.ListStrategiesResponse, error) {
	strategies, err := s.svc.ListStrategies(ctx, req.TenantId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	out := make([]*deployv1.Strategy, 0, len(strategies))
	for _, st := range strategies {
		out = append(out, toProtoStrategy(st))
	}
	return &deployv1.ListStrategiesResponse{Strategies: out}, nil
}

// CanaryService implementation

func (s *Server) StartCanary(ctx context.Context, req *deployv1.StartCanaryRequest) (*deployv1.Canary, error) {
	c, err := s.svc.StartCanary(ctx, req.TenantId, req.DeploymentId, req.Name, int(req.Weight), req.CreatedBy)
	if err != nil {
		if errors.Is(err, service.ErrCanaryInvalid) {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return toProtoCanary(c), nil
}

func (s *Server) GetCanaryStatus(ctx context.Context, req *deployv1.GetCanaryStatusRequest) (*deployv1.CanaryStatus, error) {
	c, err := s.svc.GetCanaryStatus(ctx, req.CanaryId, req.TenantId)
	if err != nil {
		if errors.Is(err, service.ErrCanaryNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if errors.Is(err, service.ErrTenantMismatch) {
			return nil, status.Error(codes.PermissionDenied, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	total := c.SuccessCount + c.FailureCount
	var successRate float64
	if total > 0 {
		successRate = float64(c.SuccessCount) / float64(total) * 100.0
	}
	return &deployv1.CanaryStatus{
		CanaryId:     c.ID,
		Status:       c.Status,
		Weight:       int32(c.Weight),
		SuccessCount: int32(c.SuccessCount),
		FailureCount: int32(c.FailureCount),
		SuccessRate:  successRate,
		UpdatedAt:    timestamppb.New(c.UpdatedAt),
	}, nil
}

func (s *Server) PromoteCanary(ctx context.Context, req *deployv1.PromoteCanaryRequest) (*deployv1.Canary, error) {
	c, err := s.svc.PromoteCanary(ctx, req.CanaryId, req.TenantId)
	if err != nil {
		if errors.Is(err, service.ErrCanaryNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if errors.Is(err, service.ErrTenantMismatch) {
			return nil, status.Error(codes.PermissionDenied, err.Error())
		}
		if errors.Is(err, service.ErrCanaryInvalid) {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return toProtoCanary(c), nil
}

func (s *Server) RollbackCanary(ctx context.Context, req *deployv1.RollbackCanaryRequest) (*deployv1.Canary, error) {
	c, err := s.svc.RollbackCanary(ctx, req.CanaryId, req.TenantId)
	if err != nil {
		if errors.Is(err, service.ErrCanaryNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if errors.Is(err, service.ErrTenantMismatch) {
			return nil, status.Error(codes.PermissionDenied, err.Error())
		}
		if errors.Is(err, service.ErrCanaryInvalid) {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return toProtoCanary(c), nil
}

func (s *Server) ListCanaries(ctx context.Context, req *deployv1.ListCanariesRequest) (*deployv1.ListCanariesResponse, error) {
	canaries, err := s.svc.ListCanaries(ctx, req.TenantId, req.Status)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	out := make([]*deployv1.Canary, 0, len(canaries))
	for _, c := range canaries {
		out = append(out, toProtoCanary(c))
	}
	return &deployv1.ListCanariesResponse{Canaries: out}, nil
}

// Mapping functions: model -> proto

func toProtoDeployment(d *models.Deployment) *deployv1.Deployment {
	return &deployv1.Deployment{
		Id:           d.ID,
		TenantId:     d.TenantID,
		Name:         d.Name,
		Type:         d.Type,
		RepoUrl:      d.RepoURL,
		Content:      d.Content,
		Path:         d.Path,
		TargetIds:    d.TargetIDs,
		Status:       d.Status,
		Strategy:     d.Strategy,
		CanaryWeight: int32(d.CanaryWeight),
		AutoRollback: d.AutoRollback,
		CreatedBy:    d.CreatedBy,
		CreatedAt:    timestamppb.New(d.CreatedAt),
		UpdatedAt:    timestamppb.New(d.UpdatedAt),
		ErrorMessage: d.ErrorMessage,
	}
}

func toProtoTemplate(t *models.Template) *deployv1.Template {
	return &deployv1.Template{
		Id:          t.ID,
		TenantId:    t.TenantID,
		Name:        t.Name,
		Description: t.Description,
		Type:        t.Type,
		RepoUrl:     t.RepoURL,
		Content:     t.Content,
		Path:        t.Path,
		Parameters:  t.Parameters,
		CreatedBy:   t.CreatedBy,
		CreatedAt:   timestamppb.New(t.CreatedAt),
		UpdatedAt:   timestamppb.New(t.UpdatedAt),
	}
}

func toProtoStrategy(st *models.Strategy) *deployv1.Strategy {
	return &deployv1.Strategy{
		Id:             st.ID,
		TenantId:       st.TenantID,
		Name:           st.Name,
		Description:    st.Description,
		Type:           st.Type,
		CanaryWeight:   int32(st.CanaryWeight),
		MaxUnavailable: int32(st.MaxUnavailable),
		MaxSurge:       int32(st.MaxSurge),
		AutoRollback:   st.AutoRollback,
		TimeoutSeconds: int32(st.TimeoutSeconds),
		CreatedBy:      st.CreatedBy,
		CreatedAt:      timestamppb.New(st.CreatedAt),
		UpdatedAt:      timestamppb.New(st.UpdatedAt),
	}
}

func toProtoCanary(c *models.Canary) *deployv1.Canary {
	return &deployv1.Canary{
		Id:           c.ID,
		TenantId:     c.TenantID,
		DeploymentId: c.DeploymentID,
		Name:         c.Name,
		Weight:       int32(c.Weight),
		Status:       c.Status,
		SuccessCount: int32(c.SuccessCount),
		FailureCount: int32(c.FailureCount),
		CreatedBy:    c.CreatedBy,
		CreatedAt:    timestamppb.New(c.CreatedAt),
		UpdatedAt:    timestamppb.New(c.UpdatedAt),
	}
}

// Mapping functions: proto -> model

func toModelTemplate(t *deployv1.Template) *models.Template {
	if t == nil {
		return nil
	}
	return &models.Template{
		ID:          t.Id,
		TenantID:    t.TenantId,
		Name:        t.Name,
		Description: t.Description,
		Type:        t.Type,
		RepoURL:     t.RepoUrl,
		Content:     t.Content,
		Path:        t.Path,
		Parameters:  t.Parameters,
		CreatedBy:   t.CreatedBy,
		CreatedAt:   t.CreatedAt.AsTime(),
		UpdatedAt:   t.UpdatedAt.AsTime(),
	}
}

func toModelStrategy(st *deployv1.Strategy) *models.Strategy {
	if st == nil {
		return nil
	}
	return &models.Strategy{
		ID:             st.Id,
		TenantID:       st.TenantId,
		Name:           st.Name,
		Description:    st.Description,
		Type:           st.Type,
		CanaryWeight:   int(st.CanaryWeight),
		MaxUnavailable: int(st.MaxUnavailable),
		MaxSurge:       int(st.MaxSurge),
		AutoRollback:   st.AutoRollback,
		TimeoutSeconds: int(st.TimeoutSeconds),
		CreatedBy:      st.CreatedBy,
		CreatedAt:      st.CreatedAt.AsTime(),
		UpdatedAt:      st.UpdatedAt.AsTime(),
	}
}

// Ensure unused imports are used
var _ = time.Now
