package logv1

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
)

// LogServiceServer is the server API for LogService service.
type LogServiceServer interface {
	SearchLogs(context.Context, *SearchLogsRequest) (*SearchLogsResponse, error)
	AppendLog(context.Context, *AppendLogRequest) (*LogEntry, error)
	GetLogStats(context.Context, *GetLogStatsRequest) (*GetLogStatsResponse, error)
	ListLogSources(context.Context, *ListLogSourcesRequest) (*ListLogSourcesResponse, error)
	mustEmbedUnimplementedLogServiceServer()
}

// UnimplementedLogServiceServer must be embedded to have forward compatible implementations.
type UnimplementedLogServiceServer struct{}

func (UnimplementedLogServiceServer) SearchLogs(context.Context, *SearchLogsRequest) (*SearchLogsResponse, error) {
	return nil, fmt.Errorf("method SearchLogs not implemented")
}

func (UnimplementedLogServiceServer) AppendLog(context.Context, *AppendLogRequest) (*LogEntry, error) {
	return nil, fmt.Errorf("method AppendLog not implemented")
}

func (UnimplementedLogServiceServer) GetLogStats(context.Context, *GetLogStatsRequest) (*GetLogStatsResponse, error) {
	return nil, fmt.Errorf("method GetLogStats not implemented")
}

func (UnimplementedLogServiceServer) ListLogSources(context.Context, *ListLogSourcesRequest) (*ListLogSourcesResponse, error) {
	return nil, fmt.Errorf("method ListLogSources not implemented")
}

func (UnimplementedLogServiceServer) mustEmbedUnimplementedLogServiceServer() {}

// UnsafeLogServiceServer may be embedded to opt out of forward compatibility for this service.
type UnsafeLogServiceServer interface {
	mustEmbedUnimplementedLogServiceServer()
}

// RegisterLogServiceServer registers the service implementation with the gRPC server.
func RegisterLogServiceServer(s grpc.ServiceRegistrar, srv LogServiceServer) {
	s.RegisterService(&LogService_ServiceDesc, srv)
}

// LogServiceClient is the client API for LogService service.
type LogServiceClient interface {
	SearchLogs(ctx context.Context, in *SearchLogsRequest, opts ...grpc.CallOption) (*SearchLogsResponse, error)
	AppendLog(ctx context.Context, in *AppendLogRequest, opts ...grpc.CallOption) (*LogEntry, error)
	GetLogStats(ctx context.Context, in *GetLogStatsRequest, opts ...grpc.CallOption) (*GetLogStatsResponse, error)
	ListLogSources(ctx context.Context, in *ListLogSourcesRequest, opts ...grpc.CallOption) (*ListLogSourcesResponse, error)
}

type logServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewLogServiceClient(cc grpc.ClientConnInterface) LogServiceClient {
	return &logServiceClient{cc: cc}
}

func (c *logServiceClient) SearchLogs(ctx context.Context, in *SearchLogsRequest, opts ...grpc.CallOption) (*SearchLogsResponse, error) {
	out := new(SearchLogsResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.log.v1.LogService/SearchLogs", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *logServiceClient) AppendLog(ctx context.Context, in *AppendLogRequest, opts ...grpc.CallOption) (*LogEntry, error) {
	out := new(LogEntry)
	err := c.cc.Invoke(ctx, "/opsmesh.log.v1.LogService/AppendLog", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *logServiceClient) GetLogStats(ctx context.Context, in *GetLogStatsRequest, opts ...grpc.CallOption) (*GetLogStatsResponse, error) {
	out := new(GetLogStatsResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.log.v1.LogService/GetLogStats", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *logServiceClient) ListLogSources(ctx context.Context, in *ListLogSourcesRequest, opts ...grpc.CallOption) (*ListLogSourcesResponse, error) {
	out := new(ListLogSourcesResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.log.v1.LogService/ListLogSources", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// LogService_ServiceDesc is the grpc.ServiceDesc for LogService service.
var LogService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "opsmesh.log.v1.LogService",
	HandlerType: (*LogServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "SearchLogs",
			Handler:    _LogService_SearchLogs_Handler,
		},
		{
			MethodName: "AppendLog",
			Handler:    _LogService_AppendLog_Handler,
		},
		{
			MethodName: "GetLogStats",
			Handler:    _LogService_GetLogStats_Handler,
		},
		{
			MethodName: "ListLogSources",
			Handler:    _LogService_ListLogSources_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "api/proto/v1/log.proto",
}

func _LogService_SearchLogs_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(SearchLogsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(LogServiceServer).SearchLogs(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.log.v1.LogService/SearchLogs",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(LogServiceServer).SearchLogs(ctx, req.(*SearchLogsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _LogService_AppendLog_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(AppendLogRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(LogServiceServer).AppendLog(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.log.v1.LogService/AppendLog",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(LogServiceServer).AppendLog(ctx, req.(*AppendLogRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _LogService_GetLogStats_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetLogStatsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(LogServiceServer).GetLogStats(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.log.v1.LogService/GetLogStats",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(LogServiceServer).GetLogStats(ctx, req.(*GetLogStatsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _LogService_ListLogSources_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListLogSourcesRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(LogServiceServer).ListLogSources(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.log.v1.LogService/ListLogSources",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(LogServiceServer).ListLogSources(ctx, req.(*ListLogSourcesRequest))
	}
	return interceptor(ctx, in, info, handler)
}
