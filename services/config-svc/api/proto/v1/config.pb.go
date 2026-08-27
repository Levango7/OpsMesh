package configv1

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ConfigItem represents a configuration entry.
type ConfigItem struct {
	Id          string
	TenantId    string
	Key         string
	Value       string
	Format      string
	Version     int32
	Description string
	UpdatedBy   string
	CreatedAt   *timestamppb.Timestamp
	UpdatedAt   *timestamppb.Timestamp
}

// SecretItem represents a secret (internal, contains plaintext value).
type SecretItem struct {
	Id        string
	TenantId  string
	Key       string
	Value     string
	KeyType   string
	Version   int32
	CreatedAt *timestamppb.Timestamp
	UpdatedAt *timestamppb.Timestamp
}

// SecretMeta represents secret metadata (external, no value).
type SecretMeta struct {
	Id        string
	TenantId  string
	Key       string
	KeyType   string
	Version   int32
	CreatedAt *timestamppb.Timestamp
	UpdatedAt *timestamppb.Timestamp
}

// NotifyChannel represents a notification channel.
type NotifyChannel struct {
	Id        string
	TenantId  string
	Name      string
	Type      string
	Config    string
	Enabled   bool
	CreatedAt *timestamppb.Timestamp
	UpdatedAt *timestamppb.Timestamp
}

// ConfigTemplate represents a configuration template.
type ConfigTemplate struct {
	Id          string
	TenantId    string
	Name        string
	Description string
	Content     string
	Variables   map[string]string
	CreatedAt   *timestamppb.Timestamp
	UpdatedAt   *timestamppb.Timestamp
}

// GetConfigRequest is the request to get a config.
type GetConfigRequest struct {
	TenantId string
	Key      string
}

// SetConfigRequest is the request to set a config.
type SetConfigRequest struct {
	TenantId    string
	Key         string
	Value       string
	Format      string
	Description string
	UpdatedBy   string
}

// DeleteConfigRequest is the request to delete a config.
type DeleteConfigRequest struct {
	TenantId string
	Key      string
}

// ListConfigsRequest is the request to list configs.
type ListConfigsRequest struct {
	TenantId string
}

// ListConfigsResponse is the response for listing configs.
type ListConfigsResponse struct {
	Configs []*ConfigItem
}

// GetConfigHistoryRequest is the request for config history.
type GetConfigHistoryRequest struct {
	TenantId string
	Key      string
}

// GetConfigHistoryResponse is the response for config history.
type GetConfigHistoryResponse struct {
	History []*ConfigItem
}

// RollbackConfigRequest is the request to rollback a config.
type RollbackConfigRequest struct {
	TenantId  string
	Key       string
	Version   int32
	UpdatedBy string
}

// CreateSecretRequest is the request to create a secret.
type CreateSecretRequest struct {
	TenantId string
	Key      string
	Value    string
	KeyType  string
}

// GetSecretRequest is the request to get a secret.
type GetSecretRequest struct {
	TenantId string
	Key      string
}

// UpdateSecretRequest is the request to update a secret.
type UpdateSecretRequest struct {
	TenantId string
	Key      string
	Value    string
	KeyType  string
}

// DeleteSecretRequest is the request to delete a secret.
type DeleteSecretRequest struct {
	TenantId string
	Key      string
}

// ListSecretsRequest is the request to list secrets.
type ListSecretsRequest struct {
	TenantId string
}

// ListSecretsResponse is the response for listing secrets.
type ListSecretsResponse struct {
	Secrets []*SecretMeta
}

// RotateSecretRequest is the request to rotate a secret.
type RotateSecretRequest struct {
	TenantId  string
	Key       string
	NewValue  string
}

// CreateChannelRequest is the request to create a channel.
type CreateChannelRequest struct {
	TenantId string
	Name     string
	Type     string
	Config   string
	Enabled  bool
}

// GetChannelRequest is the request to get a channel.
type GetChannelRequest struct {
	Id string
}

// UpdateChannelRequest is the request to update a channel.
type UpdateChannelRequest struct {
	Id      string
	Name    string
	Type    string
	Config  string
	Enabled bool
}

// DeleteChannelRequest is the request to delete a channel.
type DeleteChannelRequest struct {
	Id       string
	TenantId string
}

// ListChannelsRequest is the request to list channels.
type ListChannelsRequest struct {
	TenantId string
}

// ListChannelsResponse is the response for listing channels.
type ListChannelsResponse struct {
	Channels []*NotifyChannel
}

// TestChannelRequest is the request to test a channel.
type TestChannelRequest struct {
	Id string
}

// TestChannelResponse is the response for testing a channel.
type TestChannelResponse struct {
	Success bool
	Message string
}

// CreateTemplateRequest is the request to create a template.
type CreateTemplateRequest struct {
	TenantId    string
	Name        string
	Description string
	Content     string
	Variables   map[string]string
}

// GetTemplateRequest is the request to get a template.
type GetTemplateRequest struct {
	Id string
}

// UpdateTemplateRequest is the request to update a template.
type UpdateTemplateRequest struct {
	Id          string
	Name        string
	Description string
	Content     string
	Variables   map[string]string
}

// DeleteTemplateRequest is the request to delete a template.
type DeleteTemplateRequest struct {
	Id       string
	TenantId string
}

// ListTemplatesRequest is the request to list templates.
type ListTemplatesRequest struct {
	TenantId string
}

// ListTemplatesResponse is the response for listing templates.
type ListTemplatesResponse struct {
	Templates []*ConfigTemplate
}

// ApplyTemplateRequest is the request to apply a template.
type ApplyTemplateRequest struct {
	Id        string
	Variables map[string]string
}

// ApplyTemplateResponse is the response for applying a template.
type ApplyTemplateResponse struct {
	RenderedContent string
}

// ConfigServiceServer is the server API for ConfigService.
type ConfigServiceServer interface {
	GetConfig(context.Context, *GetConfigRequest) (*ConfigItem, error)
	SetConfig(context.Context, *SetConfigRequest) (*ConfigItem, error)
	DeleteConfig(context.Context, *DeleteConfigRequest) (*emptypb.Empty, error)
	ListConfigs(context.Context, *ListConfigsRequest) (*ListConfigsResponse, error)
	GetConfigHistory(context.Context, *GetConfigHistoryRequest) (*GetConfigHistoryResponse, error)
	RollbackConfig(context.Context, *RollbackConfigRequest) (*ConfigItem, error)
	mustEmbedUnimplementedConfigServiceServer()
}

// UnimplementedConfigServiceServer must be embedded to have forward compatible implementations.
type UnimplementedConfigServiceServer struct{}

func (UnimplementedConfigServiceServer) GetConfig(context.Context, *GetConfigRequest) (*ConfigItem, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetConfig not implemented")
}
func (UnimplementedConfigServiceServer) SetConfig(context.Context, *SetConfigRequest) (*ConfigItem, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SetConfig not implemented")
}
func (UnimplementedConfigServiceServer) DeleteConfig(context.Context, *DeleteConfigRequest) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteConfig not implemented")
}
func (UnimplementedConfigServiceServer) ListConfigs(context.Context, *ListConfigsRequest) (*ListConfigsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListConfigs not implemented")
}
func (UnimplementedConfigServiceServer) GetConfigHistory(context.Context, *GetConfigHistoryRequest) (*GetConfigHistoryResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetConfigHistory not implemented")
}
func (UnimplementedConfigServiceServer) RollbackConfig(context.Context, *RollbackConfigRequest) (*ConfigItem, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RollbackConfig not implemented")
}
func (UnimplementedConfigServiceServer) mustEmbedUnimplementedConfigServiceServer() {}

// UnsafeConfigServiceServer may be embedded to opt out of forward compatibility.
type UnsafeConfigServiceServer interface {
	mustEmbedUnimplementedConfigServiceServer()
}

// ConfigServiceClient is the client API for ConfigService.
type ConfigServiceClient interface {
	GetConfig(ctx context.Context, in *GetConfigRequest, opts ...grpc.CallOption) (*ConfigItem, error)
	SetConfig(ctx context.Context, in *SetConfigRequest, opts ...grpc.CallOption) (*ConfigItem, error)
	DeleteConfig(ctx context.Context, in *DeleteConfigRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
	ListConfigs(ctx context.Context, in *ListConfigsRequest, opts ...grpc.CallOption) (*ListConfigsResponse, error)
	GetConfigHistory(ctx context.Context, in *GetConfigHistoryRequest, opts ...grpc.CallOption) (*GetConfigHistoryResponse, error)
	RollbackConfig(ctx context.Context, in *RollbackConfigRequest, opts ...grpc.CallOption) (*ConfigItem, error)
}

type configServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewConfigServiceClient(cc grpc.ClientConnInterface) ConfigServiceClient {
	return &configServiceClient{cc: cc}
}

func (c *configServiceClient) GetConfig(ctx context.Context, in *GetConfigRequest, opts ...grpc.CallOption) (*ConfigItem, error) {
	out := new(ConfigItem)
	err := c.cc.Invoke(ctx, "/opsmesh.config.v1.ConfigService/GetConfig", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *configServiceClient) SetConfig(ctx context.Context, in *SetConfigRequest, opts ...grpc.CallOption) (*ConfigItem, error) {
	out := new(ConfigItem)
	err := c.cc.Invoke(ctx, "/opsmesh.config.v1.ConfigService/SetConfig", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *configServiceClient) DeleteConfig(ctx context.Context, in *DeleteConfigRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, "/opsmesh.config.v1.ConfigService/DeleteConfig", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *configServiceClient) ListConfigs(ctx context.Context, in *ListConfigsRequest, opts ...grpc.CallOption) (*ListConfigsResponse, error) {
	out := new(ListConfigsResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.config.v1.ConfigService/ListConfigs", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *configServiceClient) GetConfigHistory(ctx context.Context, in *GetConfigHistoryRequest, opts ...grpc.CallOption) (*GetConfigHistoryResponse, error) {
	out := new(GetConfigHistoryResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.config.v1.ConfigService/GetConfigHistory", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *configServiceClient) RollbackConfig(ctx context.Context, in *RollbackConfigRequest, opts ...grpc.CallOption) (*ConfigItem, error) {
	out := new(ConfigItem)
	err := c.cc.Invoke(ctx, "/opsmesh.config.v1.ConfigService/RollbackConfig", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RegisterConfigServiceServer registers the server.
func RegisterConfigServiceServer(s grpc.ServiceRegistrar, srv ConfigServiceServer) {
	s.RegisterService(&_ConfigService_serviceDesc, srv)
}

func _ConfigService_GetConfig_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetConfigRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ConfigServiceServer).GetConfig(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.config.v1.ConfigService/GetConfig",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ConfigServiceServer).GetConfig(ctx, req.(*GetConfigRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ConfigService_SetConfig_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(SetConfigRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ConfigServiceServer).SetConfig(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.config.v1.ConfigService/SetConfig",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ConfigServiceServer).SetConfig(ctx, req.(*SetConfigRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ConfigService_DeleteConfig_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DeleteConfigRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ConfigServiceServer).DeleteConfig(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.config.v1.ConfigService/DeleteConfig",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ConfigServiceServer).DeleteConfig(ctx, req.(*DeleteConfigRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ConfigService_ListConfigs_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListConfigsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ConfigServiceServer).ListConfigs(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.config.v1.ConfigService/ListConfigs",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ConfigServiceServer).ListConfigs(ctx, req.(*ListConfigsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ConfigService_GetConfigHistory_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetConfigHistoryRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ConfigServiceServer).GetConfigHistory(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.config.v1.ConfigService/GetConfigHistory",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ConfigServiceServer).GetConfigHistory(ctx, req.(*GetConfigHistoryRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ConfigService_RollbackConfig_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(RollbackConfigRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ConfigServiceServer).RollbackConfig(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.config.v1.ConfigService/RollbackConfig",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ConfigServiceServer).RollbackConfig(ctx, req.(*RollbackConfigRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _ConfigService_serviceDesc = grpc.ServiceDesc{
	ServiceName: "opsmesh.config.v1.ConfigService",
	HandlerType: (*ConfigServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "GetConfig",
			Handler:    _ConfigService_GetConfig_Handler,
		},
		{
			MethodName: "SetConfig",
			Handler:    _ConfigService_SetConfig_Handler,
		},
		{
			MethodName: "DeleteConfig",
			Handler:    _ConfigService_DeleteConfig_Handler,
		},
		{
			MethodName: "ListConfigs",
			Handler:    _ConfigService_ListConfigs_Handler,
		},
		{
			MethodName: "GetConfigHistory",
			Handler:    _ConfigService_GetConfigHistory_Handler,
		},
		{
			MethodName: "RollbackConfig",
			Handler:    _ConfigService_RollbackConfig_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "api/proto/v1/config.proto",
}

// SecretServiceServer is the server API for SecretService.
type SecretServiceServer interface {
	CreateSecret(context.Context, *CreateSecretRequest) (*SecretMeta, error)
	GetSecret(context.Context, *GetSecretRequest) (*SecretItem, error)
	UpdateSecret(context.Context, *UpdateSecretRequest) (*SecretMeta, error)
	DeleteSecret(context.Context, *DeleteSecretRequest) (*emptypb.Empty, error)
	ListSecrets(context.Context, *ListSecretsRequest) (*ListSecretsResponse, error)
	RotateSecret(context.Context, *RotateSecretRequest) (*SecretMeta, error)
	mustEmbedUnimplementedSecretServiceServer()
}

// UnimplementedSecretServiceServer must be embedded to have forward compatible implementations.
type UnimplementedSecretServiceServer struct{}

func (UnimplementedSecretServiceServer) CreateSecret(context.Context, *CreateSecretRequest) (*SecretMeta, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CreateSecret not implemented")
}
func (UnimplementedSecretServiceServer) GetSecret(context.Context, *GetSecretRequest) (*SecretItem, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetSecret not implemented")
}
func (UnimplementedSecretServiceServer) UpdateSecret(context.Context, *UpdateSecretRequest) (*SecretMeta, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UpdateSecret not implemented")
}
func (UnimplementedSecretServiceServer) DeleteSecret(context.Context, *DeleteSecretRequest) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteSecret not implemented")
}
func (UnimplementedSecretServiceServer) ListSecrets(context.Context, *ListSecretsRequest) (*ListSecretsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListSecrets not implemented")
}
func (UnimplementedSecretServiceServer) RotateSecret(context.Context, *RotateSecretRequest) (*SecretMeta, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RotateSecret not implemented")
}
func (UnimplementedSecretServiceServer) mustEmbedUnimplementedSecretServiceServer() {}

// UnsafeSecretServiceServer may be embedded to opt out of forward compatibility.
type UnsafeSecretServiceServer interface {
	mustEmbedUnimplementedSecretServiceServer()
}

// SecretServiceClient is the client API for SecretService.
type SecretServiceClient interface {
	CreateSecret(ctx context.Context, in *CreateSecretRequest, opts ...grpc.CallOption) (*SecretMeta, error)
	GetSecret(ctx context.Context, in *GetSecretRequest, opts ...grpc.CallOption) (*SecretItem, error)
	UpdateSecret(ctx context.Context, in *UpdateSecretRequest, opts ...grpc.CallOption) (*SecretMeta, error)
	DeleteSecret(ctx context.Context, in *DeleteSecretRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
	ListSecrets(ctx context.Context, in *ListSecretsRequest, opts ...grpc.CallOption) (*ListSecretsResponse, error)
	RotateSecret(ctx context.Context, in *RotateSecretRequest, opts ...grpc.CallOption) (*SecretMeta, error)
}

type secretServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewSecretServiceClient(cc grpc.ClientConnInterface) SecretServiceClient {
	return &secretServiceClient{cc: cc}
}

func (c *secretServiceClient) CreateSecret(ctx context.Context, in *CreateSecretRequest, opts ...grpc.CallOption) (*SecretMeta, error) {
	out := new(SecretMeta)
	err := c.cc.Invoke(ctx, "/opsmesh.config.v1.SecretService/CreateSecret", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *secretServiceClient) GetSecret(ctx context.Context, in *GetSecretRequest, opts ...grpc.CallOption) (*SecretItem, error) {
	out := new(SecretItem)
	err := c.cc.Invoke(ctx, "/opsmesh.config.v1.SecretService/GetSecret", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *secretServiceClient) UpdateSecret(ctx context.Context, in *UpdateSecretRequest, opts ...grpc.CallOption) (*SecretMeta, error) {
	out := new(SecretMeta)
	err := c.cc.Invoke(ctx, "/opsmesh.config.v1.SecretService/UpdateSecret", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *secretServiceClient) DeleteSecret(ctx context.Context, in *DeleteSecretRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, "/opsmesh.config.v1.SecretService/DeleteSecret", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *secretServiceClient) ListSecrets(ctx context.Context, in *ListSecretsRequest, opts ...grpc.CallOption) (*ListSecretsResponse, error) {
	out := new(ListSecretsResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.config.v1.SecretService/ListSecrets", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *secretServiceClient) RotateSecret(ctx context.Context, in *RotateSecretRequest, opts ...grpc.CallOption) (*SecretMeta, error) {
	out := new(SecretMeta)
	err := c.cc.Invoke(ctx, "/opsmesh.config.v1.SecretService/RotateSecret", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RegisterSecretServiceServer registers the server.
func RegisterSecretServiceServer(s grpc.ServiceRegistrar, srv SecretServiceServer) {
	s.RegisterService(&_SecretService_serviceDesc, srv)
}

func _SecretService_CreateSecret_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CreateSecretRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SecretServiceServer).CreateSecret(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.config.v1.SecretService/CreateSecret",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SecretServiceServer).CreateSecret(ctx, req.(*CreateSecretRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SecretService_GetSecret_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetSecretRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SecretServiceServer).GetSecret(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.config.v1.SecretService/GetSecret",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SecretServiceServer).GetSecret(ctx, req.(*GetSecretRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SecretService_UpdateSecret_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(UpdateSecretRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SecretServiceServer).UpdateSecret(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.config.v1.SecretService/UpdateSecret",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SecretServiceServer).UpdateSecret(ctx, req.(*UpdateSecretRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SecretService_DeleteSecret_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DeleteSecretRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SecretServiceServer).DeleteSecret(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.config.v1.SecretService/DeleteSecret",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SecretServiceServer).DeleteSecret(ctx, req.(*DeleteSecretRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SecretService_ListSecrets_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListSecretsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SecretServiceServer).ListSecrets(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.config.v1.SecretService/ListSecrets",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SecretServiceServer).ListSecrets(ctx, req.(*ListSecretsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SecretService_RotateSecret_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(RotateSecretRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SecretServiceServer).RotateSecret(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.config.v1.SecretService/RotateSecret",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SecretServiceServer).RotateSecret(ctx, req.(*RotateSecretRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _SecretService_serviceDesc = grpc.ServiceDesc{
	ServiceName: "opsmesh.config.v1.SecretService",
	HandlerType: (*SecretServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "CreateSecret",
			Handler:    _SecretService_CreateSecret_Handler,
		},
		{
			MethodName: "GetSecret",
			Handler:    _SecretService_GetSecret_Handler,
		},
		{
			MethodName: "UpdateSecret",
			Handler:    _SecretService_UpdateSecret_Handler,
		},
		{
			MethodName: "DeleteSecret",
			Handler:    _SecretService_DeleteSecret_Handler,
		},
		{
			MethodName: "ListSecrets",
			Handler:    _SecretService_ListSecrets_Handler,
		},
		{
			MethodName: "RotateSecret",
			Handler:    _SecretService_RotateSecret_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "api/proto/v1/config.proto",
}

// NotifyChannelServiceServer is the server API for NotifyChannelService.
type NotifyChannelServiceServer interface {
	CreateChannel(context.Context, *CreateChannelRequest) (*NotifyChannel, error)
	GetChannel(context.Context, *GetChannelRequest) (*NotifyChannel, error)
	UpdateChannel(context.Context, *UpdateChannelRequest) (*NotifyChannel, error)
	DeleteChannel(context.Context, *DeleteChannelRequest) (*emptypb.Empty, error)
	ListChannels(context.Context, *ListChannelsRequest) (*ListChannelsResponse, error)
	TestChannel(context.Context, *TestChannelRequest) (*TestChannelResponse, error)
	mustEmbedUnimplementedNotifyChannelServiceServer()
}

// UnimplementedNotifyChannelServiceServer must be embedded to have forward compatible implementations.
type UnimplementedNotifyChannelServiceServer struct{}

func (UnimplementedNotifyChannelServiceServer) CreateChannel(context.Context, *CreateChannelRequest) (*NotifyChannel, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CreateChannel not implemented")
}
func (UnimplementedNotifyChannelServiceServer) GetChannel(context.Context, *GetChannelRequest) (*NotifyChannel, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetChannel not implemented")
}
func (UnimplementedNotifyChannelServiceServer) UpdateChannel(context.Context, *UpdateChannelRequest) (*NotifyChannel, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UpdateChannel not implemented")
}
func (UnimplementedNotifyChannelServiceServer) DeleteChannel(context.Context, *DeleteChannelRequest) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteChannel not implemented")
}
func (UnimplementedNotifyChannelServiceServer) ListChannels(context.Context, *ListChannelsRequest) (*ListChannelsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListChannels not implemented")
}
func (UnimplementedNotifyChannelServiceServer) TestChannel(context.Context, *TestChannelRequest) (*TestChannelResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method TestChannel not implemented")
}
func (UnimplementedNotifyChannelServiceServer) mustEmbedUnimplementedNotifyChannelServiceServer() {}

// UnsafeNotifyChannelServiceServer may be embedded to opt out of forward compatibility.
type UnsafeNotifyChannelServiceServer interface {
	mustEmbedUnimplementedNotifyChannelServiceServer()
}

// NotifyChannelServiceClient is the client API for NotifyChannelService.
type NotifyChannelServiceClient interface {
	CreateChannel(ctx context.Context, in *CreateChannelRequest, opts ...grpc.CallOption) (*NotifyChannel, error)
	GetChannel(ctx context.Context, in *GetChannelRequest, opts ...grpc.CallOption) (*NotifyChannel, error)
	UpdateChannel(ctx context.Context, in *UpdateChannelRequest, opts ...grpc.CallOption) (*NotifyChannel, error)
	DeleteChannel(ctx context.Context, in *DeleteChannelRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
	ListChannels(ctx context.Context, in *ListChannelsRequest, opts ...grpc.CallOption) (*ListChannelsResponse, error)
	TestChannel(ctx context.Context, in *TestChannelRequest, opts ...grpc.CallOption) (*TestChannelResponse, error)
}

type notifyChannelServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewNotifyChannelServiceClient(cc grpc.ClientConnInterface) NotifyChannelServiceClient {
	return &notifyChannelServiceClient{cc: cc}
}

func (c *notifyChannelServiceClient) CreateChannel(ctx context.Context, in *CreateChannelRequest, opts ...grpc.CallOption) (*NotifyChannel, error) {
	out := new(NotifyChannel)
	err := c.cc.Invoke(ctx, "/opsmesh.config.v1.NotifyChannelService/CreateChannel", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *notifyChannelServiceClient) GetChannel(ctx context.Context, in *GetChannelRequest, opts ...grpc.CallOption) (*NotifyChannel, error) {
	out := new(NotifyChannel)
	err := c.cc.Invoke(ctx, "/opsmesh.config.v1.NotifyChannelService/GetChannel", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *notifyChannelServiceClient) UpdateChannel(ctx context.Context, in *UpdateChannelRequest, opts ...grpc.CallOption) (*NotifyChannel, error) {
	out := new(NotifyChannel)
	err := c.cc.Invoke(ctx, "/opsmesh.config.v1.NotifyChannelService/UpdateChannel", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *notifyChannelServiceClient) DeleteChannel(ctx context.Context, in *DeleteChannelRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, "/opsmesh.config.v1.NotifyChannelService/DeleteChannel", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *notifyChannelServiceClient) ListChannels(ctx context.Context, in *ListChannelsRequest, opts ...grpc.CallOption) (*ListChannelsResponse, error) {
	out := new(ListChannelsResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.config.v1.NotifyChannelService/ListChannels", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *notifyChannelServiceClient) TestChannel(ctx context.Context, in *TestChannelRequest, opts ...grpc.CallOption) (*TestChannelResponse, error) {
	out := new(TestChannelResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.config.v1.NotifyChannelService/TestChannel", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RegisterNotifyChannelServiceServer registers the server.
func RegisterNotifyChannelServiceServer(s grpc.ServiceRegistrar, srv NotifyChannelServiceServer) {
	s.RegisterService(&_NotifyChannelService_serviceDesc, srv)
}

func _NotifyChannelService_CreateChannel_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CreateChannelRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(NotifyChannelServiceServer).CreateChannel(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.config.v1.NotifyChannelService/CreateChannel",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(NotifyChannelServiceServer).CreateChannel(ctx, req.(*CreateChannelRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _NotifyChannelService_GetChannel_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetChannelRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(NotifyChannelServiceServer).GetChannel(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.config.v1.NotifyChannelService/GetChannel",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(NotifyChannelServiceServer).GetChannel(ctx, req.(*GetChannelRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _NotifyChannelService_UpdateChannel_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(UpdateChannelRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(NotifyChannelServiceServer).UpdateChannel(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.config.v1.NotifyChannelService/UpdateChannel",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(NotifyChannelServiceServer).UpdateChannel(ctx, req.(*UpdateChannelRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _NotifyChannelService_DeleteChannel_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DeleteChannelRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(NotifyChannelServiceServer).DeleteChannel(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.config.v1.NotifyChannelService/DeleteChannel",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(NotifyChannelServiceServer).DeleteChannel(ctx, req.(*DeleteChannelRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _NotifyChannelService_ListChannels_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListChannelsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(NotifyChannelServiceServer).ListChannels(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.config.v1.NotifyChannelService/ListChannels",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(NotifyChannelServiceServer).ListChannels(ctx, req.(*ListChannelsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _NotifyChannelService_TestChannel_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(TestChannelRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(NotifyChannelServiceServer).TestChannel(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.config.v1.NotifyChannelService/TestChannel",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(NotifyChannelServiceServer).TestChannel(ctx, req.(*TestChannelRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _NotifyChannelService_serviceDesc = grpc.ServiceDesc{
	ServiceName: "opsmesh.config.v1.NotifyChannelService",
	HandlerType: (*NotifyChannelServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "CreateChannel",
			Handler:    _NotifyChannelService_CreateChannel_Handler,
		},
		{
			MethodName: "GetChannel",
			Handler:    _NotifyChannelService_GetChannel_Handler,
		},
		{
			MethodName: "UpdateChannel",
			Handler:    _NotifyChannelService_UpdateChannel_Handler,
		},
		{
			MethodName: "DeleteChannel",
			Handler:    _NotifyChannelService_DeleteChannel_Handler,
		},
		{
			MethodName: "ListChannels",
			Handler:    _NotifyChannelService_ListChannels_Handler,
		},
		{
			MethodName: "TestChannel",
			Handler:    _NotifyChannelService_TestChannel_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "api/proto/v1/config.proto",
}

// TemplateServiceServer is the server API for TemplateService.
type TemplateServiceServer interface {
	CreateTemplate(context.Context, *CreateTemplateRequest) (*ConfigTemplate, error)
	GetTemplate(context.Context, *GetTemplateRequest) (*ConfigTemplate, error)
	UpdateTemplate(context.Context, *UpdateTemplateRequest) (*ConfigTemplate, error)
	DeleteTemplate(context.Context, *DeleteTemplateRequest) (*emptypb.Empty, error)
	ListTemplates(context.Context, *ListTemplatesRequest) (*ListTemplatesResponse, error)
	ApplyTemplate(context.Context, *ApplyTemplateRequest) (*ApplyTemplateResponse, error)
	mustEmbedUnimplementedTemplateServiceServer()
}

// UnimplementedTemplateServiceServer must be embedded to have forward compatible implementations.
type UnimplementedTemplateServiceServer struct{}

func (UnimplementedTemplateServiceServer) CreateTemplate(context.Context, *CreateTemplateRequest) (*ConfigTemplate, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CreateTemplate not implemented")
}
func (UnimplementedTemplateServiceServer) GetTemplate(context.Context, *GetTemplateRequest) (*ConfigTemplate, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetTemplate not implemented")
}
func (UnimplementedTemplateServiceServer) UpdateTemplate(context.Context, *UpdateTemplateRequest) (*ConfigTemplate, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UpdateTemplate not implemented")
}
func (UnimplementedTemplateServiceServer) DeleteTemplate(context.Context, *DeleteTemplateRequest) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteTemplate not implemented")
}
func (UnimplementedTemplateServiceServer) ListTemplates(context.Context, *ListTemplatesRequest) (*ListTemplatesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListTemplates not implemented")
}
func (UnimplementedTemplateServiceServer) ApplyTemplate(context.Context, *ApplyTemplateRequest) (*ApplyTemplateResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ApplyTemplate not implemented")
}
func (UnimplementedTemplateServiceServer) mustEmbedUnimplementedTemplateServiceServer() {}

// UnsafeTemplateServiceServer may be embedded to opt out of forward compatibility.
type UnsafeTemplateServiceServer interface {
	mustEmbedUnimplementedTemplateServiceServer()
}

// TemplateServiceClient is the client API for TemplateService.
type TemplateServiceClient interface {
	CreateTemplate(ctx context.Context, in *CreateTemplateRequest, opts ...grpc.CallOption) (*ConfigTemplate, error)
	GetTemplate(ctx context.Context, in *GetTemplateRequest, opts ...grpc.CallOption) (*ConfigTemplate, error)
	UpdateTemplate(ctx context.Context, in *UpdateTemplateRequest, opts ...grpc.CallOption) (*ConfigTemplate, error)
	DeleteTemplate(ctx context.Context, in *DeleteTemplateRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
	ListTemplates(ctx context.Context, in *ListTemplatesRequest, opts ...grpc.CallOption) (*ListTemplatesResponse, error)
	ApplyTemplate(ctx context.Context, in *ApplyTemplateRequest, opts ...grpc.CallOption) (*ApplyTemplateResponse, error)
}

type templateServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewTemplateServiceClient(cc grpc.ClientConnInterface) TemplateServiceClient {
	return &templateServiceClient{cc: cc}
}

func (c *templateServiceClient) CreateTemplate(ctx context.Context, in *CreateTemplateRequest, opts ...grpc.CallOption) (*ConfigTemplate, error) {
	out := new(ConfigTemplate)
	err := c.cc.Invoke(ctx, "/opsmesh.config.v1.TemplateService/CreateTemplate", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *templateServiceClient) GetTemplate(ctx context.Context, in *GetTemplateRequest, opts ...grpc.CallOption) (*ConfigTemplate, error) {
	out := new(ConfigTemplate)
	err := c.cc.Invoke(ctx, "/opsmesh.config.v1.TemplateService/GetTemplate", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *templateServiceClient) UpdateTemplate(ctx context.Context, in *UpdateTemplateRequest, opts ...grpc.CallOption) (*ConfigTemplate, error) {
	out := new(ConfigTemplate)
	err := c.cc.Invoke(ctx, "/opsmesh.config.v1.TemplateService/UpdateTemplate", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *templateServiceClient) DeleteTemplate(ctx context.Context, in *DeleteTemplateRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, "/opsmesh.config.v1.TemplateService/DeleteTemplate", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *templateServiceClient) ListTemplates(ctx context.Context, in *ListTemplatesRequest, opts ...grpc.CallOption) (*ListTemplatesResponse, error) {
	out := new(ListTemplatesResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.config.v1.TemplateService/ListTemplates", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *templateServiceClient) ApplyTemplate(ctx context.Context, in *ApplyTemplateRequest, opts ...grpc.CallOption) (*ApplyTemplateResponse, error) {
	out := new(ApplyTemplateResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.config.v1.TemplateService/ApplyTemplate", in, out, opts...)
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
		FullMethod: "/opsmesh.config.v1.TemplateService/CreateTemplate",
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
		FullMethod: "/opsmesh.config.v1.TemplateService/GetTemplate",
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
		FullMethod: "/opsmesh.config.v1.TemplateService/UpdateTemplate",
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
		FullMethod: "/opsmesh.config.v1.TemplateService/DeleteTemplate",
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
		FullMethod: "/opsmesh.config.v1.TemplateService/ListTemplates",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TemplateServiceServer).ListTemplates(ctx, req.(*ListTemplatesRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _TemplateService_ApplyTemplate_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ApplyTemplateRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TemplateServiceServer).ApplyTemplate(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.config.v1.TemplateService/ApplyTemplate",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TemplateServiceServer).ApplyTemplate(ctx, req.(*ApplyTemplateRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _TemplateService_serviceDesc = grpc.ServiceDesc{
	ServiceName: "opsmesh.config.v1.TemplateService",
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
		{
			MethodName: "ApplyTemplate",
			Handler:    _TemplateService_ApplyTemplate_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "api/proto/v1/config.proto",
}
