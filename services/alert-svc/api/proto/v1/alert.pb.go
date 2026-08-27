package alertv1

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// AlertRule represents an alert rule.
type AlertRule struct {
	Id        string
	TenantId  string
	Name      string
	Metric    string
	Op        string
	Threshold float64
	Duration  int32
	Severity  string
	Channels  []string
	Enabled   bool
	CreatedAt *timestamppb.Timestamp
	UpdatedAt *timestamppb.Timestamp
}

// Alert represents an alert event.
type Alert struct {
	Id        string
	TenantId  string
	RuleId    string
	RuleName  string
	Severity  string
	Message   string
	Values    map[string]float64
	Status    string
	FiredAt   *timestamppb.Timestamp
	UpdatedAt *timestamppb.Timestamp
}

// CreateRuleRequest is the request to create a rule.
type CreateRuleRequest struct {
	Rule *AlertRule
}

// GetRuleRequest is the request to get a rule.
type GetRuleRequest struct {
	Id string
}

// UpdateRuleRequest is the request to update a rule.
type UpdateRuleRequest struct {
	Rule *AlertRule
}

// DeleteRuleRequest is the request to delete a rule.
type DeleteRuleRequest struct {
	Id string
}

// ListRulesResponse is the response for listing rules.
type ListRulesResponse struct {
	Rules []*AlertRule
}

// EvaluateRequest is the request to evaluate metrics.
type EvaluateRequest struct {
	TenantId string
	DeviceId string
	Metrics  map[string]float64
}

// EvaluateResponse is the response for evaluation.
type EvaluateResponse struct {
	Alerts []*Alert
}

// GetAlertRequest is the request to get an alert.
type GetAlertRequest struct {
	Id string
}

// ListAlertsRequest is the request to list alerts.
type ListAlertsRequest struct {
	TenantId string
	Status   string
	Limit    int32
}

// ListAlertsResponse is the response for listing alerts.
type ListAlertsResponse struct {
	Alerts []*Alert
}

// AckAlertRequest is the request to acknowledge an alert.
type AckAlertRequest struct {
	Id string
}

// SilenceAlertRequest is the request to silence an alert.
type SilenceAlertRequest struct {
	Id              string
	DurationMinutes int32
	Comment         string
}

// AlertServiceServer is the server API for AlertService.
type AlertServiceServer interface {
	CreateRule(context.Context, *CreateRuleRequest) (*AlertRule, error)
	GetRule(context.Context, *GetRuleRequest) (*AlertRule, error)
	ListRules(context.Context, *emptypb.Empty) (*ListRulesResponse, error)
	UpdateRule(context.Context, *UpdateRuleRequest) (*AlertRule, error)
	DeleteRule(context.Context, *DeleteRuleRequest) (*emptypb.Empty, error)
	Evaluate(context.Context, *EvaluateRequest) (*EvaluateResponse, error)
	GetAlert(context.Context, *GetAlertRequest) (*Alert, error)
	ListAlerts(context.Context, *ListAlertsRequest) (*ListAlertsResponse, error)
	AckAlert(context.Context, *AckAlertRequest) (*emptypb.Empty, error)
	SilenceAlert(context.Context, *SilenceAlertRequest) (*emptypb.Empty, error)
	mustEmbedUnimplementedAlertServiceServer()
}

// UnimplementedAlertServiceServer must be embedded to have forward compatible implementations.
type UnimplementedAlertServiceServer struct{}

func (UnimplementedAlertServiceServer) CreateRule(context.Context, *CreateRuleRequest) (*AlertRule, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CreateRule not implemented")
}
func (UnimplementedAlertServiceServer) GetRule(context.Context, *GetRuleRequest) (*AlertRule, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetRule not implemented")
}
func (UnimplementedAlertServiceServer) ListRules(context.Context, *emptypb.Empty) (*ListRulesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListRules not implemented")
}
func (UnimplementedAlertServiceServer) UpdateRule(context.Context, *UpdateRuleRequest) (*AlertRule, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UpdateRule not implemented")
}
func (UnimplementedAlertServiceServer) DeleteRule(context.Context, *DeleteRuleRequest) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteRule not implemented")
}
func (UnimplementedAlertServiceServer) Evaluate(context.Context, *EvaluateRequest) (*EvaluateResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Evaluate not implemented")
}
func (UnimplementedAlertServiceServer) GetAlert(context.Context, *GetAlertRequest) (*Alert, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetAlert not implemented")
}
func (UnimplementedAlertServiceServer) ListAlerts(context.Context, *ListAlertsRequest) (*ListAlertsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListAlerts not implemented")
}
func (UnimplementedAlertServiceServer) AckAlert(context.Context, *AckAlertRequest) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method AckAlert not implemented")
}
func (UnimplementedAlertServiceServer) SilenceAlert(context.Context, *SilenceAlertRequest) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SilenceAlert not implemented")
}
func (UnimplementedAlertServiceServer) mustEmbedUnimplementedAlertServiceServer() {}

// UnsafeAlertServiceServer may be embedded to opt out of forward compatibility.
type UnsafeAlertServiceServer interface {
	mustEmbedUnimplementedAlertServiceServer()
}

// AlertServiceClient is the client API for AlertService.
type AlertServiceClient interface {
	CreateRule(ctx context.Context, in *CreateRuleRequest, opts ...grpc.CallOption) (*AlertRule, error)
	GetRule(ctx context.Context, in *GetRuleRequest, opts ...grpc.CallOption) (*AlertRule, error)
	ListRules(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*ListRulesResponse, error)
	UpdateRule(ctx context.Context, in *UpdateRuleRequest, opts ...grpc.CallOption) (*AlertRule, error)
	DeleteRule(ctx context.Context, in *DeleteRuleRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
	Evaluate(ctx context.Context, in *EvaluateRequest, opts ...grpc.CallOption) (*EvaluateResponse, error)
	GetAlert(ctx context.Context, in *GetAlertRequest, opts ...grpc.CallOption) (*Alert, error)
	ListAlerts(ctx context.Context, in *ListAlertsRequest, opts ...grpc.CallOption) (*ListAlertsResponse, error)
	AckAlert(ctx context.Context, in *AckAlertRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
	SilenceAlert(ctx context.Context, in *SilenceAlertRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
}

type alertServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewAlertServiceClient(cc grpc.ClientConnInterface) AlertServiceClient {
	return &alertServiceClient{cc: cc}
}

func (c *alertServiceClient) CreateRule(ctx context.Context, in *CreateRuleRequest, opts ...grpc.CallOption) (*AlertRule, error) {
	out := new(AlertRule)
	err := c.cc.Invoke(ctx, "/opsmesh.alert.v1.AlertService/CreateRule", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *alertServiceClient) GetRule(ctx context.Context, in *GetRuleRequest, opts ...grpc.CallOption) (*AlertRule, error) {
	out := new(AlertRule)
	err := c.cc.Invoke(ctx, "/opsmesh.alert.v1.AlertService/GetRule", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *alertServiceClient) ListRules(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*ListRulesResponse, error) {
	out := new(ListRulesResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.alert.v1.AlertService/ListRules", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *alertServiceClient) UpdateRule(ctx context.Context, in *UpdateRuleRequest, opts ...grpc.CallOption) (*AlertRule, error) {
	out := new(AlertRule)
	err := c.cc.Invoke(ctx, "/opsmesh.alert.v1.AlertService/UpdateRule", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *alertServiceClient) DeleteRule(ctx context.Context, in *DeleteRuleRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, "/opsmesh.alert.v1.AlertService/DeleteRule", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *alertServiceClient) Evaluate(ctx context.Context, in *EvaluateRequest, opts ...grpc.CallOption) (*EvaluateResponse, error) {
	out := new(EvaluateResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.alert.v1.AlertService/Evaluate", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *alertServiceClient) GetAlert(ctx context.Context, in *GetAlertRequest, opts ...grpc.CallOption) (*Alert, error) {
	out := new(Alert)
	err := c.cc.Invoke(ctx, "/opsmesh.alert.v1.AlertService/GetAlert", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *alertServiceClient) ListAlerts(ctx context.Context, in *ListAlertsRequest, opts ...grpc.CallOption) (*ListAlertsResponse, error) {
	out := new(ListAlertsResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.alert.v1.AlertService/ListAlerts", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *alertServiceClient) AckAlert(ctx context.Context, in *AckAlertRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, "/opsmesh.alert.v1.AlertService/AckAlert", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *alertServiceClient) SilenceAlert(ctx context.Context, in *SilenceAlertRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, "/opsmesh.alert.v1.AlertService/SilenceAlert", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RegisterAlertServiceServer registers the server.
func RegisterAlertServiceServer(s grpc.ServiceRegistrar, srv AlertServiceServer) {
	s.RegisterService(&_AlertService_serviceDesc, srv)
}

func _AlertService_CreateRule_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CreateRuleRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AlertServiceServer).CreateRule(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.alert.v1.AlertService/CreateRule",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AlertServiceServer).CreateRule(ctx, req.(*CreateRuleRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _AlertService_GetRule_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetRuleRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AlertServiceServer).GetRule(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.alert.v1.AlertService/GetRule",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AlertServiceServer).GetRule(ctx, req.(*GetRuleRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _AlertService_ListRules_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(emptypb.Empty)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AlertServiceServer).ListRules(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.alert.v1.AlertService/ListRules",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AlertServiceServer).ListRules(ctx, req.(*emptypb.Empty))
	}
	return interceptor(ctx, in, info, handler)
}

func _AlertService_UpdateRule_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(UpdateRuleRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AlertServiceServer).UpdateRule(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.alert.v1.AlertService/UpdateRule",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AlertServiceServer).UpdateRule(ctx, req.(*UpdateRuleRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _AlertService_DeleteRule_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DeleteRuleRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AlertServiceServer).DeleteRule(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.alert.v1.AlertService/DeleteRule",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AlertServiceServer).DeleteRule(ctx, req.(*DeleteRuleRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _AlertService_Evaluate_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(EvaluateRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AlertServiceServer).Evaluate(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.alert.v1.AlertService/Evaluate",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AlertServiceServer).Evaluate(ctx, req.(*EvaluateRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _AlertService_GetAlert_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetAlertRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AlertServiceServer).GetAlert(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.alert.v1.AlertService/GetAlert",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AlertServiceServer).GetAlert(ctx, req.(*GetAlertRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _AlertService_ListAlerts_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListAlertsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AlertServiceServer).ListAlerts(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.alert.v1.AlertService/ListAlerts",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AlertServiceServer).ListAlerts(ctx, req.(*ListAlertsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _AlertService_AckAlert_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(AckAlertRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AlertServiceServer).AckAlert(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.alert.v1.AlertService/AckAlert",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AlertServiceServer).AckAlert(ctx, req.(*AckAlertRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _AlertService_SilenceAlert_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(SilenceAlertRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AlertServiceServer).SilenceAlert(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.alert.v1.AlertService/SilenceAlert",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AlertServiceServer).SilenceAlert(ctx, req.(*SilenceAlertRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _AlertService_serviceDesc = grpc.ServiceDesc{
	ServiceName: "opsmesh.alert.v1.AlertService",
	HandlerType: (*AlertServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "CreateRule",
			Handler:    _AlertService_CreateRule_Handler,
		},
		{
			MethodName: "GetRule",
			Handler:    _AlertService_GetRule_Handler,
		},
		{
			MethodName: "ListRules",
			Handler:    _AlertService_ListRules_Handler,
		},
		{
			MethodName: "UpdateRule",
			Handler:    _AlertService_UpdateRule_Handler,
		},
		{
			MethodName: "DeleteRule",
			Handler:    _AlertService_DeleteRule_Handler,
		},
		{
			MethodName: "Evaluate",
			Handler:    _AlertService_Evaluate_Handler,
		},
		{
			MethodName: "GetAlert",
			Handler:    _AlertService_GetAlert_Handler,
		},
		{
			MethodName: "ListAlerts",
			Handler:    _AlertService_ListAlerts_Handler,
		},
		{
			MethodName: "AckAlert",
			Handler:    _AlertService_AckAlert_Handler,
		},
		{
			MethodName: "SilenceAlert",
			Handler:    _AlertService_SilenceAlert_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "api/proto/v1/alert.proto",
}
