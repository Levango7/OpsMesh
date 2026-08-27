// Package server provides the gRPC server implementation for log-svc.
package server

import (
	"context"
	"encoding/json"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"opsmesh.io/log-svc/api/proto/v1"
)

// jsonCodec implements grpc.Codec for JSON serialization.
type jsonCodec struct{}

// Marshal serializes a value to JSON.
func (jsonCodec) Marshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

// Unmarshal deserializes JSON data into a value.
func (jsonCodec) Unmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// Name returns the codec name.
func (jsonCodec) Name() string {
	return "json"
}

// String returns the codec name (implements grpc.Codec).
func (jsonCodec) String() string {
	return "json"
}

// Server implements the LogServiceServer interface with validation.
type Server struct {
	logv1.UnimplementedLogServiceServer
	svc logv1.LogServiceServer
}

// NewServer creates a new gRPC server instance.
func NewServer(svc logv1.LogServiceServer) *Server {
	return &Server{svc: svc}
}

// SearchLogs implements LogServiceServer.SearchLogs.
func (s *Server) SearchLogs(ctx context.Context, req *logv1.SearchLogsRequest) (*logv1.SearchLogsResponse, error) {
	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	return s.svc.SearchLogs(ctx, req)
}

// AppendLog implements LogServiceServer.AppendLog.
func (s *Server) AppendLog(ctx context.Context, req *logv1.AppendLogRequest) (*logv1.LogEntry, error) {
	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.Message == "" {
		return nil, status.Error(codes.InvalidArgument, "message is required")
	}
	return s.svc.AppendLog(ctx, req)
}

// GetLogStats implements LogServiceServer.GetLogStats.
func (s *Server) GetLogStats(ctx context.Context, req *logv1.GetLogStatsRequest) (*logv1.GetLogStatsResponse, error) {
	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	return s.svc.GetLogStats(ctx, req)
}

// ListLogSources implements LogServiceServer.ListLogSources.
func (s *Server) ListLogSources(ctx context.Context, req *logv1.ListLogSourcesRequest) (*logv1.ListLogSourcesResponse, error) {
	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	return s.svc.ListLogSources(ctx, req)
}

// GRPCServer wraps a grpc.Server with additional functionality.
type GRPCServer struct {
	server *grpc.Server
}

// NewGRPCServer creates a new gRPC server with the given service.
func NewGRPCServer(svc logv1.LogServiceServer, opts ...grpc.ServerOption) *GRPCServer {
	// Use JSON codec for message serialization
	codec := jsonCodec{}
	opts = append([]grpc.ServerOption{
		grpc.CustomCodec(codec),
	}, opts...)

	grpcServer := grpc.NewServer(opts...)
	logv1.RegisterLogServiceServer(grpcServer, svc)

	return &GRPCServer{server: grpcServer}
}

// Serve starts serving on the given listener.
func (s *GRPCServer) Serve(lis net.Listener) error {
	return s.server.Serve(lis)
}

// GracefulStop gracefully stops the server.
func (s *GRPCServer) GracefulStop() {
	s.server.GracefulStop()
}

// Stop immediately stops the server.
func (s *GRPCServer) Stop() {
	s.server.Stop()
}
