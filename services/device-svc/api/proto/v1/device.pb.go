package devicev1

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Device represents a managed device.
type Device struct {
	Id            string
	TenantId      string
	Name          string
	Ip            string
	Mac           string
	Os            string
	Arch          string
	Status        string
	AgentId       string
	Tags          []string
	Labels        map[string]string
	Group         string
	LastHeartbeat *timestamppb.Timestamp
	CreatedAt     *timestamppb.Timestamp
	UpdatedAt     *timestamppb.Timestamp
}

// Agent represents a registered agent.
type Agent struct {
	Id            string
	TenantId      string
	DeviceId      string
	Hostname      string
	Version       string
	Status        string
	Load          int32
	Os            string
	Arch          string
	Addr          string
	GrpcPort      int32
	MetricsPort   int32
	LastHeartbeat *timestamppb.Timestamp
	CreatedAt     *timestamppb.Timestamp
	UpdatedAt     *timestamppb.Timestamp
}

// CI represents a Configuration Item.
type CI struct {
	Id         string
	TenantId   string
	CiType     string
	Name       string
	Status     string
	Attributes map[string]string
	Source     string
	AgentId    string
	DeviceId   string
	Version    int32
	CreatedAt  *timestamppb.Timestamp
	UpdatedAt  *timestamppb.Timestamp
}

// CIRelation represents a relationship between two CIs.
type CIRelation struct {
	Id           int64
	SourceCiId   string
	TargetCiId   string
	RelationType string
	TenantId     string
	Attributes   map[string]string
	CreatedAt    *timestamppb.Timestamp
}

// DiscoveryJob represents a network discovery job.
type DiscoveryJob struct {
	Id           string
	TenantId     string
	Cidr         string
	Status       string
	TotalHosts   int32
	ScannedHosts int32
	FoundDevices int32
	Error        string
	StartedAt    *timestamppb.Timestamp
	CompletedAt  *timestamppb.Timestamp
}

// DeviceStatus represents the current status of a device.
type DeviceStatus struct {
	DeviceId      string
	Status        string
	Reachable     bool
	UptimeSeconds int64
	LastHeartbeat *timestamppb.Timestamp
}

// Request/Response messages

type RegisterDeviceRequest struct {
	Device *Device
}

type HeartbeatRequest struct {
	DeviceId string
	Status   string
}

type GetDeviceRequest struct {
	Id string
}

type ListDevicesRequest struct {
	TenantId string
	Status   string
	Group    string
	Limit    int32
}

type ListDevicesResponse struct {
	Devices []*Device
}

type UpdateDeviceRequest struct {
	Device *Device
}

type DeleteDeviceRequest struct {
	Id string
}

type GetDeviceStatusRequest struct {
	DeviceId string
}

type RegisterAgentRequest struct {
	Agent *Agent
}

type GetAgentRequest struct {
	Id string
}

type ListAgentsRequest struct {
	TenantId string
	Status   string
	Limit    int32
}

type ListAgentsResponse struct {
	Agents []*Agent
}

type UpdateAgentStatusRequest struct {
	AgentId string
	Status  string
	Load    int32
}

type AgentHeartbeatRequest struct {
	AgentId string
	Status  string
	Load    int32
}

type CreateCIRequest struct {
	Ci *CI
}

type GetCIRequest struct {
	Id string
}

type UpdateCIRequest struct {
	Ci *CI
}

type DeleteCIRequest struct {
	Id string
}

type ListCIsRequest struct {
	TenantId string
	CiType   string
	Status   string
	Limit    int32
}

type ListCIsResponse struct {
	Cis []*CI
}

type CreateCIRelationRequest struct {
	Relation *CIRelation
}

type GetCIRelationsRequest struct {
	CiId string
}

type GetCIRelationsResponse struct {
	Relations []*CIRelation
}

type StartDiscoveryRequest struct {
	Cidr     string
	TenantId string
}

type GetDiscoveryStatusRequest struct {
	JobId string
}

type ListDiscoveredDevicesRequest struct {
	JobId    string
	TenantId string
}

type ListDiscoveredDevicesResponse struct {
	Devices []*Device
}

// DeviceServiceServer is the server API for DeviceService.
type DeviceServiceServer interface {
	RegisterDevice(context.Context, *RegisterDeviceRequest) (*Device, error)
	Heartbeat(context.Context, *HeartbeatRequest) (*emptypb.Empty, error)
	GetDevice(context.Context, *GetDeviceRequest) (*Device, error)
	ListDevices(context.Context, *ListDevicesRequest) (*ListDevicesResponse, error)
	UpdateDevice(context.Context, *UpdateDeviceRequest) (*Device, error)
	DeleteDevice(context.Context, *DeleteDeviceRequest) (*emptypb.Empty, error)
	GetDeviceStatus(context.Context, *GetDeviceStatusRequest) (*DeviceStatus, error)
	mustEmbedUnimplementedDeviceServiceServer()
}

// UnimplementedDeviceServiceServer must be embedded to have forward compatible implementations.
type UnimplementedDeviceServiceServer struct{}

func (UnimplementedDeviceServiceServer) RegisterDevice(context.Context, *RegisterDeviceRequest) (*Device, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RegisterDevice not implemented")
}
func (UnimplementedDeviceServiceServer) Heartbeat(context.Context, *HeartbeatRequest) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Heartbeat not implemented")
}
func (UnimplementedDeviceServiceServer) GetDevice(context.Context, *GetDeviceRequest) (*Device, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetDevice not implemented")
}
func (UnimplementedDeviceServiceServer) ListDevices(context.Context, *ListDevicesRequest) (*ListDevicesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListDevices not implemented")
}
func (UnimplementedDeviceServiceServer) UpdateDevice(context.Context, *UpdateDeviceRequest) (*Device, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UpdateDevice not implemented")
}
func (UnimplementedDeviceServiceServer) DeleteDevice(context.Context, *DeleteDeviceRequest) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteDevice not implemented")
}
func (UnimplementedDeviceServiceServer) GetDeviceStatus(context.Context, *GetDeviceStatusRequest) (*DeviceStatus, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetDeviceStatus not implemented")
}
func (UnimplementedDeviceServiceServer) mustEmbedUnimplementedDeviceServiceServer() {}

// UnsafeDeviceServiceServer may be embedded to opt out of forward compatibility.
type UnsafeDeviceServiceServer interface {
	mustEmbedUnimplementedDeviceServiceServer()
}

// DeviceServiceClient is the client API for DeviceService.
type DeviceServiceClient interface {
	RegisterDevice(ctx context.Context, in *RegisterDeviceRequest, opts ...grpc.CallOption) (*Device, error)
	Heartbeat(ctx context.Context, in *HeartbeatRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
	GetDevice(ctx context.Context, in *GetDeviceRequest, opts ...grpc.CallOption) (*Device, error)
	ListDevices(ctx context.Context, in *ListDevicesRequest, opts ...grpc.CallOption) (*ListDevicesResponse, error)
	UpdateDevice(ctx context.Context, in *UpdateDeviceRequest, opts ...grpc.CallOption) (*Device, error)
	DeleteDevice(ctx context.Context, in *DeleteDeviceRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
	GetDeviceStatus(ctx context.Context, in *GetDeviceStatusRequest, opts ...grpc.CallOption) (*DeviceStatus, error)
}

type deviceServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewDeviceServiceClient(cc grpc.ClientConnInterface) DeviceServiceClient {
	return &deviceServiceClient{cc: cc}
}

func (c *deviceServiceClient) RegisterDevice(ctx context.Context, in *RegisterDeviceRequest, opts ...grpc.CallOption) (*Device, error) {
	out := new(Device)
	err := c.cc.Invoke(ctx, "/opsmesh.device.v1.DeviceService/RegisterDevice", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *deviceServiceClient) Heartbeat(ctx context.Context, in *HeartbeatRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, "/opsmesh.device.v1.DeviceService/Heartbeat", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *deviceServiceClient) GetDevice(ctx context.Context, in *GetDeviceRequest, opts ...grpc.CallOption) (*Device, error) {
	out := new(Device)
	err := c.cc.Invoke(ctx, "/opsmesh.device.v1.DeviceService/GetDevice", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *deviceServiceClient) ListDevices(ctx context.Context, in *ListDevicesRequest, opts ...grpc.CallOption) (*ListDevicesResponse, error) {
	out := new(ListDevicesResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.device.v1.DeviceService/ListDevices", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *deviceServiceClient) UpdateDevice(ctx context.Context, in *UpdateDeviceRequest, opts ...grpc.CallOption) (*Device, error) {
	out := new(Device)
	err := c.cc.Invoke(ctx, "/opsmesh.device.v1.DeviceService/UpdateDevice", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *deviceServiceClient) DeleteDevice(ctx context.Context, in *DeleteDeviceRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, "/opsmesh.device.v1.DeviceService/DeleteDevice", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *deviceServiceClient) GetDeviceStatus(ctx context.Context, in *GetDeviceStatusRequest, opts ...grpc.CallOption) (*DeviceStatus, error) {
	out := new(DeviceStatus)
	err := c.cc.Invoke(ctx, "/opsmesh.device.v1.DeviceService/GetDeviceStatus", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RegisterDeviceServiceServer registers the server.
func RegisterDeviceServiceServer(s grpc.ServiceRegistrar, srv DeviceServiceServer) {
	s.RegisterService(&_DeviceService_serviceDesc, srv)
}

func _DeviceService_RegisterDevice_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(RegisterDeviceRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DeviceServiceServer).RegisterDevice(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.device.v1.DeviceService/RegisterDevice",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DeviceServiceServer).RegisterDevice(ctx, req.(*RegisterDeviceRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _DeviceService_Heartbeat_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(HeartbeatRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DeviceServiceServer).Heartbeat(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.device.v1.DeviceService/Heartbeat",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DeviceServiceServer).Heartbeat(ctx, req.(*HeartbeatRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _DeviceService_GetDevice_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetDeviceRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DeviceServiceServer).GetDevice(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.device.v1.DeviceService/GetDevice",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DeviceServiceServer).GetDevice(ctx, req.(*GetDeviceRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _DeviceService_ListDevices_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListDevicesRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DeviceServiceServer).ListDevices(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.device.v1.DeviceService/ListDevices",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DeviceServiceServer).ListDevices(ctx, req.(*ListDevicesRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _DeviceService_UpdateDevice_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(UpdateDeviceRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DeviceServiceServer).UpdateDevice(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.device.v1.DeviceService/UpdateDevice",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DeviceServiceServer).UpdateDevice(ctx, req.(*UpdateDeviceRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _DeviceService_DeleteDevice_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DeleteDeviceRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DeviceServiceServer).DeleteDevice(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.device.v1.DeviceService/DeleteDevice",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DeviceServiceServer).DeleteDevice(ctx, req.(*DeleteDeviceRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _DeviceService_GetDeviceStatus_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetDeviceStatusRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DeviceServiceServer).GetDeviceStatus(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.device.v1.DeviceService/GetDeviceStatus",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DeviceServiceServer).GetDeviceStatus(ctx, req.(*GetDeviceStatusRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _DeviceService_serviceDesc = grpc.ServiceDesc{
	ServiceName: "opsmesh.device.v1.DeviceService",
	HandlerType: (*DeviceServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "RegisterDevice", Handler: _DeviceService_RegisterDevice_Handler},
		{MethodName: "Heartbeat", Handler: _DeviceService_Heartbeat_Handler},
		{MethodName: "GetDevice", Handler: _DeviceService_GetDevice_Handler},
		{MethodName: "ListDevices", Handler: _DeviceService_ListDevices_Handler},
		{MethodName: "UpdateDevice", Handler: _DeviceService_UpdateDevice_Handler},
		{MethodName: "DeleteDevice", Handler: _DeviceService_DeleteDevice_Handler},
		{MethodName: "GetDeviceStatus", Handler: _DeviceService_GetDeviceStatus_Handler},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "api/proto/v1/device.proto",
}

// AgentServiceServer is the server API for AgentService.
type AgentServiceServer interface {
	RegisterAgent(context.Context, *RegisterAgentRequest) (*Agent, error)
	GetAgent(context.Context, *GetAgentRequest) (*Agent, error)
	ListAgents(context.Context, *ListAgentsRequest) (*ListAgentsResponse, error)
	UpdateAgentStatus(context.Context, *UpdateAgentStatusRequest) (*Agent, error)
	AgentHeartbeat(context.Context, *AgentHeartbeatRequest) (*emptypb.Empty, error)
	mustEmbedUnimplementedAgentServiceServer()
}

// UnimplementedAgentServiceServer must be embedded to have forward compatible implementations.
type UnimplementedAgentServiceServer struct{}

func (UnimplementedAgentServiceServer) RegisterAgent(context.Context, *RegisterAgentRequest) (*Agent, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RegisterAgent not implemented")
}
func (UnimplementedAgentServiceServer) GetAgent(context.Context, *GetAgentRequest) (*Agent, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetAgent not implemented")
}
func (UnimplementedAgentServiceServer) ListAgents(context.Context, *ListAgentsRequest) (*ListAgentsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListAgents not implemented")
}
func (UnimplementedAgentServiceServer) UpdateAgentStatus(context.Context, *UpdateAgentStatusRequest) (*Agent, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UpdateAgentStatus not implemented")
}
func (UnimplementedAgentServiceServer) AgentHeartbeat(context.Context, *AgentHeartbeatRequest) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method AgentHeartbeat not implemented")
}
func (UnimplementedAgentServiceServer) mustEmbedUnimplementedAgentServiceServer() {}

// UnsafeAgentServiceServer may be embedded to opt out of forward compatibility.
type UnsafeAgentServiceServer interface {
	mustEmbedUnimplementedAgentServiceServer()
}

// AgentServiceClient is the client API for AgentService.
type AgentServiceClient interface {
	RegisterAgent(ctx context.Context, in *RegisterAgentRequest, opts ...grpc.CallOption) (*Agent, error)
	GetAgent(ctx context.Context, in *GetAgentRequest, opts ...grpc.CallOption) (*Agent, error)
	ListAgents(ctx context.Context, in *ListAgentsRequest, opts ...grpc.CallOption) (*ListAgentsResponse, error)
	UpdateAgentStatus(ctx context.Context, in *UpdateAgentStatusRequest, opts ...grpc.CallOption) (*Agent, error)
	AgentHeartbeat(ctx context.Context, in *AgentHeartbeatRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
}

type agentServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewAgentServiceClient(cc grpc.ClientConnInterface) AgentServiceClient {
	return &agentServiceClient{cc: cc}
}

func (c *agentServiceClient) RegisterAgent(ctx context.Context, in *RegisterAgentRequest, opts ...grpc.CallOption) (*Agent, error) {
	out := new(Agent)
	err := c.cc.Invoke(ctx, "/opsmesh.device.v1.AgentService/RegisterAgent", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *agentServiceClient) GetAgent(ctx context.Context, in *GetAgentRequest, opts ...grpc.CallOption) (*Agent, error) {
	out := new(Agent)
	err := c.cc.Invoke(ctx, "/opsmesh.device.v1.AgentService/GetAgent", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *agentServiceClient) ListAgents(ctx context.Context, in *ListAgentsRequest, opts ...grpc.CallOption) (*ListAgentsResponse, error) {
	out := new(ListAgentsResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.device.v1.AgentService/ListAgents", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *agentServiceClient) UpdateAgentStatus(ctx context.Context, in *UpdateAgentStatusRequest, opts ...grpc.CallOption) (*Agent, error) {
	out := new(Agent)
	err := c.cc.Invoke(ctx, "/opsmesh.device.v1.AgentService/UpdateAgentStatus", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *agentServiceClient) AgentHeartbeat(ctx context.Context, in *AgentHeartbeatRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, "/opsmesh.device.v1.AgentService/AgentHeartbeat", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RegisterAgentServiceServer registers the server.
func RegisterAgentServiceServer(s grpc.ServiceRegistrar, srv AgentServiceServer) {
	s.RegisterService(&_AgentService_serviceDesc, srv)
}

func _AgentService_RegisterAgent_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(RegisterAgentRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AgentServiceServer).RegisterAgent(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.device.v1.AgentService/RegisterAgent",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AgentServiceServer).RegisterAgent(ctx, req.(*RegisterAgentRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _AgentService_GetAgent_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetAgentRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AgentServiceServer).GetAgent(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.device.v1.AgentService/GetAgent",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AgentServiceServer).GetAgent(ctx, req.(*GetAgentRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _AgentService_ListAgents_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListAgentsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AgentServiceServer).ListAgents(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.device.v1.AgentService/ListAgents",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AgentServiceServer).ListAgents(ctx, req.(*ListAgentsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _AgentService_UpdateAgentStatus_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(UpdateAgentStatusRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AgentServiceServer).UpdateAgentStatus(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.device.v1.AgentService/UpdateAgentStatus",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AgentServiceServer).UpdateAgentStatus(ctx, req.(*UpdateAgentStatusRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _AgentService_AgentHeartbeat_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(AgentHeartbeatRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AgentServiceServer).AgentHeartbeat(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.device.v1.AgentService/AgentHeartbeat",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AgentServiceServer).AgentHeartbeat(ctx, req.(*AgentHeartbeatRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _AgentService_serviceDesc = grpc.ServiceDesc{
	ServiceName: "opsmesh.device.v1.AgentService",
	HandlerType: (*AgentServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "RegisterAgent", Handler: _AgentService_RegisterAgent_Handler},
		{MethodName: "GetAgent", Handler: _AgentService_GetAgent_Handler},
		{MethodName: "ListAgents", Handler: _AgentService_ListAgents_Handler},
		{MethodName: "UpdateAgentStatus", Handler: _AgentService_UpdateAgentStatus_Handler},
		{MethodName: "AgentHeartbeat", Handler: _AgentService_AgentHeartbeat_Handler},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "api/proto/v1/device.proto",
}

// CMDBServiceServer is the server API for CMDBService.
type CMDBServiceServer interface {
	CreateCI(context.Context, *CreateCIRequest) (*CI, error)
	GetCI(context.Context, *GetCIRequest) (*CI, error)
	UpdateCI(context.Context, *UpdateCIRequest) (*CI, error)
	DeleteCI(context.Context, *DeleteCIRequest) (*emptypb.Empty, error)
	ListCIs(context.Context, *ListCIsRequest) (*ListCIsResponse, error)
	CreateCIRelation(context.Context, *CreateCIRelationRequest) (*CIRelation, error)
	GetCIRelations(context.Context, *GetCIRelationsRequest) (*GetCIRelationsResponse, error)
	mustEmbedUnimplementedCMDBServiceServer()
}

// UnimplementedCMDBServiceServer must be embedded to have forward compatible implementations.
type UnimplementedCMDBServiceServer struct{}

func (UnimplementedCMDBServiceServer) CreateCI(context.Context, *CreateCIRequest) (*CI, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CreateCI not implemented")
}
func (UnimplementedCMDBServiceServer) GetCI(context.Context, *GetCIRequest) (*CI, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetCI not implemented")
}
func (UnimplementedCMDBServiceServer) UpdateCI(context.Context, *UpdateCIRequest) (*CI, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UpdateCI not implemented")
}
func (UnimplementedCMDBServiceServer) DeleteCI(context.Context, *DeleteCIRequest) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteCI not implemented")
}
func (UnimplementedCMDBServiceServer) ListCIs(context.Context, *ListCIsRequest) (*ListCIsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListCIs not implemented")
}
func (UnimplementedCMDBServiceServer) CreateCIRelation(context.Context, *CreateCIRelationRequest) (*CIRelation, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CreateCIRelation not implemented")
}
func (UnimplementedCMDBServiceServer) GetCIRelations(context.Context, *GetCIRelationsRequest) (*GetCIRelationsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetCIRelations not implemented")
}
func (UnimplementedCMDBServiceServer) mustEmbedUnimplementedCMDBServiceServer() {}

// UnsafeCMDBServiceServer may be embedded to opt out of forward compatibility.
type UnsafeCMDBServiceServer interface {
	mustEmbedUnimplementedCMDBServiceServer()
}

// CMDBServiceClient is the client API for CMDBService.
type CMDBServiceClient interface {
	CreateCI(ctx context.Context, in *CreateCIRequest, opts ...grpc.CallOption) (*CI, error)
	GetCI(ctx context.Context, in *GetCIRequest, opts ...grpc.CallOption) (*CI, error)
	UpdateCI(ctx context.Context, in *UpdateCIRequest, opts ...grpc.CallOption) (*CI, error)
	DeleteCI(ctx context.Context, in *DeleteCIRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
	ListCIs(ctx context.Context, in *ListCIsRequest, opts ...grpc.CallOption) (*ListCIsResponse, error)
	CreateCIRelation(ctx context.Context, in *CreateCIRelationRequest, opts ...grpc.CallOption) (*CIRelation, error)
	GetCIRelations(ctx context.Context, in *GetCIRelationsRequest, opts ...grpc.CallOption) (*GetCIRelationsResponse, error)
}

type cmdbServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewCMDBServiceClient(cc grpc.ClientConnInterface) CMDBServiceClient {
	return &cmdbServiceClient{cc: cc}
}

func (c *cmdbServiceClient) CreateCI(ctx context.Context, in *CreateCIRequest, opts ...grpc.CallOption) (*CI, error) {
	out := new(CI)
	err := c.cc.Invoke(ctx, "/opsmesh.device.v1.CMDBService/CreateCI", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *cmdbServiceClient) GetCI(ctx context.Context, in *GetCIRequest, opts ...grpc.CallOption) (*CI, error) {
	out := new(CI)
	err := c.cc.Invoke(ctx, "/opsmesh.device.v1.CMDBService/GetCI", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *cmdbServiceClient) UpdateCI(ctx context.Context, in *UpdateCIRequest, opts ...grpc.CallOption) (*CI, error) {
	out := new(CI)
	err := c.cc.Invoke(ctx, "/opsmesh.device.v1.CMDBService/UpdateCI", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *cmdbServiceClient) DeleteCI(ctx context.Context, in *DeleteCIRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, "/opsmesh.device.v1.CMDBService/DeleteCI", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *cmdbServiceClient) ListCIs(ctx context.Context, in *ListCIsRequest, opts ...grpc.CallOption) (*ListCIsResponse, error) {
	out := new(ListCIsResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.device.v1.CMDBService/ListCIs", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *cmdbServiceClient) CreateCIRelation(ctx context.Context, in *CreateCIRelationRequest, opts ...grpc.CallOption) (*CIRelation, error) {
	out := new(CIRelation)
	err := c.cc.Invoke(ctx, "/opsmesh.device.v1.CMDBService/CreateCIRelation", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *cmdbServiceClient) GetCIRelations(ctx context.Context, in *GetCIRelationsRequest, opts ...grpc.CallOption) (*GetCIRelationsResponse, error) {
	out := new(GetCIRelationsResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.device.v1.CMDBService/GetCIRelations", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RegisterCMDBServiceServer registers the server.
func RegisterCMDBServiceServer(s grpc.ServiceRegistrar, srv CMDBServiceServer) {
	s.RegisterService(&_CMDBService_serviceDesc, srv)
}

func _CMDBService_CreateCI_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CreateCIRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CMDBServiceServer).CreateCI(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.device.v1.CMDBService/CreateCI",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CMDBServiceServer).CreateCI(ctx, req.(*CreateCIRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CMDBService_GetCI_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetCIRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CMDBServiceServer).GetCI(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.device.v1.CMDBService/GetCI",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CMDBServiceServer).GetCI(ctx, req.(*GetCIRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CMDBService_UpdateCI_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(UpdateCIRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CMDBServiceServer).UpdateCI(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.device.v1.CMDBService/UpdateCI",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CMDBServiceServer).UpdateCI(ctx, req.(*UpdateCIRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CMDBService_DeleteCI_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DeleteCIRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CMDBServiceServer).DeleteCI(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.device.v1.CMDBService/DeleteCI",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CMDBServiceServer).DeleteCI(ctx, req.(*DeleteCIRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CMDBService_ListCIs_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListCIsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CMDBServiceServer).ListCIs(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.device.v1.CMDBService/ListCIs",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CMDBServiceServer).ListCIs(ctx, req.(*ListCIsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CMDBService_CreateCIRelation_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CreateCIRelationRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CMDBServiceServer).CreateCIRelation(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.device.v1.CMDBService/CreateCIRelation",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CMDBServiceServer).CreateCIRelation(ctx, req.(*CreateCIRelationRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CMDBService_GetCIRelations_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetCIRelationsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CMDBServiceServer).GetCIRelations(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.device.v1.CMDBService/GetCIRelations",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CMDBServiceServer).GetCIRelations(ctx, req.(*GetCIRelationsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _CMDBService_serviceDesc = grpc.ServiceDesc{
	ServiceName: "opsmesh.device.v1.CMDBService",
	HandlerType: (*CMDBServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "CreateCI", Handler: _CMDBService_CreateCI_Handler},
		{MethodName: "GetCI", Handler: _CMDBService_GetCI_Handler},
		{MethodName: "UpdateCI", Handler: _CMDBService_UpdateCI_Handler},
		{MethodName: "DeleteCI", Handler: _CMDBService_DeleteCI_Handler},
		{MethodName: "ListCIs", Handler: _CMDBService_ListCIs_Handler},
		{MethodName: "CreateCIRelation", Handler: _CMDBService_CreateCIRelation_Handler},
		{MethodName: "GetCIRelations", Handler: _CMDBService_GetCIRelations_Handler},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "api/proto/v1/device.proto",
}

// DiscoveryServiceServer is the server API for DiscoveryService.
type DiscoveryServiceServer interface {
	StartDiscovery(context.Context, *StartDiscoveryRequest) (*DiscoveryJob, error)
	GetDiscoveryStatus(context.Context, *GetDiscoveryStatusRequest) (*DiscoveryJob, error)
	ListDiscoveredDevices(context.Context, *ListDiscoveredDevicesRequest) (*ListDiscoveredDevicesResponse, error)
	mustEmbedUnimplementedDiscoveryServiceServer()
}

// UnimplementedDiscoveryServiceServer must be embedded to have forward compatible implementations.
type UnimplementedDiscoveryServiceServer struct{}

func (UnimplementedDiscoveryServiceServer) StartDiscovery(context.Context, *StartDiscoveryRequest) (*DiscoveryJob, error) {
	return nil, status.Errorf(codes.Unimplemented, "method StartDiscovery not implemented")
}
func (UnimplementedDiscoveryServiceServer) GetDiscoveryStatus(context.Context, *GetDiscoveryStatusRequest) (*DiscoveryJob, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetDiscoveryStatus not implemented")
}
func (UnimplementedDiscoveryServiceServer) ListDiscoveredDevices(context.Context, *ListDiscoveredDevicesRequest) (*ListDiscoveredDevicesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListDiscoveredDevices not implemented")
}
func (UnimplementedDiscoveryServiceServer) mustEmbedUnimplementedDiscoveryServiceServer() {}

// UnsafeDiscoveryServiceServer may be embedded to opt out of forward compatibility.
type UnsafeDiscoveryServiceServer interface {
	mustEmbedUnimplementedDiscoveryServiceServer()
}

// DiscoveryServiceClient is the client API for DiscoveryService.
type DiscoveryServiceClient interface {
	StartDiscovery(ctx context.Context, in *StartDiscoveryRequest, opts ...grpc.CallOption) (*DiscoveryJob, error)
	GetDiscoveryStatus(ctx context.Context, in *GetDiscoveryStatusRequest, opts ...grpc.CallOption) (*DiscoveryJob, error)
	ListDiscoveredDevices(ctx context.Context, in *ListDiscoveredDevicesRequest, opts ...grpc.CallOption) (*ListDiscoveredDevicesResponse, error)
}

type discoveryServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewDiscoveryServiceClient(cc grpc.ClientConnInterface) DiscoveryServiceClient {
	return &discoveryServiceClient{cc: cc}
}

func (c *discoveryServiceClient) StartDiscovery(ctx context.Context, in *StartDiscoveryRequest, opts ...grpc.CallOption) (*DiscoveryJob, error) {
	out := new(DiscoveryJob)
	err := c.cc.Invoke(ctx, "/opsmesh.device.v1.DiscoveryService/StartDiscovery", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *discoveryServiceClient) GetDiscoveryStatus(ctx context.Context, in *GetDiscoveryStatusRequest, opts ...grpc.CallOption) (*DiscoveryJob, error) {
	out := new(DiscoveryJob)
	err := c.cc.Invoke(ctx, "/opsmesh.device.v1.DiscoveryService/GetDiscoveryStatus", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *discoveryServiceClient) ListDiscoveredDevices(ctx context.Context, in *ListDiscoveredDevicesRequest, opts ...grpc.CallOption) (*ListDiscoveredDevicesResponse, error) {
	out := new(ListDiscoveredDevicesResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.device.v1.DiscoveryService/ListDiscoveredDevices", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RegisterDiscoveryServiceServer registers the server.
func RegisterDiscoveryServiceServer(s grpc.ServiceRegistrar, srv DiscoveryServiceServer) {
	s.RegisterService(&_DiscoveryService_serviceDesc, srv)
}

func _DiscoveryService_StartDiscovery_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(StartDiscoveryRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DiscoveryServiceServer).StartDiscovery(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.device.v1.DiscoveryService/StartDiscovery",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DiscoveryServiceServer).StartDiscovery(ctx, req.(*StartDiscoveryRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _DiscoveryService_GetDiscoveryStatus_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetDiscoveryStatusRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DiscoveryServiceServer).GetDiscoveryStatus(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.device.v1.DiscoveryService/GetDiscoveryStatus",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DiscoveryServiceServer).GetDiscoveryStatus(ctx, req.(*GetDiscoveryStatusRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _DiscoveryService_ListDiscoveredDevices_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListDiscoveredDevicesRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DiscoveryServiceServer).ListDiscoveredDevices(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.device.v1.DiscoveryService/ListDiscoveredDevices",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DiscoveryServiceServer).ListDiscoveredDevices(ctx, req.(*ListDiscoveredDevicesRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _DiscoveryService_serviceDesc = grpc.ServiceDesc{
	ServiceName: "opsmesh.device.v1.DiscoveryService",
	HandlerType: (*DiscoveryServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "StartDiscovery", Handler: _DiscoveryService_StartDiscovery_Handler},
		{MethodName: "GetDiscoveryStatus", Handler: _DiscoveryService_GetDiscoveryStatus_Handler},
		{MethodName: "ListDiscoveredDevices", Handler: _DiscoveryService_ListDiscoveredDevices_Handler},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "api/proto/v1/device.proto",
}
