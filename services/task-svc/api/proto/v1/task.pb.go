package taskv1

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Task represents a task.
type Task struct {
	TaskId           string
	AgentId          string
	TenantId         string
	Type             string
	Command          string
	Content          string
	Path             string
	Status           string
	ClaimedBy        string
	ClaimedAt        *timestamppb.Timestamp
	ClaimEpoch       int64
	CreatedAt        *timestamppb.Timestamp
	RetryCount       int32
	MaxRetries       int32
	DeadLetter       bool
	Timeout          int32
	RetryDelay       int32
	Schedule         string
	ParentId         string
	DependsOn        []string
	ApprovalRequired bool
	ApprovedBy       string
	ApprovedAt       *timestamppb.Timestamp
	BatchId          string
}

// TaskResult represents a task execution result.
type TaskResult struct {
	TaskId     string
	AgentId    string
	ExitCode   int32
	Stdout     string
	Stderr     string
	DurationMs int64
	FinishedAt *timestamppb.Timestamp
	ClaimEpoch int64
}

// Schedule represents a scheduled task.
type Schedule struct {
	Id          string
	TenantId    string
	Name        string
	CronExpr    string
	TaskType    string
	Command     string
	Content     string
	Path        string
	AgentId     string
	Enabled     bool
	LastFiredAt *timestamppb.Timestamp
	CreatedAt   *timestamppb.Timestamp
	UpdatedAt   *timestamppb.Timestamp
}

// BatchTask represents a batch task.
type BatchTask struct {
	BatchId      string
	TenantId     string
	Name         string
	TotalCount   int32
	SuccessCount int32
	FailedCount  int32
	PendingCount int32
	Status       string
	CreatedAt    *timestamppb.Timestamp
}

// LogLine represents a log line.
type LogLine struct {
	Timestamp *timestamppb.Timestamp
	Level     string
	Message   string
}

// CreateTaskRequest is the request to create a task.
type CreateTaskRequest struct {
	Task *Task
}

// GetTaskRequest is the request to get a task.
type GetTaskRequest struct {
	TaskId string
}

// ListTasksRequest is the request to list tasks.
type ListTasksRequest struct {
	TenantId string
	Status   string
	AgentId  string
	Limit    int32
}

// ListTasksResponse is the response for listing tasks.
type ListTasksResponse struct {
	Tasks []*Task
}

// ClaimTaskRequest is the request to claim a task.
type ClaimTaskRequest struct {
	AgentId string
}

// ReportResultRequest is the request to report a result.
type ReportResultRequest struct {
	Result *TaskResult
}

// CancelTaskRequest is the request to cancel a task.
type CancelTaskRequest struct {
	TaskId   string
	TenantId string
}

// ApproveTaskRequest is the request to approve a task.
type ApproveTaskRequest struct {
	TaskId     string
	TenantId   string
	ApprovedBy string
}

// RejectTaskRequest is the request to reject a task.
type RejectTaskRequest struct {
	TaskId     string
	TenantId   string
	RejectedBy string
}

// GetTaskStatusRequest is the request to get task status.
type GetTaskStatusRequest struct {
	TaskId string
}

// TaskStatusResponse is the response for task status.
type TaskStatusResponse struct {
	TaskId     string
	Status     string
	ClaimedBy  string
	ClaimEpoch int64
	RetryCount int32
	DeadLetter bool
}

// CreateScheduleRequest is the request to create a schedule.
type CreateScheduleRequest struct {
	Schedule *Schedule
}

// GetScheduleRequest is the request to get a schedule.
type GetScheduleRequest struct {
	Id string
}

// UpdateScheduleRequest is the request to update a schedule.
type UpdateScheduleRequest struct {
	Schedule *Schedule
}

// DeleteScheduleRequest is the request to delete a schedule.
type DeleteScheduleRequest struct {
	Id string
}

// ListSchedulesRequest is the request to list schedules.
type ListSchedulesRequest struct {
	TenantId string
}

// ListSchedulesResponse is the response for listing schedules.
type ListSchedulesResponse struct {
	Schedules []*Schedule
}

// GetTaskResultRequest is the request to get a task result.
type GetTaskResultRequest struct {
	TaskId string
}

// ListTaskResultsRequest is the request to list task results.
type ListTaskResultsRequest struct {
	TenantId string
	AgentId  string
	Limit    int32
}

// ListTaskResultsResponse is the response for listing task results.
type ListTaskResultsResponse struct {
	Results []*TaskResult
}

// GetTaskLogsRequest is the request to get task logs.
type GetTaskLogsRequest struct {
	TaskId string
}

// TaskLogsResponse is the response for task logs.
type TaskLogsResponse struct {
	TaskId string
	Logs   []*LogLine
}

// CreateBatchTaskRequest is the request to create a batch task.
type CreateBatchTaskRequest struct {
	TenantId string
	Name     string
	AgentId  string
	Type     string
	Command  string
	Content  string
	Path     string
	AgentIds []string
}

// GetBatchStatusRequest is the request to get batch status.
type GetBatchStatusRequest struct {
	BatchId string
}

// BatchStatusResponse is the response for batch status.
type BatchStatusResponse struct {
	Batch *BatchTask
	Tasks []*Task
}

// ListBatchTasksRequest is the request to list batch tasks.
type ListBatchTasksRequest struct {
	TenantId string
}

// ListBatchTasksResponse is the response for listing batch tasks.
type ListBatchTasksResponse struct {
	Batches []*BatchTask
}

// TaskServiceServer is the server API for TaskService.
type TaskServiceServer interface {
	CreateTask(context.Context, *CreateTaskRequest) (*Task, error)
	GetTask(context.Context, *GetTaskRequest) (*Task, error)
	ListTasks(context.Context, *ListTasksRequest) (*ListTasksResponse, error)
	ClaimTask(context.Context, *ClaimTaskRequest) (*Task, error)
	ReportResult(context.Context, *ReportResultRequest) (*TaskResult, error)
	CancelTask(context.Context, *CancelTaskRequest) (*emptypb.Empty, error)
	ApproveTask(context.Context, *ApproveTaskRequest) (*Task, error)
	RejectTask(context.Context, *RejectTaskRequest) (*Task, error)
	GetTaskStatus(context.Context, *GetTaskStatusRequest) (*TaskStatusResponse, error)
	mustEmbedUnimplementedTaskServiceServer()
}

// UnimplementedTaskServiceServer must be embedded to have forward compatible implementations.
type UnimplementedTaskServiceServer struct{}

func (UnimplementedTaskServiceServer) CreateTask(context.Context, *CreateTaskRequest) (*Task, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CreateTask not implemented")
}
func (UnimplementedTaskServiceServer) GetTask(context.Context, *GetTaskRequest) (*Task, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetTask not implemented")
}
func (UnimplementedTaskServiceServer) ListTasks(context.Context, *ListTasksRequest) (*ListTasksResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListTasks not implemented")
}
func (UnimplementedTaskServiceServer) ClaimTask(context.Context, *ClaimTaskRequest) (*Task, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ClaimTask not implemented")
}
func (UnimplementedTaskServiceServer) ReportResult(context.Context, *ReportResultRequest) (*TaskResult, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ReportResult not implemented")
}
func (UnimplementedTaskServiceServer) CancelTask(context.Context, *CancelTaskRequest) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CancelTask not implemented")
}
func (UnimplementedTaskServiceServer) ApproveTask(context.Context, *ApproveTaskRequest) (*Task, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ApproveTask not implemented")
}
func (UnimplementedTaskServiceServer) RejectTask(context.Context, *RejectTaskRequest) (*Task, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RejectTask not implemented")
}
func (UnimplementedTaskServiceServer) GetTaskStatus(context.Context, *GetTaskStatusRequest) (*TaskStatusResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetTaskStatus not implemented")
}
func (UnimplementedTaskServiceServer) mustEmbedUnimplementedTaskServiceServer() {}

// UnsafeTaskServiceServer may be embedded to opt out of forward compatibility.
type UnsafeTaskServiceServer interface {
	mustEmbedUnimplementedTaskServiceServer()
}

// TaskServiceClient is the client API for TaskService.
type TaskServiceClient interface {
	CreateTask(ctx context.Context, in *CreateTaskRequest, opts ...grpc.CallOption) (*Task, error)
	GetTask(ctx context.Context, in *GetTaskRequest, opts ...grpc.CallOption) (*Task, error)
	ListTasks(ctx context.Context, in *ListTasksRequest, opts ...grpc.CallOption) (*ListTasksResponse, error)
	ClaimTask(ctx context.Context, in *ClaimTaskRequest, opts ...grpc.CallOption) (*Task, error)
	ReportResult(ctx context.Context, in *ReportResultRequest, opts ...grpc.CallOption) (*TaskResult, error)
	CancelTask(ctx context.Context, in *CancelTaskRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
	ApproveTask(ctx context.Context, in *ApproveTaskRequest, opts ...grpc.CallOption) (*Task, error)
	RejectTask(ctx context.Context, in *RejectTaskRequest, opts ...grpc.CallOption) (*Task, error)
	GetTaskStatus(ctx context.Context, in *GetTaskStatusRequest, opts ...grpc.CallOption) (*TaskStatusResponse, error)
}

type taskServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewTaskServiceClient(cc grpc.ClientConnInterface) TaskServiceClient {
	return &taskServiceClient{cc: cc}
}

func (c *taskServiceClient) CreateTask(ctx context.Context, in *CreateTaskRequest, opts ...grpc.CallOption) (*Task, error) {
	out := new(Task)
	err := c.cc.Invoke(ctx, "/opsmesh.task.v1.TaskService/CreateTask", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *taskServiceClient) GetTask(ctx context.Context, in *GetTaskRequest, opts ...grpc.CallOption) (*Task, error) {
	out := new(Task)
	err := c.cc.Invoke(ctx, "/opsmesh.task.v1.TaskService/GetTask", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *taskServiceClient) ListTasks(ctx context.Context, in *ListTasksRequest, opts ...grpc.CallOption) (*ListTasksResponse, error) {
	out := new(ListTasksResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.task.v1.TaskService/ListTasks", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *taskServiceClient) ClaimTask(ctx context.Context, in *ClaimTaskRequest, opts ...grpc.CallOption) (*Task, error) {
	out := new(Task)
	err := c.cc.Invoke(ctx, "/opsmesh.task.v1.TaskService/ClaimTask", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *taskServiceClient) ReportResult(ctx context.Context, in *ReportResultRequest, opts ...grpc.CallOption) (*TaskResult, error) {
	out := new(TaskResult)
	err := c.cc.Invoke(ctx, "/opsmesh.task.v1.TaskService/ReportResult", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *taskServiceClient) CancelTask(ctx context.Context, in *CancelTaskRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, "/opsmesh.task.v1.TaskService/CancelTask", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *taskServiceClient) ApproveTask(ctx context.Context, in *ApproveTaskRequest, opts ...grpc.CallOption) (*Task, error) {
	out := new(Task)
	err := c.cc.Invoke(ctx, "/opsmesh.task.v1.TaskService/ApproveTask", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *taskServiceClient) RejectTask(ctx context.Context, in *RejectTaskRequest, opts ...grpc.CallOption) (*Task, error) {
	out := new(Task)
	err := c.cc.Invoke(ctx, "/opsmesh.task.v1.TaskService/RejectTask", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *taskServiceClient) GetTaskStatus(ctx context.Context, in *GetTaskStatusRequest, opts ...grpc.CallOption) (*TaskStatusResponse, error) {
	out := new(TaskStatusResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.task.v1.TaskService/GetTaskStatus", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RegisterTaskServiceServer registers the server.
func RegisterTaskServiceServer(s grpc.ServiceRegistrar, srv TaskServiceServer) {
	s.RegisterService(&_TaskService_serviceDesc, srv)
}

func _TaskService_CreateTask_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CreateTaskRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TaskServiceServer).CreateTask(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.task.v1.TaskService/CreateTask",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TaskServiceServer).CreateTask(ctx, req.(*CreateTaskRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _TaskService_GetTask_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetTaskRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TaskServiceServer).GetTask(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.task.v1.TaskService/GetTask",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TaskServiceServer).GetTask(ctx, req.(*GetTaskRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _TaskService_ListTasks_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListTasksRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TaskServiceServer).ListTasks(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.task.v1.TaskService/ListTasks",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TaskServiceServer).ListTasks(ctx, req.(*ListTasksRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _TaskService_ClaimTask_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ClaimTaskRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TaskServiceServer).ClaimTask(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.task.v1.TaskService/ClaimTask",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TaskServiceServer).ClaimTask(ctx, req.(*ClaimTaskRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _TaskService_ReportResult_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ReportResultRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TaskServiceServer).ReportResult(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.task.v1.TaskService/ReportResult",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TaskServiceServer).ReportResult(ctx, req.(*ReportResultRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _TaskService_CancelTask_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CancelTaskRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TaskServiceServer).CancelTask(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.task.v1.TaskService/CancelTask",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TaskServiceServer).CancelTask(ctx, req.(*CancelTaskRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _TaskService_ApproveTask_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ApproveTaskRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TaskServiceServer).ApproveTask(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.task.v1.TaskService/ApproveTask",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TaskServiceServer).ApproveTask(ctx, req.(*ApproveTaskRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _TaskService_RejectTask_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(RejectTaskRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TaskServiceServer).RejectTask(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.task.v1.TaskService/RejectTask",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TaskServiceServer).RejectTask(ctx, req.(*RejectTaskRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _TaskService_GetTaskStatus_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetTaskStatusRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TaskServiceServer).GetTaskStatus(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.task.v1.TaskService/GetTaskStatus",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TaskServiceServer).GetTaskStatus(ctx, req.(*GetTaskStatusRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _TaskService_serviceDesc = grpc.ServiceDesc{
	ServiceName: "opsmesh.task.v1.TaskService",
	HandlerType: (*TaskServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "CreateTask",
			Handler:    _TaskService_CreateTask_Handler,
		},
		{
			MethodName: "GetTask",
			Handler:    _TaskService_GetTask_Handler,
		},
		{
			MethodName: "ListTasks",
			Handler:    _TaskService_ListTasks_Handler,
		},
		{
			MethodName: "ClaimTask",
			Handler:    _TaskService_ClaimTask_Handler,
		},
		{
			MethodName: "ReportResult",
			Handler:    _TaskService_ReportResult_Handler,
		},
		{
			MethodName: "CancelTask",
			Handler:    _TaskService_CancelTask_Handler,
		},
		{
			MethodName: "ApproveTask",
			Handler:    _TaskService_ApproveTask_Handler,
		},
		{
			MethodName: "RejectTask",
			Handler:    _TaskService_RejectTask_Handler,
		},
		{
			MethodName: "GetTaskStatus",
			Handler:    _TaskService_GetTaskStatus_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "api/proto/v1/task.proto",
}

// ScheduleServiceServer is the server API for ScheduleService.
type ScheduleServiceServer interface {
	CreateSchedule(context.Context, *CreateScheduleRequest) (*Schedule, error)
	GetSchedule(context.Context, *GetScheduleRequest) (*Schedule, error)
	UpdateSchedule(context.Context, *UpdateScheduleRequest) (*Schedule, error)
	DeleteSchedule(context.Context, *DeleteScheduleRequest) (*emptypb.Empty, error)
	ListSchedules(context.Context, *ListSchedulesRequest) (*ListSchedulesResponse, error)
	mustEmbedUnimplementedScheduleServiceServer()
}

// UnimplementedScheduleServiceServer must be embedded to have forward compatible implementations.
type UnimplementedScheduleServiceServer struct{}

func (UnimplementedScheduleServiceServer) CreateSchedule(context.Context, *CreateScheduleRequest) (*Schedule, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CreateSchedule not implemented")
}
func (UnimplementedScheduleServiceServer) GetSchedule(context.Context, *GetScheduleRequest) (*Schedule, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetSchedule not implemented")
}
func (UnimplementedScheduleServiceServer) UpdateSchedule(context.Context, *UpdateScheduleRequest) (*Schedule, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UpdateSchedule not implemented")
}
func (UnimplementedScheduleServiceServer) DeleteSchedule(context.Context, *DeleteScheduleRequest) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteSchedule not implemented")
}
func (UnimplementedScheduleServiceServer) ListSchedules(context.Context, *ListSchedulesRequest) (*ListSchedulesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListSchedules not implemented")
}
func (UnimplementedScheduleServiceServer) mustEmbedUnimplementedScheduleServiceServer() {}

// UnsafeScheduleServiceServer may be embedded to opt out of forward compatibility.
type UnsafeScheduleServiceServer interface {
	mustEmbedUnimplementedScheduleServiceServer()
}

// ScheduleServiceClient is the client API for ScheduleService.
type ScheduleServiceClient interface {
	CreateSchedule(ctx context.Context, in *CreateScheduleRequest, opts ...grpc.CallOption) (*Schedule, error)
	GetSchedule(ctx context.Context, in *GetScheduleRequest, opts ...grpc.CallOption) (*Schedule, error)
	UpdateSchedule(ctx context.Context, in *UpdateScheduleRequest, opts ...grpc.CallOption) (*Schedule, error)
	DeleteSchedule(ctx context.Context, in *DeleteScheduleRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
	ListSchedules(ctx context.Context, in *ListSchedulesRequest, opts ...grpc.CallOption) (*ListSchedulesResponse, error)
}

type scheduleServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewScheduleServiceClient(cc grpc.ClientConnInterface) ScheduleServiceClient {
	return &scheduleServiceClient{cc: cc}
}

func (c *scheduleServiceClient) CreateSchedule(ctx context.Context, in *CreateScheduleRequest, opts ...grpc.CallOption) (*Schedule, error) {
	out := new(Schedule)
	err := c.cc.Invoke(ctx, "/opsmesh.task.v1.ScheduleService/CreateSchedule", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *scheduleServiceClient) GetSchedule(ctx context.Context, in *GetScheduleRequest, opts ...grpc.CallOption) (*Schedule, error) {
	out := new(Schedule)
	err := c.cc.Invoke(ctx, "/opsmesh.task.v1.ScheduleService/GetSchedule", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *scheduleServiceClient) UpdateSchedule(ctx context.Context, in *UpdateScheduleRequest, opts ...grpc.CallOption) (*Schedule, error) {
	out := new(Schedule)
	err := c.cc.Invoke(ctx, "/opsmesh.task.v1.ScheduleService/UpdateSchedule", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *scheduleServiceClient) DeleteSchedule(ctx context.Context, in *DeleteScheduleRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, "/opsmesh.task.v1.ScheduleService/DeleteSchedule", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *scheduleServiceClient) ListSchedules(ctx context.Context, in *ListSchedulesRequest, opts ...grpc.CallOption) (*ListSchedulesResponse, error) {
	out := new(ListSchedulesResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.task.v1.ScheduleService/ListSchedules", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RegisterScheduleServiceServer registers the server.
func RegisterScheduleServiceServer(s grpc.ServiceRegistrar, srv ScheduleServiceServer) {
	s.RegisterService(&_ScheduleService_serviceDesc, srv)
}

func _ScheduleService_CreateSchedule_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CreateScheduleRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ScheduleServiceServer).CreateSchedule(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.task.v1.ScheduleService/CreateSchedule",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ScheduleServiceServer).CreateSchedule(ctx, req.(*CreateScheduleRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ScheduleService_GetSchedule_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetScheduleRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ScheduleServiceServer).GetSchedule(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.task.v1.ScheduleService/GetSchedule",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ScheduleServiceServer).GetSchedule(ctx, req.(*GetScheduleRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ScheduleService_UpdateSchedule_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(UpdateScheduleRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ScheduleServiceServer).UpdateSchedule(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.task.v1.ScheduleService/UpdateSchedule",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ScheduleServiceServer).UpdateSchedule(ctx, req.(*UpdateScheduleRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ScheduleService_DeleteSchedule_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DeleteScheduleRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ScheduleServiceServer).DeleteSchedule(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.task.v1.ScheduleService/DeleteSchedule",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ScheduleServiceServer).DeleteSchedule(ctx, req.(*DeleteScheduleRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ScheduleService_ListSchedules_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListSchedulesRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ScheduleServiceServer).ListSchedules(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.task.v1.ScheduleService/ListSchedules",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ScheduleServiceServer).ListSchedules(ctx, req.(*ListSchedulesRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _ScheduleService_serviceDesc = grpc.ServiceDesc{
	ServiceName: "opsmesh.task.v1.ScheduleService",
	HandlerType: (*ScheduleServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "CreateSchedule",
			Handler:    _ScheduleService_CreateSchedule_Handler,
		},
		{
			MethodName: "GetSchedule",
			Handler:    _ScheduleService_GetSchedule_Handler,
		},
		{
			MethodName: "UpdateSchedule",
			Handler:    _ScheduleService_UpdateSchedule_Handler,
		},
		{
			MethodName: "DeleteSchedule",
			Handler:    _ScheduleService_DeleteSchedule_Handler,
		},
		{
			MethodName: "ListSchedules",
			Handler:    _ScheduleService_ListSchedules_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "api/proto/v1/task.proto",
}

// ResultServiceServer is the server API for ResultService.
type ResultServiceServer interface {
	GetTaskResult(context.Context, *GetTaskResultRequest) (*TaskResult, error)
	ListTaskResults(context.Context, *ListTaskResultsRequest) (*ListTaskResultsResponse, error)
	GetTaskLogs(context.Context, *GetTaskLogsRequest) (*TaskLogsResponse, error)
	mustEmbedUnimplementedResultServiceServer()
}

// UnimplementedResultServiceServer must be embedded to have forward compatible implementations.
type UnimplementedResultServiceServer struct{}

func (UnimplementedResultServiceServer) GetTaskResult(context.Context, *GetTaskResultRequest) (*TaskResult, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetTaskResult not implemented")
}
func (UnimplementedResultServiceServer) ListTaskResults(context.Context, *ListTaskResultsRequest) (*ListTaskResultsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListTaskResults not implemented")
}
func (UnimplementedResultServiceServer) GetTaskLogs(context.Context, *GetTaskLogsRequest) (*TaskLogsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetTaskLogs not implemented")
}
func (UnimplementedResultServiceServer) mustEmbedUnimplementedResultServiceServer() {}

// UnsafeResultServiceServer may be embedded to opt out of forward compatibility.
type UnsafeResultServiceServer interface {
	mustEmbedUnimplementedResultServiceServer()
}

// ResultServiceClient is the client API for ResultService.
type ResultServiceClient interface {
	GetTaskResult(ctx context.Context, in *GetTaskResultRequest, opts ...grpc.CallOption) (*TaskResult, error)
	ListTaskResults(ctx context.Context, in *ListTaskResultsRequest, opts ...grpc.CallOption) (*ListTaskResultsResponse, error)
	GetTaskLogs(ctx context.Context, in *GetTaskLogsRequest, opts ...grpc.CallOption) (*TaskLogsResponse, error)
}

type resultServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewResultServiceClient(cc grpc.ClientConnInterface) ResultServiceClient {
	return &resultServiceClient{cc: cc}
}

func (c *resultServiceClient) GetTaskResult(ctx context.Context, in *GetTaskResultRequest, opts ...grpc.CallOption) (*TaskResult, error) {
	out := new(TaskResult)
	err := c.cc.Invoke(ctx, "/opsmesh.task.v1.ResultService/GetTaskResult", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *resultServiceClient) ListTaskResults(ctx context.Context, in *ListTaskResultsRequest, opts ...grpc.CallOption) (*ListTaskResultsResponse, error) {
	out := new(ListTaskResultsResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.task.v1.ResultService/ListTaskResults", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *resultServiceClient) GetTaskLogs(ctx context.Context, in *GetTaskLogsRequest, opts ...grpc.CallOption) (*TaskLogsResponse, error) {
	out := new(TaskLogsResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.task.v1.ResultService/GetTaskLogs", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RegisterResultServiceServer registers the server.
func RegisterResultServiceServer(s grpc.ServiceRegistrar, srv ResultServiceServer) {
	s.RegisterService(&_ResultService_serviceDesc, srv)
}

func _ResultService_GetTaskResult_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetTaskResultRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ResultServiceServer).GetTaskResult(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.task.v1.ResultService/GetTaskResult",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ResultServiceServer).GetTaskResult(ctx, req.(*GetTaskResultRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ResultService_ListTaskResults_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListTaskResultsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ResultServiceServer).ListTaskResults(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.task.v1.ResultService/ListTaskResults",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ResultServiceServer).ListTaskResults(ctx, req.(*ListTaskResultsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ResultService_GetTaskLogs_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetTaskLogsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ResultServiceServer).GetTaskLogs(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.task.v1.ResultService/GetTaskLogs",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ResultServiceServer).GetTaskLogs(ctx, req.(*GetTaskLogsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _ResultService_serviceDesc = grpc.ServiceDesc{
	ServiceName: "opsmesh.task.v1.ResultService",
	HandlerType: (*ResultServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "GetTaskResult",
			Handler:    _ResultService_GetTaskResult_Handler,
		},
		{
			MethodName: "ListTaskResults",
			Handler:    _ResultService_ListTaskResults_Handler,
		},
		{
			MethodName: "GetTaskLogs",
			Handler:    _ResultService_GetTaskLogs_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "api/proto/v1/task.proto",
}

// BatchServiceServer is the server API for BatchService.
type BatchServiceServer interface {
	CreateBatchTask(context.Context, *CreateBatchTaskRequest) (*BatchTask, error)
	GetBatchStatus(context.Context, *GetBatchStatusRequest) (*BatchStatusResponse, error)
	ListBatchTasks(context.Context, *ListBatchTasksRequest) (*ListBatchTasksResponse, error)
	mustEmbedUnimplementedBatchServiceServer()
}

// UnimplementedBatchServiceServer must be embedded to have forward compatible implementations.
type UnimplementedBatchServiceServer struct{}

func (UnimplementedBatchServiceServer) CreateBatchTask(context.Context, *CreateBatchTaskRequest) (*BatchTask, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CreateBatchTask not implemented")
}
func (UnimplementedBatchServiceServer) GetBatchStatus(context.Context, *GetBatchStatusRequest) (*BatchStatusResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetBatchStatus not implemented")
}
func (UnimplementedBatchServiceServer) ListBatchTasks(context.Context, *ListBatchTasksRequest) (*ListBatchTasksResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListBatchTasks not implemented")
}
func (UnimplementedBatchServiceServer) mustEmbedUnimplementedBatchServiceServer() {}

// UnsafeBatchServiceServer may be embedded to opt out of forward compatibility.
type UnsafeBatchServiceServer interface {
	mustEmbedUnimplementedBatchServiceServer()
}

// BatchServiceClient is the client API for BatchService.
type BatchServiceClient interface {
	CreateBatchTask(ctx context.Context, in *CreateBatchTaskRequest, opts ...grpc.CallOption) (*BatchTask, error)
	GetBatchStatus(ctx context.Context, in *GetBatchStatusRequest, opts ...grpc.CallOption) (*BatchStatusResponse, error)
	ListBatchTasks(ctx context.Context, in *ListBatchTasksRequest, opts ...grpc.CallOption) (*ListBatchTasksResponse, error)
}

type batchServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewBatchServiceClient(cc grpc.ClientConnInterface) BatchServiceClient {
	return &batchServiceClient{cc: cc}
}

func (c *batchServiceClient) CreateBatchTask(ctx context.Context, in *CreateBatchTaskRequest, opts ...grpc.CallOption) (*BatchTask, error) {
	out := new(BatchTask)
	err := c.cc.Invoke(ctx, "/opsmesh.task.v1.BatchService/CreateBatchTask", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *batchServiceClient) GetBatchStatus(ctx context.Context, in *GetBatchStatusRequest, opts ...grpc.CallOption) (*BatchStatusResponse, error) {
	out := new(BatchStatusResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.task.v1.BatchService/GetBatchStatus", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *batchServiceClient) ListBatchTasks(ctx context.Context, in *ListBatchTasksRequest, opts ...grpc.CallOption) (*ListBatchTasksResponse, error) {
	out := new(ListBatchTasksResponse)
	err := c.cc.Invoke(ctx, "/opsmesh.task.v1.BatchService/ListBatchTasks", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RegisterBatchServiceServer registers the server.
func RegisterBatchServiceServer(s grpc.ServiceRegistrar, srv BatchServiceServer) {
	s.RegisterService(&_BatchService_serviceDesc, srv)
}

func _BatchService_CreateBatchTask_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CreateBatchTaskRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(BatchServiceServer).CreateBatchTask(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.task.v1.BatchService/CreateBatchTask",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(BatchServiceServer).CreateBatchTask(ctx, req.(*CreateBatchTaskRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _BatchService_GetBatchStatus_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetBatchStatusRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(BatchServiceServer).GetBatchStatus(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.task.v1.BatchService/GetBatchStatus",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(BatchServiceServer).GetBatchStatus(ctx, req.(*GetBatchStatusRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _BatchService_ListBatchTasks_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListBatchTasksRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(BatchServiceServer).ListBatchTasks(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opsmesh.task.v1.BatchService/ListBatchTasks",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(BatchServiceServer).ListBatchTasks(ctx, req.(*ListBatchTasksRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _BatchService_serviceDesc = grpc.ServiceDesc{
	ServiceName: "opsmesh.task.v1.BatchService",
	HandlerType: (*BatchServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "CreateBatchTask",
			Handler:    _BatchService_CreateBatchTask_Handler,
		},
		{
			MethodName: "GetBatchStatus",
			Handler:    _BatchService_GetBatchStatus_Handler,
		},
		{
			MethodName: "ListBatchTasks",
			Handler:    _BatchService_ListBatchTasks_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "api/proto/v1/task.proto",
}
