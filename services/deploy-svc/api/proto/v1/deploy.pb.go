package deployv1

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Deployment represents a deployment task.
type Deployment struct {
	Id           string
	TenantId     string
	Name         string
	Type         string
	RepoUrl      string
	Content      string
	Path         string
	TargetIds    []string
	Status       string
	Strategy     string
	CanaryWeight int32
	AutoRollback bool
	CreatedBy    string
	CreatedAt    *timestamppb.Timestamp
	UpdatedAt    *timestamppb.Timestamp
	ErrorMessage string
}

// Template represents a deployment template.
type Template struct {
	Id          string
	TenantId    string
	Name        string
	Description string
	Type        string
	RepoUrl     string
	Content     string
	Path        string
	Parameters  map[string]string
	CreatedBy   string
	CreatedAt   *timestamppb.Timestamp
	UpdatedAt   *timestamppb.Timestamp
}

// Strategy represents a deployment strategy.
type Strategy struct {
	Id             string
	TenantId       string
	Name           string
	Description    string
	Type           string
	CanaryWeight   int32
	MaxUnavailable int32
	MaxSurge       int32
	AutoRollback   bool
	TimeoutSeconds int32
	CreatedBy      string
	CreatedAt      *timestamppb.Timestamp
	UpdatedAt      *timestamppb.Timestamp
}

// Canary represents a canary deployment.
type Canary struct {
	Id           string
	TenantId     string
	DeploymentId string
	Name         string
	Weight       int32
	Status       string
	SuccessCount int32
	FailureCount int32
	CreatedBy    string
	CreatedAt    *timestamppb.Timestamp
	UpdatedAt    *timestamppb.Timestamp
}

// DeploymentStatus represents the status of a deployment.
type DeploymentStatus struct {
	DeploymentId     string
	Status           string
	TotalTargets     int32
	CompletedTargets int32
	FailedTargets    int32
	ErrorMessage     string
	UpdatedAt        *timestamppb.Timestamp
}

// CanaryStatus represents the status of a canary deployment.
type CanaryStatus struct {
	CanaryId       string
	Status         string
	Weight         int32
	SuccessCount   int32
	FailureCount   int32
	SuccessRate    float64
	AnalysisResult string
	UpdatedAt      *timestamppb.Timestamp
}

// Request/Response messages for DeploymentService

type CreateDeploymentRequest struct {
	TenantId     string
	Name         string
	Type         string
	RepoUrl      string
	Content      string
	Path         string
	TargetIds    []string
	Strategy     string
	CanaryWeight int32
	AutoRollback bool
	CreatedBy    string
}

type GetDeploymentRequest struct {
	Id       string
	TenantId string
}

type ListDeploymentsRequest struct {
	TenantId string
	Status   string
	Limit    int32
}

type ListDeploymentsResponse struct {
	Deployments []*Deployment
}

type RollbackDeploymentRequest struct {
	Id       string
	TenantId string
}

type CancelDeploymentRequest struct {
	Id       string
	TenantId string
}

type GetDeploymentStatusRequest struct {
	Id       string
	TenantId string
}

// Request/Response messages for TemplateService

type CreateTemplateRequest struct {
	TenantId    string
	Name        string
	Description string
	Type        string
	RepoUrl     string
	Content     string
	Path        string
	Parameters  map[string]string
	CreatedBy   string
}

type GetTemplateRequest struct {
	Id       string
	TenantId string
}

type UpdateTemplateRequest struct {
	Template *Template
}

type DeleteTemplateRequest struct {
	Id       string
	TenantId string
}

type ListTemplatesRequest struct {
	TenantId string
	Limit    int32
}

type ListTemplatesResponse struct {
	Templates []*Template
}

// Request/Response messages for StrategyService

type CreateStrategyRequest struct {
	TenantId       string
	Name           string
	Description    string
	Type           string
	CanaryWeight   int32
	MaxUnavailable int32
	MaxSurge       int32
	AutoRollback   bool
	TimeoutSeconds int32
	CreatedBy      string
}

type GetStrategyRequest struct {
	Id       string
	TenantId string
}

type UpdateStrategyRequest struct {
	Strategy *Strategy
}

type DeleteStrategyRequest struct {
	Id       string
	TenantId string
}

type ListStrategiesRequest struct {
	TenantId string
	Limit    int32
}

type ListStrategiesResponse struct {
	Strategies []*Strategy
}

// Request/Response messages for CanaryService

type StartCanaryRequest struct {
	TenantId     string
	DeploymentId string
	Name         string
	Weight       int32
	CreatedBy    string
}

type GetCanaryStatusRequest struct {
	CanaryId string
	TenantId string
}

type PromoteCanaryRequest struct {
	CanaryId string
	TenantId string
}

type RollbackCanaryRequest struct {
	CanaryId string
	TenantId string
}

type ListCanariesRequest struct {
	TenantId string
	Status   string
	Limit    int32
}

type ListCanariesResponse struct {
	Canaries []*Canary
}

// DeploymentServiceServer is the server API for DeploymentService.
type DeploymentServiceServer interface {
	CreateDeployment(context.Context, *CreateDeploymentRequest) (*Deployment, error)
	GetDeployment(context.Context, *GetDeploymentRequest) (*Deployment, error)
	ListDeployments(context.Context, *ListDeploymentsRequest) (*ListDeploymentsResponse, error)
	RollbackDeployment(context.Context, *RollbackDeploymentRequest) (*Deployment, error)
	CancelDeployment(context.Context, *CancelDeploymentRequest) (*Deployment, error)
	GetDeploymentStatus(context.Context, *GetDeploymentStatusRequest) (*DeploymentStatus, error)
	mustEmbedUnimplementedDeploymentServiceServer()
}

// UnimplementedDeploymentServiceServer must be embedded to have forward compatible implementations.
type UnimplementedDeploymentServiceServer struct{}

func (UnimplementedDeploymentServiceServer) CreateDeployment(context.Context, *CreateDeploymentRequest) (*Deployment, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CreateDeployment not implemented")
}
func (UnimplementedDeploymentServiceServer) GetDeployment(context.Context, *GetDeploymentRequest) (*Deployment, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetDeployment not implemented")
}
func (UnimplementedDeploymentServiceServer) ListDeployments(context.Context, *ListDeploymentsRequest) (*ListDeploymentsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListDeployments not implemented")
}
func (UnimplementedDeploymentServiceServer) RollbackDeployment(context.Context, *RollbackDeploymentRequest) (*Deployment, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RollbackDeployment not implemented")
}
func (UnimplementedDeploymentServiceServer) CancelDeployment(context.Context, *CancelDeploymentRequest) (*Deployment, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CancelDeployment not implemented")
}
func (UnimplementedDeploymentServiceServer) GetDeploymentStatus(context.Context, *GetDeploymentStatusRequest) (*DeploymentStatus, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetDeploymentStatus not implemented")
}
func (UnimplementedDeploymentServiceServer) mustEmbedUnimplementedDeploymentServiceServer() {}

// UnsafeDeploymentServiceServer may be embedded to opt out of forward compatibility.
type UnsafeDeploymentServiceServer interface {
	mustEmbedUnimplementedDeploymentServiceServer()
}

// DeploymentServiceClient is the client API for DeploymentService.
type DeploymentServiceClient interface {
	CreateDeployment(ctx context.Context, in *CreateDeploymentRequest, opts ...grpc.CallOption) (*Deployment, error)
	GetDeployment(ctx context.Context, in *GetDeploymentRequest, opts ...grpc.CallOption) (*Deployment, error)
	ListDeployments(ctx context.Context, in *ListDeploymentsRequest, opts ...grpc.CallOption) (*ListDeploymentsResponse, error)
	RollbackDeployment(ctx context.Context, in *RollbackDeploymentRequest, opts ...grpc.CallOption) (*Deployment, error)
	CancelDeployment(ctx context.Context, in *CancelDeploymentRequest, opts ...grpc.CallOption) (*Deployment, error)
	GetDeploymentStatus(ctx context.Context, in *GetDeploymentStatusRequest, opts ...grpc.CallOption) (*DeploymentStatus, error)
}

type deploymentServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewDeploymentServiceClient(cc grpc.ClientConnInterface) DeploymentServiceClient {
	return &deploymentServiceClient{cc: cc}
}

func (c *deploymentServiceClient) CreateDeployment(ctx context.Context, in *CreateDeploymentRequest, opts ...grpc.CallOption) (*Deployment, error) {
	out := new(Deployment)
	err := c.cc.Invoke(ctx, "/opsmesh.deploy.v1.DeploymentService/CreateDeployment", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *deploymentServiceClient) GetDeployment(ctx context.Context, in *GetDeploymentRequest, opts ...grpc.CallOption) (*Deployment, error) {
	out := new(Deployment)
	err := c.cc.Invoke(ctx, "/opsmesh.deploy.v1.DeploymentService/GetDeployment", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *deploymentServiceClient) ListDeployments(ctx context.Context, in *ListDeploymentsRequest, opts ...grpc.CallOption) (*ListDeploymentsResponse, error) {
	out := new(ListDeploymentsResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.deploy.v1.DeploymentService/ListDeployments", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *deploymentServiceClient) RollbackDeployment(ctx context.Context, in *RollbackDeploymentRequest, opts ...grpc.CallOption) (*Deployment, error) {
	out := new(Deployment)
	err := c.cc.Invoke(ctx, "/opsmesh.deploy.v1.DeploymentService/RollbackDeployment", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *deploymentServiceClient) CancelDeployment(ctx context.Context, in *CancelDeploymentRequest, opts ...grpc.CallOption) (*Deployment, error) {
	out := new(Deployment)
	err := c.cc.Invoke(ctx, "/opsmesh.deploy.v1.DeploymentService/CancelDeployment", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *deploymentServiceClient) GetDeploymentStatus(ctx context.Context, in *GetDeploymentStatusRequest, opts ...grpc.CallOption) (*DeploymentStatus, error) {
	out := new(DeploymentStatus)
	err := c.cc.Invoke(ctx, "/opsmesh.deploy.v1.DeploymentService/GetDeploymentStatus", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RegisterDeploymentServiceServer registers the server.
func RegisterDeploymentServiceServer(s grpc.ServiceRegistrar, srv DeploymentServiceServer) {
	s.RegisterService(&_DeploymentService_serviceDesc, srv)
}

func _DeploymentService_CreateDeployment_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CreateDeploymentRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DeploymentServiceServer).CreateDeployment(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.deploy.v1.DeploymentService/CreateDeployment",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DeploymentServiceServer).CreateDeployment(ctx, req.(*CreateDeploymentRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _DeploymentService_GetDeployment_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetDeploymentRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DeploymentServiceServer).GetDeployment(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.deploy.v1.DeploymentService/GetDeployment",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DeploymentServiceServer).GetDeployment(ctx, req.(*GetDeploymentRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _DeploymentService_ListDeployments_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListDeploymentsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DeploymentServiceServer).ListDeployments(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.deploy.v1.DeploymentService/ListDeployments",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DeploymentServiceServer).ListDeployments(ctx, req.(*ListDeploymentsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _DeploymentService_RollbackDeployment_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(RollbackDeploymentRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DeploymentServiceServer).RollbackDeployment(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.deploy.v1.DeploymentService/RollbackDeployment",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DeploymentServiceServer).RollbackDeployment(ctx, req.(*RollbackDeploymentRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _DeploymentService_CancelDeployment_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CancelDeploymentRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DeploymentServiceServer).CancelDeployment(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.deploy.v1.DeploymentService/CancelDeployment",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DeploymentServiceServer).CancelDeployment(ctx, req.(*CancelDeploymentRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _DeploymentService_GetDeploymentStatus_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetDeploymentStatusRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DeploymentServiceServer).GetDeploymentStatus(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.deploy.v1.DeploymentService/GetDeploymentStatus",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DeploymentServiceServer).GetDeploymentStatus(ctx, req.(*GetDeploymentStatusRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _DeploymentService_serviceDesc = grpc.ServiceDesc{
	ServiceName: "opsmesh.deploy.v1.DeploymentService",
	HandlerType: (*DeploymentServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "CreateDeployment",
			Handler:    _DeploymentService_CreateDeployment_Handler,
		},
		{
			MethodName: "GetDeployment",
			Handler:    _DeploymentService_GetDeployment_Handler,
		},
		{
			MethodName: "ListDeployments",
			Handler:    _DeploymentService_ListDeployments_Handler,
		},
		{
			MethodName: "RollbackDeployment",
			Handler:    _DeploymentService_RollbackDeployment_Handler,
		},
		{
			MethodName: "CancelDeployment",
			Handler:    _DeploymentService_CancelDeployment_Handler,
		},
		{
			MethodName: "GetDeploymentStatus",
			Handler:    _DeploymentService_GetDeploymentStatus_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "api/proto/v1/deploy.proto",
}

// TemplateServiceServer is the server API for TemplateService.
type TemplateServiceServer interface {
	CreateTemplate(context.Context, *CreateTemplateRequest) (*Template, error)
	GetTemplate(context.Context, *GetTemplateRequest) (*Template, error)
	UpdateTemplate(context.Context, *UpdateTemplateRequest) (*Template, error)
	DeleteTemplate(context.Context, *DeleteTemplateRequest) (*emptypb.Empty, error)
	ListTemplates(context.Context, *ListTemplatesRequest) (*ListTemplatesResponse, error)
	mustEmbedUnimplementedTemplateServiceServer()
}

// UnimplementedTemplateServiceServer must be embedded to have forward compatible implementations.
type UnimplementedTemplateServiceServer struct{}

func (UnimplementedTemplateServiceServer) CreateTemplate(context.Context, *CreateTemplateRequest) (*Template, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CreateTemplate not implemented")
}
func (UnimplementedTemplateServiceServer) GetTemplate(context.Context, *GetTemplateRequest) (*Template, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetTemplate not implemented")
}
func (UnimplementedTemplateServiceServer) UpdateTemplate(context.Context, *UpdateTemplateRequest) (*Template, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UpdateTemplate not implemented")
}
func (UnimplementedTemplateServiceServer) DeleteTemplate(context.Context, *DeleteTemplateRequest) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteTemplate not implemented")
}
func (UnimplementedTemplateServiceServer) ListTemplates(context.Context, *ListTemplatesRequest) (*ListTemplatesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListTemplates not implemented")
}
func (UnimplementedTemplateServiceServer) mustEmbedUnimplementedTemplateServiceServer() {}

// UnsafeTemplateServiceServer may be embedded to opt out of forward compatibility.
type UnsafeTemplateServiceServer interface {
	mustEmbedUnimplementedTemplateServiceServer()
}

// TemplateServiceClient is the client API for TemplateService.
type TemplateServiceClient interface {
	CreateTemplate(ctx context.Context, in *CreateTemplateRequest, opts ...grpc.CallOption) (*Template, error)
	GetTemplate(ctx context.Context, in *GetTemplateRequest, opts ...grpc.CallOption) (*Template, error)
	UpdateTemplate(ctx context.Context, in *UpdateTemplateRequest, opts ...grpc.CallOption) (*Template, error)
	DeleteTemplate(ctx context.Context, in *DeleteTemplateRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
	ListTemplates(ctx context.Context, in *ListTemplatesRequest, opts ...grpc.CallOption) (*ListTemplatesResponse, error)
}

type templateServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewTemplateServiceClient(cc grpc.ClientConnInterface) TemplateServiceClient {
	return &templateServiceClient{cc: cc}
}

func (c *templateServiceClient) CreateTemplate(ctx context.Context, in *CreateTemplateRequest, opts ...grpc.CallOption) (*Template, error) {
	out := new(Template)
	err := c.cc.Invoke(ctx, "/opsmesh.deploy.v1.TemplateService/CreateTemplate", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *templateServiceClient) GetTemplate(ctx context.Context, in *GetTemplateRequest, opts ...grpc.CallOption) (*Template, error) {
	out := new(Template)
	err := c.cc.Invoke(ctx, "/opsmesh.deploy.v1.TemplateService/GetTemplate", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *templateServiceClient) UpdateTemplate(ctx context.Context, in *UpdateTemplateRequest, opts ...grpc.CallOption) (*Template, error) {
	out := new(Template)
	err := c.cc.Invoke(ctx, "/opsmesh.deploy.v1.TemplateService/UpdateTemplate", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *templateServiceClient) DeleteTemplate(ctx context.Context, in *DeleteTemplateRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, "/opsmesh.deploy.v1.TemplateService/DeleteTemplate", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *templateServiceClient) ListTemplates(ctx context.Context, in *ListTemplatesRequest, opts ...grpc.CallOption) (*ListTemplatesResponse, error) {
	out := new(ListTemplatesResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.deploy.v1.TemplateService/ListTemplates", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RegisterTemplateServiceServer registers the server.
func RegisterTemplateServiceServer(s grpc.ServiceRegistrar, srv TemplateServiceServer) {
	s.RegisterService(&_TemplateService_serviceDesc, srv)
}

func _TemplateService_CreateTemplate_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CreateTemplateRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TemplateServiceServer).CreateTemplate(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.deploy.v1.TemplateService/CreateTemplate",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TemplateServiceServer).CreateTemplate(ctx, req.(*CreateTemplateRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _TemplateService_GetTemplate_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetTemplateRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TemplateServiceServer).GetTemplate(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.deploy.v1.TemplateService/GetTemplate",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TemplateServiceServer).GetTemplate(ctx, req.(*GetTemplateRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _TemplateService_UpdateTemplate_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(UpdateTemplateRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TemplateServiceServer).UpdateTemplate(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.deploy.v1.TemplateService/UpdateTemplate",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TemplateServiceServer).UpdateTemplate(ctx, req.(*UpdateTemplateRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _TemplateService_DeleteTemplate_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DeleteTemplateRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TemplateServiceServer).DeleteTemplate(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.deploy.v1.TemplateService/DeleteTemplate",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TemplateServiceServer).DeleteTemplate(ctx, req.(*DeleteTemplateRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _TemplateService_ListTemplates_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListTemplatesRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TemplateServiceServer).ListTemplates(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.deploy.v1.TemplateService/ListTemplates",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TemplateServiceServer).ListTemplates(ctx, req.(*ListTemplatesRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _TemplateService_serviceDesc = grpc.ServiceDesc{
	ServiceName: "opsmesh.deploy.v1.TemplateService",
	HandlerType: (*TemplateServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "CreateTemplate",
			Handler:    _TemplateService_CreateTemplate_Handler,
		},
		{
			MethodName: "GetTemplate",
			Handler:    _TemplateService_GetTemplate_Handler,
		},
		{
			MethodName: "UpdateTemplate",
			Handler:    _TemplateService_UpdateTemplate_Handler,
		},
		{
			MethodName: "DeleteTemplate",
			Handler:    _TemplateService_DeleteTemplate_Handler,
		},
		{
			MethodName: "ListTemplates",
			Handler:    _TemplateService_ListTemplates_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "api/proto/v1/deploy.proto",
}

// StrategyServiceServer is the server API for StrategyService.
type StrategyServiceServer interface {
	CreateStrategy(context.Context, *CreateStrategyRequest) (*Strategy, error)
	GetStrategy(context.Context, *GetStrategyRequest) (*Strategy, error)
	UpdateStrategy(context.Context, *UpdateStrategyRequest) (*Strategy, error)
	DeleteStrategy(context.Context, *DeleteStrategyRequest) (*emptypb.Empty, error)
	ListStrategies(context.Context, *ListStrategiesRequest) (*ListStrategiesResponse, error)
	mustEmbedUnimplementedStrategyServiceServer()
}

// UnimplementedStrategyServiceServer must be embedded to have forward compatible implementations.
type UnimplementedStrategyServiceServer struct{}

func (UnimplementedStrategyServiceServer) CreateStrategy(context.Context, *CreateStrategyRequest) (*Strategy, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CreateStrategy not implemented")
}
func (UnimplementedStrategyServiceServer) GetStrategy(context.Context, *GetStrategyRequest) (*Strategy, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetStrategy not implemented")
}
func (UnimplementedStrategyServiceServer) UpdateStrategy(context.Context, *UpdateStrategyRequest) (*Strategy, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UpdateStrategy not implemented")
}
func (UnimplementedStrategyServiceServer) DeleteStrategy(context.Context, *DeleteStrategyRequest) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteStrategy not implemented")
}
func (UnimplementedStrategyServiceServer) ListStrategies(context.Context, *ListStrategiesRequest) (*ListStrategiesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListStrategies not implemented")
}
func (UnimplementedStrategyServiceServer) mustEmbedUnimplementedStrategyServiceServer() {}

// UnsafeStrategyServiceServer may be embedded to opt out of forward compatibility.
type UnsafeStrategyServiceServer interface {
	mustEmbedUnimplementedStrategyServiceServer()
}

// StrategyServiceClient is the client API for StrategyService.
type StrategyServiceClient interface {
	CreateStrategy(ctx context.Context, in *CreateStrategyRequest, opts ...grpc.CallOption) (*Strategy, error)
	GetStrategy(ctx context.Context, in *GetStrategyRequest, opts ...grpc.CallOption) (*Strategy, error)
	UpdateStrategy(ctx context.Context, in *UpdateStrategyRequest, opts ...grpc.CallOption) (*Strategy, error)
	DeleteStrategy(ctx context.Context, in *DeleteStrategyRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
	ListStrategies(ctx context.Context, in *ListStrategiesRequest, opts ...grpc.CallOption) (*ListStrategiesResponse, error)
}

type strategyServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewStrategyServiceClient(cc grpc.ClientConnInterface) StrategyServiceClient {
	return &strategyServiceClient{cc: cc}
}

func (c *strategyServiceClient) CreateStrategy(ctx context.Context, in *CreateStrategyRequest, opts ...grpc.CallOption) (*Strategy, error) {
	out := new(Strategy)
	err := c.cc.Invoke(ctx, "/opsmesh.deploy.v1.StrategyService/CreateStrategy", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *strategyServiceClient) GetStrategy(ctx context.Context, in *GetStrategyRequest, opts ...grpc.CallOption) (*Strategy, error) {
	out := new(Strategy)
	err := c.cc.Invoke(ctx, "/opsmesh.deploy.v1.StrategyService/GetStrategy", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *strategyServiceClient) UpdateStrategy(ctx context.Context, in *UpdateStrategyRequest, opts ...grpc.CallOption) (*Strategy, error) {
	out := new(Strategy)
	err := c.cc.Invoke(ctx, "/opsmesh.deploy.v1.StrategyService/UpdateStrategy", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *strategyServiceClient) DeleteStrategy(ctx context.Context, in *DeleteStrategyRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, "/opsmesh.deploy.v1.StrategyService/DeleteStrategy", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *strategyServiceClient) ListStrategies(ctx context.Context, in *ListStrategiesRequest, opts ...grpc.CallOption) (*ListStrategiesResponse, error) {
	out := new(ListStrategiesResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.deploy.v1.StrategyService/ListStrategies", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RegisterStrategyServiceServer registers the server.
func RegisterStrategyServiceServer(s grpc.ServiceRegistrar, srv StrategyServiceServer) {
	s.RegisterService(&_StrategyService_serviceDesc, srv)
}

func _StrategyService_CreateStrategy_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CreateStrategyRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(StrategyServiceServer).CreateStrategy(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.deploy.v1.StrategyService/CreateStrategy",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(StrategyServiceServer).CreateStrategy(ctx, req.(*CreateStrategyRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _StrategyService_GetStrategy_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetStrategyRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(StrategyServiceServer).GetStrategy(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.deploy.v1.StrategyService/GetStrategy",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(StrategyServiceServer).GetStrategy(ctx, req.(*GetStrategyRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _StrategyService_UpdateStrategy_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(UpdateStrategyRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(StrategyServiceServer).UpdateStrategy(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.deploy.v1.StrategyService/UpdateStrategy",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(StrategyServiceServer).UpdateStrategy(ctx, req.(*UpdateStrategyRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _StrategyService_DeleteStrategy_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DeleteStrategyRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(StrategyServiceServer).DeleteStrategy(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.deploy.v1.StrategyService/DeleteStrategy",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(StrategyServiceServer).DeleteStrategy(ctx, req.(*DeleteStrategyRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _StrategyService_ListStrategies_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListStrategiesRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(StrategyServiceServer).ListStrategies(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.deploy.v1.StrategyService/ListStrategies",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(StrategyServiceServer).ListStrategies(ctx, req.(*ListStrategiesRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _StrategyService_serviceDesc = grpc.ServiceDesc{
	ServiceName: "opsmesh.deploy.v1.StrategyService",
	HandlerType: (*StrategyServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "CreateStrategy",
			Handler:    _StrategyService_CreateStrategy_Handler,
		},
		{
			MethodName: "GetStrategy",
			Handler:    _StrategyService_GetStrategy_Handler,
		},
		{
			MethodName: "UpdateStrategy",
			Handler:    _StrategyService_UpdateStrategy_Handler,
		},
		{
			MethodName: "DeleteStrategy",
			Handler:    _StrategyService_DeleteStrategy_Handler,
		},
		{
			MethodName: "ListStrategies",
			Handler:    _StrategyService_ListStrategies_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "api/proto/v1/deploy.proto",
}

// CanaryServiceServer is the server API for CanaryService.
type CanaryServiceServer interface {
	StartCanary(context.Context, *StartCanaryRequest) (*Canary, error)
	GetCanaryStatus(context.Context, *GetCanaryStatusRequest) (*CanaryStatus, error)
	PromoteCanary(context.Context, *PromoteCanaryRequest) (*Canary, error)
	RollbackCanary(context.Context, *RollbackCanaryRequest) (*Canary, error)
	ListCanaries(context.Context, *ListCanariesRequest) (*ListCanariesResponse, error)
	mustEmbedUnimplementedCanaryServiceServer()
}

// UnimplementedCanaryServiceServer must be embedded to have forward compatible implementations.
type UnimplementedCanaryServiceServer struct{}

func (UnimplementedCanaryServiceServer) StartCanary(context.Context, *StartCanaryRequest) (*Canary, error) {
	return nil, status.Errorf(codes.Unimplemented, "method StartCanary not implemented")
}
func (UnimplementedCanaryServiceServer) GetCanaryStatus(context.Context, *GetCanaryStatusRequest) (*CanaryStatus, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetCanaryStatus not implemented")
}
func (UnimplementedCanaryServiceServer) PromoteCanary(context.Context, *PromoteCanaryRequest) (*Canary, error) {
	return nil, status.Errorf(codes.Unimplemented, "method PromoteCanary not implemented")
}
func (UnimplementedCanaryServiceServer) RollbackCanary(context.Context, *RollbackCanaryRequest) (*Canary, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RollbackCanary not implemented")
}
func (UnimplementedCanaryServiceServer) ListCanaries(context.Context, *ListCanariesRequest) (*ListCanariesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListCanaries not implemented")
}
func (UnimplementedCanaryServiceServer) mustEmbedUnimplementedCanaryServiceServer() {}

// UnsafeCanaryServiceServer may be embedded to opt out of forward compatibility.
type UnsafeCanaryServiceServer interface {
	mustEmbedUnimplementedCanaryServiceServer()
}

// CanaryServiceClient is the client API for CanaryService.
type CanaryServiceClient interface {
	StartCanary(ctx context.Context, in *StartCanaryRequest, opts ...grpc.CallOption) (*Canary, error)
	GetCanaryStatus(ctx context.Context, in *GetCanaryStatusRequest, opts ...grpc.CallOption) (*CanaryStatus, error)
	PromoteCanary(ctx context.Context, in *PromoteCanaryRequest, opts ...grpc.CallOption) (*Canary, error)
	RollbackCanary(ctx context.Context, in *RollbackCanaryRequest, opts ...grpc.CallOption) (*Canary, error)
	ListCanaries(ctx context.Context, in *ListCanariesRequest, opts ...grpc.CallOption) (*ListCanariesResponse, error)
}

type canaryServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewCanaryServiceClient(cc grpc.ClientConnInterface) CanaryServiceClient {
	return &canaryServiceClient{cc: cc}
}

func (c *canaryServiceClient) StartCanary(ctx context.Context, in *StartCanaryRequest, opts ...grpc.CallOption) (*Canary, error) {
	out := new(Canary)
	err := c.cc.Invoke(ctx, "/opsmesh.deploy.v1.CanaryService/StartCanary", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *canaryServiceClient) GetCanaryStatus(ctx context.Context, in *GetCanaryStatusRequest, opts ...grpc.CallOption) (*CanaryStatus, error) {
	out := new(CanaryStatus)
	err := c.cc.Invoke(ctx, "/opsmesh.deploy.v1.CanaryService/GetCanaryStatus", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *canaryServiceClient) PromoteCanary(ctx context.Context, in *PromoteCanaryRequest, opts ...grpc.CallOption) (*Canary, error) {
	out := new(Canary)
	err := c.cc.Invoke(ctx, "/opsmesh.deploy.v1.CanaryService/PromoteCanary", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *canaryServiceClient) RollbackCanary(ctx context.Context, in *RollbackCanaryRequest, opts ...grpc.CallOption) (*Canary, error) {
	out := new(Canary)
	err := c.cc.Invoke(ctx, "/opsmesh.deploy.v1.CanaryService/RollbackCanary", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *canaryServiceClient) ListCanaries(ctx context.Context, in *ListCanariesRequest, opts ...grpc.CallOption) (*ListCanariesResponse, error) {
	out := new(ListCanariesResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.deploy.v1.CanaryService/ListCanaries", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RegisterCanaryServiceServer registers the server.
func RegisterCanaryServiceServer(s grpc.ServiceRegistrar, srv CanaryServiceServer) {
	s.RegisterService(&_CanaryService_serviceDesc, srv)
}

func _CanaryService_StartCanary_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(StartCanaryRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CanaryServiceServer).StartCanary(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.deploy.v1.CanaryService/StartCanary",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CanaryServiceServer).StartCanary(ctx, req.(*StartCanaryRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CanaryService_GetCanaryStatus_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetCanaryStatusRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CanaryServiceServer).GetCanaryStatus(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.deploy.v1.CanaryService/GetCanaryStatus",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CanaryServiceServer).GetCanaryStatus(ctx, req.(*GetCanaryStatusRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CanaryService_PromoteCanary_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(PromoteCanaryRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CanaryServiceServer).PromoteCanary(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.deploy.v1.CanaryService/PromoteCanary",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CanaryServiceServer).PromoteCanary(ctx, req.(*PromoteCanaryRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CanaryService_RollbackCanary_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(RollbackCanaryRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CanaryServiceServer).RollbackCanary(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.deploy.v1.CanaryService/RollbackCanary",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CanaryServiceServer).RollbackCanary(ctx, req.(*RollbackCanaryRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CanaryService_ListCanaries_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListCanariesRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CanaryServiceServer).ListCanaries(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.deploy.v1.CanaryService/ListCanaries",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CanaryServiceServer).ListCanaries(ctx, req.(*ListCanariesRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _CanaryService_serviceDesc = grpc.ServiceDesc{
	ServiceName: "opsmesh.deploy.v1.CanaryService",
	HandlerType: (*CanaryServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "StartCanary",
			Handler:    _CanaryService_StartCanary_Handler,
		},
		{
			MethodName: "GetCanaryStatus",
			Handler:    _CanaryService_GetCanaryStatus_Handler,
		},
		{
			MethodName: "PromoteCanary",
			Handler:    _CanaryService_PromoteCanary_Handler,
		},
		{
			MethodName: "RollbackCanary",
			Handler:    _CanaryService_RollbackCanary_Handler,
		},
		{
			MethodName: "ListCanaries",
			Handler:    _CanaryService_ListCanaries_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "api/proto/v1/deploy.proto",
}
