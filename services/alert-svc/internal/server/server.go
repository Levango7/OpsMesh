package server

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	alertv1 "github.com/Levango7/OpsMesh/services/alert-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/alert-svc/internal/service"
)

// Server implements the AlertService gRPC interface.
type Server struct {
	alertv1.UnimplementedAlertServiceServer
	svc *service.Service
}

// NewServer creates a new gRPC server.
func NewServer(svc *service.Service) *Server {
	return &Server{svc: svc}
}

// CreateRule creates a new alert rule.
func (s *Server) CreateRule(ctx context.Context, req *alertv1.CreateRuleRequest) (*alertv1.AlertRule, error) {
	rule, err := s.svc.CreateRule(ctx, req)
	if err != nil {
		if err == service.ErrRuleInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return rule, nil
}

// GetRule retrieves a rule by ID.
func (s *Server) GetRule(ctx context.Context, req *alertv1.GetRuleRequest) (*alertv1.AlertRule, error) {
	rule, err := s.svc.GetRule(ctx, req)
	if err != nil {
		if err == service.ErrRuleNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return rule, nil
}

// ListRules lists all rules.
func (s *Server) ListRules(ctx context.Context, _ *emptypb.Empty) (*alertv1.ListRulesResponse, error) {
	return s.svc.ListRules(ctx)
}

// UpdateRule updates an existing rule.
func (s *Server) UpdateRule(ctx context.Context, req *alertv1.UpdateRuleRequest) (*alertv1.AlertRule, error) {
	rule, err := s.svc.UpdateRule(ctx, req)
	if err != nil {
		if err == service.ErrRuleNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if err == service.ErrRuleInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return rule, nil
}

// DeleteRule deletes a rule by ID.
func (s *Server) DeleteRule(ctx context.Context, req *alertv1.DeleteRuleRequest) (*emptypb.Empty, error) {
	if err := s.svc.DeleteRule(ctx, req); err != nil {
		if err == service.ErrRuleNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &emptypb.Empty{}, nil
}

// Evaluate evaluates metrics against rules.
func (s *Server) Evaluate(ctx context.Context, req *alertv1.EvaluateRequest) (*alertv1.EvaluateResponse, error) {
	return s.svc.Evaluate(ctx, req)
}

// GetAlert retrieves an alert by ID.
func (s *Server) GetAlert(ctx context.Context, req *alertv1.GetAlertRequest) (*alertv1.Alert, error) {
	alert, err := s.svc.GetAlert(ctx, req)
	if err != nil {
		if err == service.ErrAlertNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return alert, nil
}

// ListAlerts lists alerts with optional filtering.
func (s *Server) ListAlerts(ctx context.Context, req *alertv1.ListAlertsRequest) (*alertv1.ListAlertsResponse, error) {
	return s.svc.ListAlerts(ctx, req)
}

// AckAlert acknowledges an alert.
func (s *Server) AckAlert(ctx context.Context, req *alertv1.AckAlertRequest) (*emptypb.Empty, error) {
	if err := s.svc.AckAlert(ctx, req); err != nil {
		if err == service.ErrAlertNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &emptypb.Empty{}, nil
}

// SilenceAlert silences an alert.
func (s *Server) SilenceAlert(ctx context.Context, req *alertv1.SilenceAlertRequest) (*emptypb.Empty, error) {
	if err := s.svc.SilenceAlert(ctx, req); err != nil {
		if err == service.ErrAlertNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &emptypb.Empty{}, nil
}
