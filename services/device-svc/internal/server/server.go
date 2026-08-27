package server

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	devicev1 "github.com/Levango7/OpsMesh/services/device-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/device-svc/internal/service"
)

// Server implements the DeviceService, AgentService, CMDBService, and DiscoveryService gRPC interfaces.
type Server struct {
	devicev1.UnimplementedDeviceServiceServer
	devicev1.UnimplementedAgentServiceServer
	devicev1.UnimplementedCMDBServiceServer
	devicev1.UnimplementedDiscoveryServiceServer
	svc *service.Service
}

// NewServer creates a new gRPC server.
func NewServer(svc *service.Service) *Server {
	return &Server{svc: svc}
}

// === DeviceService ===

// RegisterDevice registers a new device.
func (s *Server) RegisterDevice(ctx context.Context, req *devicev1.RegisterDeviceRequest) (*devicev1.Device, error) {
	d, err := s.svc.RegisterDevice(ctx, req)
	if err != nil {
		if err == service.ErrDeviceInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return d, nil
}

// Heartbeat updates device heartbeat.
func (s *Server) Heartbeat(ctx context.Context, req *devicev1.HeartbeatRequest) (*emptypb.Empty, error) {
	if err := s.svc.HeartbeatDevice(ctx, req); err != nil {
		if err == service.ErrDeviceNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &emptypb.Empty{}, nil
}

// GetDevice retrieves a device by ID.
func (s *Server) GetDevice(ctx context.Context, req *devicev1.GetDeviceRequest) (*devicev1.Device, error) {
	d, err := s.svc.GetDevice(ctx, req)
	if err != nil {
		if err == service.ErrDeviceNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return d, nil
}

// ListDevices lists devices.
func (s *Server) ListDevices(ctx context.Context, req *devicev1.ListDevicesRequest) (*devicev1.ListDevicesResponse, error) {
	return s.svc.ListDevices(ctx, req)
}

// UpdateDevice updates a device.
func (s *Server) UpdateDevice(ctx context.Context, req *devicev1.UpdateDeviceRequest) (*devicev1.Device, error) {
	d, err := s.svc.UpdateDevice(ctx, req)
	if err != nil {
		if err == service.ErrDeviceNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if err == service.ErrDeviceInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return d, nil
}

// DeleteDevice removes a device.
func (s *Server) DeleteDevice(ctx context.Context, req *devicev1.DeleteDeviceRequest) (*emptypb.Empty, error) {
	if err := s.svc.DeleteDevice(ctx, req); err != nil {
		if err == service.ErrDeviceNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &emptypb.Empty{}, nil
}

// GetDeviceStatus returns device status.
func (s *Server) GetDeviceStatus(ctx context.Context, req *devicev1.GetDeviceStatusRequest) (*devicev1.DeviceStatus, error) {
	st, err := s.svc.GetDeviceStatus(ctx, req)
	if err != nil {
		if err == service.ErrDeviceNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return st, nil
}

// === AgentService ===

// RegisterAgent registers a new agent.
func (s *Server) RegisterAgent(ctx context.Context, req *devicev1.RegisterAgentRequest) (*devicev1.Agent, error) {
	a, err := s.svc.RegisterAgent(ctx, req)
	if err != nil {
		if err == service.ErrAgentInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return a, nil
}

// GetAgent retrieves an agent by ID.
func (s *Server) GetAgent(ctx context.Context, req *devicev1.GetAgentRequest) (*devicev1.Agent, error) {
	a, err := s.svc.GetAgent(ctx, req)
	if err != nil {
		if err == service.ErrAgentNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return a, nil
}

// ListAgents lists agents.
func (s *Server) ListAgents(ctx context.Context, req *devicev1.ListAgentsRequest) (*devicev1.ListAgentsResponse, error) {
	return s.svc.ListAgents(ctx, req)
}

// UpdateAgentStatus updates agent status.
func (s *Server) UpdateAgentStatus(ctx context.Context, req *devicev1.UpdateAgentStatusRequest) (*devicev1.Agent, error) {
	a, err := s.svc.UpdateAgentStatus(ctx, req)
	if err != nil {
		if err == service.ErrAgentNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return a, nil
}

// Heartbeat updates agent heartbeat.
func (s *Server) AgentHeartbeat(ctx context.Context, req *devicev1.AgentHeartbeatRequest) (*emptypb.Empty, error) {
	if err := s.svc.HeartbeatAgent(ctx, req); err != nil {
		if err == service.ErrAgentNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &emptypb.Empty{}, nil
}

// === CMDBService ===

// CreateCI creates a new CI.
func (s *Server) CreateCI(ctx context.Context, req *devicev1.CreateCIRequest) (*devicev1.CI, error) {
	ci, err := s.svc.CreateCI(ctx, req)
	if err != nil {
		if err == service.ErrCIInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return ci, nil
}

// GetCI retrieves a CI by ID.
func (s *Server) GetCI(ctx context.Context, req *devicev1.GetCIRequest) (*devicev1.CI, error) {
	ci, err := s.svc.GetCI(ctx, req)
	if err != nil {
		if err == service.ErrCINotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return ci, nil
}

// UpdateCI updates a CI.
func (s *Server) UpdateCI(ctx context.Context, req *devicev1.UpdateCIRequest) (*devicev1.CI, error) {
	ci, err := s.svc.UpdateCI(ctx, req)
	if err != nil {
		if err == service.ErrCINotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if err == service.ErrCIInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return ci, nil
}

// DeleteCI removes a CI.
func (s *Server) DeleteCI(ctx context.Context, req *devicev1.DeleteCIRequest) (*emptypb.Empty, error) {
	if err := s.svc.DeleteCI(ctx, req); err != nil {
		if err == service.ErrCINotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &emptypb.Empty{}, nil
}

// ListCIs lists CIs.
func (s *Server) ListCIs(ctx context.Context, req *devicev1.ListCIsRequest) (*devicev1.ListCIsResponse, error) {
	return s.svc.ListCIs(ctx, req)
}

// CreateCIRelation creates a CI relationship.
func (s *Server) CreateCIRelation(ctx context.Context, req *devicev1.CreateCIRelationRequest) (*devicev1.CIRelation, error) {
	rel, err := s.svc.CreateCIRelation(ctx, req)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return rel, nil
}

// GetCIRelations retrieves CI relations.
func (s *Server) GetCIRelations(ctx context.Context, req *devicev1.GetCIRelationsRequest) (*devicev1.GetCIRelationsResponse, error) {
	return s.svc.GetCIRelations(ctx, req)
}

// === DiscoveryService ===

// StartDiscovery initiates network discovery.
func (s *Server) StartDiscovery(ctx context.Context, req *devicev1.StartDiscoveryRequest) (*devicev1.DiscoveryJob, error) {
	job, err := s.svc.StartDiscovery(ctx, req)
	if err != nil {
		if err == service.ErrJobInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return job, nil
}

// GetDiscoveryStatus returns discovery job status.
func (s *Server) GetDiscoveryStatus(ctx context.Context, req *devicev1.GetDiscoveryStatusRequest) (*devicev1.DiscoveryJob, error) {
	job, err := s.svc.GetDiscoveryStatus(ctx, req)
	if err != nil {
		if err == service.ErrJobNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return job, nil
}

// ListDiscoveredDevices lists discovered devices.
func (s *Server) ListDiscoveredDevices(ctx context.Context, req *devicev1.ListDiscoveredDevicesRequest) (*devicev1.ListDiscoveredDevicesResponse, error) {
	return s.svc.ListDiscoveredDevices(ctx, req)
}
