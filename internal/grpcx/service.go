package grpcx

import (
	"context"

	"google.golang.org/grpc"

	"opsmesh/internal/proto"
)

// 以下是 gRPC 四方法的消息类型。
// 复用 proto.AgentInfo / proto.Task / proto.TaskResult（已是 JSON tag 友好结构体，
// jsonCodec 可直接序列化），其余为本次通道新增的轻量信封。

// RegisterResp 控制面注册返回的响应：分配 agentID + 控制面下发的配置。
type RegisterResp struct {
	AgentID       string         `json:"agentID"`
	ControlConfig map[string]int `json:"controlConfig"` // heartbeatInterval / taskPollInterval
}

// HeartbeatReq agent 心跳请求。
type HeartbeatReq struct {
	AgentID    string            `json:"agentID"`
	Status     string            `json:"status"`
	Load       int               `json:"load"`
	CmdbReport *proto.CmdbReport `json:"cmdbReport,omitempty"` // CMDB 增量上报（cmdb 选填）
}

// Empty 无业务负载的空响应（心跳/上报结果用）。
type Empty struct{}

// PullTasksReq 拉取任务的请求。
type PullTasksReq struct {
	AgentID string `json:"agentID"`
}

// PullTasksResp 拉取任务的响应。
type PullTasksResp struct {
	Tasks []proto.Task `json:"tasks"`
}

// CancelTaskReq 取消任务的请求（F3）：控制面/网关发起，按 taskID 取消。
// TenantID 由服务端用网关注入身份强制覆盖（防止越权取消他租户任务）。
type CancelTaskReq struct {
	TaskID   string `json:"taskID"`
	TenantID string `json:"tenantID"`
}

// PollCancelsReq agent 轮询取消信号：携带 agentID，请控制面返回本 agent 当前
// 处于 cancelled 状态的任务 ID 列表（F3 取消信号真正下达到 agent worker）。
type PollCancelsReq struct {
	AgentID string `json:"agentID"`
}

// PollCancelsResp 控制面返回的已取消任务 ID 列表。
type PollCancelsResp struct {
	CancelledTaskIDs []string `json:"cancelledTaskIDs"`
}

// RegistrationServer 是 gRPC 注册通道的服务端接口，由控制面实现。
// 四个方法一一对应 agent↔控制面 的 注册 / 心跳 / 拉任务 / 上报结果。
type RegistrationServer interface {
	Register(ctx context.Context, req *proto.AgentInfo) (*RegisterResp, error)
	Heartbeat(ctx context.Context, req *HeartbeatReq) (*Empty, error)
	PullTasks(ctx context.Context, req *PullTasksReq) (*PullTasksResp, error)
	ReportResult(ctx context.Context, req *proto.TaskResult) (*Empty, error)
	CancelTask(ctx context.Context, req *CancelTaskReq) (*Empty, error)
	PollCancels(ctx context.Context, req *PollCancelsReq) (*PollCancelsResp, error)
}

// Registration_ServiceDesc 手写 ServiceDesc，无需 protoc 生成。
// 服务名 opsmesh.v1.Registration（带版本前缀，破坏性变更可灰度，P2-3），四个一元方法，无流式方法。
var Registration_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "opsmesh.v1.Registration",
	HandlerType: (*RegistrationServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "Register", Handler: _Registration_Register_Handler},
		{MethodName: "Heartbeat", Handler: _Registration_Heartbeat_Handler},
		{MethodName: "PullTasks", Handler: _Registration_PullTasks_Handler},
		{MethodName: "ReportResult", Handler: _Registration_ReportResult_Handler},
		{MethodName: "CancelTask", Handler: _Registration_CancelTask_Handler},
		{MethodName: "PollCancels", Handler: _Registration_PollCancels_Handler},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "opsmesh/registration",
}

// ---- 四个方法的服务端分发 Handler（标准 gRPC 一元 Handler 写法）----

func _Registration_Register_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(proto.AgentInfo)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(RegistrationServer).Register(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.v1.Registration/Register",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(RegistrationServer).Register(ctx, req.(*proto.AgentInfo))
	}
	return interceptor(ctx, in, info, handler)
}

func _Registration_Heartbeat_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(HeartbeatReq)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(RegistrationServer).Heartbeat(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.v1.Registration/Heartbeat",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(RegistrationServer).Heartbeat(ctx, req.(*HeartbeatReq))
	}
	return interceptor(ctx, in, info, handler)
}

func _Registration_PullTasks_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(PullTasksReq)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(RegistrationServer).PullTasks(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.v1.Registration/PullTasks",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(RegistrationServer).PullTasks(ctx, req.(*PullTasksReq))
	}
	return interceptor(ctx, in, info, handler)
}

func _Registration_ReportResult_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(proto.TaskResult)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(RegistrationServer).ReportResult(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.v1.Registration/ReportResult",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(RegistrationServer).ReportResult(ctx, req.(*proto.TaskResult))
	}
	return interceptor(ctx, in, info, handler)
}

func _Registration_CancelTask_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CancelTaskReq)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(RegistrationServer).CancelTask(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.v1.Registration/CancelTask",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(RegistrationServer).CancelTask(ctx, req.(*CancelTaskReq))
	}
	return interceptor(ctx, in, info, handler)
}

func _Registration_PollCancels_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(PollCancelsReq)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(RegistrationServer).PollCancels(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.v1.Registration/PollCancels",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(RegistrationServer).PollCancels(ctx, req.(*PollCancelsReq))
	}
	return interceptor(ctx, in, info, handler)
}
