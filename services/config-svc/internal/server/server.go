package server

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	configv1 "github.com/Levango7/OpsMesh/services/config-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/config-svc/internal/service"
)

// Server implements the gRPC service handlers.
type Server struct {
	configv1.UnimplementedConfigServiceServer
	configv1.UnimplementedSecretServiceServer
	configv1.UnimplementedNotifyChannelServiceServer
	configv1.UnimplementedTemplateServiceServer
	svc *service.Service
}

// NewServer creates a new gRPC server.
func NewServer(svc *service.Service) *Server {
	return &Server{svc: svc}
}

// ConfigService handlers

func (s *Server) GetConfig(ctx context.Context, req *configv1.GetConfigRequest) (*configv1.ConfigItem, error) {
	cfg, err := s.svc.GetConfig(ctx, req)
	if err != nil {
		if err == service.ErrConfigNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if err == service.ErrConfigInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return cfg, nil
}

func (s *Server) SetConfig(ctx context.Context, req *configv1.SetConfigRequest) (*configv1.ConfigItem, error) {
	cfg, err := s.svc.SetConfig(ctx, req)
	if err != nil {
		if err == service.ErrConfigInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return cfg, nil
}

func (s *Server) DeleteConfig(ctx context.Context, req *configv1.DeleteConfigRequest) (*emptypb.Empty, error) {
	_, err := s.svc.DeleteConfig(ctx, req)
	if err != nil {
		if err == service.ErrConfigNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if err == service.ErrConfigInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &emptypb.Empty{}, nil
}

func (s *Server) ListConfigs(ctx context.Context, req *configv1.ListConfigsRequest) (*configv1.ListConfigsResponse, error) {
	return s.svc.ListConfigs(ctx, req)
}

func (s *Server) GetConfigHistory(ctx context.Context, req *configv1.GetConfigHistoryRequest) (*configv1.GetConfigHistoryResponse, error) {
	resp, err := s.svc.GetConfigHistory(ctx, req)
	if err != nil {
		if err == service.ErrConfigInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return resp, nil
}

func (s *Server) RollbackConfig(ctx context.Context, req *configv1.RollbackConfigRequest) (*configv1.ConfigItem, error) {
	cfg, err := s.svc.RollbackConfig(ctx, req)
	if err != nil {
		if err == service.ErrConfigInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		if err == service.ErrVersionNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return cfg, nil
}

// SecretService handlers

func (s *Server) CreateSecret(ctx context.Context, req *configv1.CreateSecretRequest) (*configv1.SecretMeta, error) {
	meta, err := s.svc.CreateSecret(ctx, req)
	if err != nil {
		if err == service.ErrSecretInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return meta, nil
}

func (s *Server) GetSecret(ctx context.Context, req *configv1.GetSecretRequest) (*configv1.SecretItem, error) {
	item, err := s.svc.GetSecret(ctx, req)
	if err != nil {
		if err == service.ErrSecretNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if err == service.ErrSecretInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return item, nil
}

func (s *Server) UpdateSecret(ctx context.Context, req *configv1.UpdateSecretRequest) (*configv1.SecretMeta, error) {
	meta, err := s.svc.UpdateSecret(ctx, req)
	if err != nil {
		if err == service.ErrSecretNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if err == service.ErrSecretInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return meta, nil
}

func (s *Server) DeleteSecret(ctx context.Context, req *configv1.DeleteSecretRequest) (*emptypb.Empty, error) {
	_, err := s.svc.DeleteSecret(ctx, req)
	if err != nil {
		if err == service.ErrSecretNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if err == service.ErrSecretInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &emptypb.Empty{}, nil
}

func (s *Server) ListSecrets(ctx context.Context, req *configv1.ListSecretsRequest) (*configv1.ListSecretsResponse, error) {
	return s.svc.ListSecrets(ctx, req)
}

func (s *Server) RotateSecret(ctx context.Context, req *configv1.RotateSecretRequest) (*configv1.SecretMeta, error) {
	meta, err := s.svc.RotateSecret(ctx, req)
	if err != nil {
		if err == service.ErrSecretNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if err == service.ErrSecretInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return meta, nil
}

// NotifyChannelService handlers

func (s *Server) CreateChannel(ctx context.Context, req *configv1.CreateChannelRequest) (*configv1.NotifyChannel, error) {
	ch, err := s.svc.CreateChannel(ctx, req)
	if err != nil {
		if err == service.ErrChannelInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return ch, nil
}

func (s *Server) GetChannel(ctx context.Context, req *configv1.GetChannelRequest) (*configv1.NotifyChannel, error) {
	ch, err := s.svc.GetChannel(ctx, req)
	if err != nil {
		if err == service.ErrChannelNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if err == service.ErrChannelInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return ch, nil
}

func (s *Server) UpdateChannel(ctx context.Context, req *configv1.UpdateChannelRequest) (*configv1.NotifyChannel, error) {
	ch, err := s.svc.UpdateChannel(ctx, req)
	if err != nil {
		if err == service.ErrChannelNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if err == service.ErrChannelInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return ch, nil
}

func (s *Server) DeleteChannel(ctx context.Context, req *configv1.DeleteChannelRequest) (*emptypb.Empty, error) {
	_, err := s.svc.DeleteChannel(ctx, req)
	if err != nil {
		if err == service.ErrChannelNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if err == service.ErrChannelInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &emptypb.Empty{}, nil
}

func (s *Server) ListChannels(ctx context.Context, req *configv1.ListChannelsRequest) (*configv1.ListChannelsResponse, error) {
	return s.svc.ListChannels(ctx, req)
}

func (s *Server) TestChannel(ctx context.Context, req *configv1.TestChannelRequest) (*configv1.TestChannelResponse, error) {
	resp, err := s.svc.TestChannel(ctx, req)
	if err != nil {
		if err == service.ErrChannelInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return resp, nil
}

// TemplateService handlers

func (s *Server) CreateTemplate(ctx context.Context, req *configv1.CreateTemplateRequest) (*configv1.ConfigTemplate, error) {
	tmpl, err := s.svc.CreateTemplate(ctx, req)
	if err != nil {
		if err == service.ErrTemplateInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return tmpl, nil
}

func (s *Server) GetTemplate(ctx context.Context, req *configv1.GetTemplateRequest) (*configv1.ConfigTemplate, error) {
	tmpl, err := s.svc.GetTemplate(ctx, req)
	if err != nil {
		if err == service.ErrTemplateNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if err == service.ErrTemplateInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return tmpl, nil
}

func (s *Server) UpdateTemplate(ctx context.Context, req *configv1.UpdateTemplateRequest) (*configv1.ConfigTemplate, error) {
	tmpl, err := s.svc.UpdateTemplate(ctx, req)
	if err != nil {
		if err == service.ErrTemplateNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if err == service.ErrTemplateInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return tmpl, nil
}

func (s *Server) DeleteTemplate(ctx context.Context, req *configv1.DeleteTemplateRequest) (*emptypb.Empty, error) {
	_, err := s.svc.DeleteTemplate(ctx, req)
	if err != nil {
		if err == service.ErrTemplateNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if err == service.ErrTemplateInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &emptypb.Empty{}, nil
}

func (s *Server) ListTemplates(ctx context.Context, req *configv1.ListTemplatesRequest) (*configv1.ListTemplatesResponse, error) {
	return s.svc.ListTemplates(ctx, req)
}

func (s *Server) ApplyTemplate(ctx context.Context, req *configv1.ApplyTemplateRequest) (*configv1.ApplyTemplateResponse, error) {
	resp, err := s.svc.ApplyTemplate(ctx, req)
	if err != nil {
		if err == service.ErrTemplateNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if err == service.ErrTemplateInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return resp, nil
}
