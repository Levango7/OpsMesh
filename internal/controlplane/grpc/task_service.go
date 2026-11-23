package grpc

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
	"github.com/Levango7/OpsMesh/internal/taskvalidate"
	taskv1 "github.com/Levango7/OpsMesh/services/task-svc/api/proto/v1"
)

// ShouldEmbedTaskService 判断控制面是否应在本进程提供 TaskService。
// 未配置外部地址或地址就是控制面自身 gRPC 端口时嵌入；指向独立 task-svc 时不嵌入。
func ShouldEmbedTaskService(cfg *config.Config) bool {
	if cfg == nil || strings.TrimSpace(cfg.TaskSvcGRPCAddr) == "" {
		return true
	}
	host, port, err := net.SplitHostPort(strings.TrimSpace(cfg.TaskSvcGRPCAddr))
	if err != nil || port != strconv.Itoa(cfg.GRPCPort) {
		return false
	}
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

var (
	errTaskNotFound = errors.New("task not found")
	errTaskInvalid  = errors.New("task invalid")
)

// TaskServiceImpl 把控制面 store 的 TaskStore 适配为 taskv1.TaskServiceServer。
type TaskServiceImpl struct {
	taskv1.UnimplementedTaskServiceServer
	Store store.Store
	// Agent 复用控制面既有租户、签名、preClaim 和结果副作用实现。
	// 单测可只注入 Store；生产装配必须注入 Agent，不能退回无保护的直写路径。
	Agent *GrpcServerImpl
}

func taskInvalid(err error) error {
	if err == nil {
		err = errTaskInvalid
	}
	return status.Error(codes.InvalidArgument, err.Error())
}

func taskNotFound() error { return status.Error(codes.NotFound, errTaskNotFound.Error()) }

// authorizeManagement 内嵌服务只承担 agent 领取/上报，绝不在 agent 端口新开任务管理面。
// 本机 HTTP 请求走控制面本地 handler；外部管理 RPC 只由独立 task-svc 提供。
// Agent==nil 的直通形态仅供真实 gRPC 适配层单测，不是生产装配入口。
func (s *TaskServiceImpl) authorizeManagement() error {
	if s.Agent != nil {
		return status.Error(codes.Unimplemented, "embedded TaskService only serves agent claim/report")
	}
	return nil
}

// TaskServiceImpl 的 TaskService 适配层：与控制面 HTTP 下发共用同一个 TaskStore。
// 这样 agent 在单机控制面 gRPC 端口即可领取真实任务，不再落到无任务 stub。
// 只有未配置外部 task-svc 时才注册本实现，避免与独立 task-svc 双写队列。
func (s *TaskServiceImpl) CreateTask(ctx context.Context, req *taskv1.CreateTaskRequest) (*taskv1.Task, error) {
	if req == nil || req.Task == nil || req.Task.AgentId == "" {
		return nil, taskInvalid(nil)
	}
	if err := s.authorizeManagement(); err != nil {
		return nil, err
	}
	if req.Task.Type == "" || req.Task.Type == "shell" {
		if err := taskvalidate.ValidateCommand(req.Task.Command); err != nil {
			return nil, taskInvalid(err)
		}
	}
	created, err := s.Store.CreateTask(protoFromTask(req.Task))
	if err != nil {
		return nil, status.Error(codes.Internal, "create task failed")
	}
	return taskToProto(created), nil
}

func (s *TaskServiceImpl) GetTask(ctx context.Context, req *taskv1.GetTaskRequest) (*taskv1.Task, error) {
	if req == nil || req.TaskId == "" {
		return nil, taskInvalid(nil)
	}
	t := s.Store.TaskByID(req.TaskId)
	if t == nil {
		return nil, taskNotFound()
	}
	if err := s.authorizeManagement(); err != nil {
		return nil, err
	}
	return taskToProto(t), nil
}

func (s *TaskServiceImpl) ListTasks(ctx context.Context, req *taskv1.ListTasksRequest) (*taskv1.ListTasksResponse, error) {
	if req == nil {
		return nil, taskInvalid(nil)
	}
	if err := s.authorizeManagement(); err != nil {
		return nil, err
	}
	all := s.Store.AllTasks(req.TenantId)
	out := make([]*taskv1.Task, 0, len(all))
	for _, t := range all {
		if req.Status != "" && t.Status != req.Status {
			continue
		}
		if req.AgentId != "" && t.AgentID != req.AgentId {
			continue
		}
		out = append(out, taskToProto(t))
		if req.Limit > 0 && len(out) >= int(req.Limit) {
			break
		}
	}
	return &taskv1.ListTasksResponse{Tasks: out}, nil
}

func (s *TaskServiceImpl) ClaimTask(ctx context.Context, req *taskv1.ClaimTaskRequest) (*taskv1.Task, error) {
	if req == nil || req.AgentId == "" {
		return nil, taskInvalid(nil)
	}
	var t *proto.Task
	if s.Agent != nil {
		if err := s.Agent.CheckAgentTenant(ctx, req.AgentId); err != nil {
			return nil, err
		}
		if err := s.Agent.verifyAgentSignature(ctx, req.AgentId, req); err != nil {
			return nil, err
		}
		t = s.Agent.svc().ClaimTask(req.AgentId)
		if s.Agent.Metrics != nil {
			s.Agent.Metrics.SetQueueDepth(s.Agent.svc().PendingDepth())
		}
	} else {
		t = s.Store.ClaimTask(req.AgentId)
	}
	if t == nil {
		return nil, taskNotFound()
	}
	return taskToProto(t), nil
}

func (s *TaskServiceImpl) ReportResult(ctx context.Context, req *taskv1.ReportResultRequest) (*taskv1.TaskResult, error) {
	if req == nil || req.Result == nil || req.Result.TaskId == "" || req.Result.AgentId == "" {
		return nil, taskInvalid(nil)
	}
	// 只复制业务字段，不复制 protobuf 内部锁，也不原地修改调用方请求。
	r := &taskv1.TaskResult{
		TaskId: req.Result.TaskId, AgentId: req.Result.AgentId, ExitCode: req.Result.ExitCode,
		Stdout: req.Result.Stdout, Stderr: req.Result.Stderr, DurationMs: req.Result.DurationMs,
		FinishedAt: req.Result.FinishedAt, ClaimEpoch: req.Result.ClaimEpoch,
	}
	if r.FinishedAt == nil {
		r.FinishedAt = timestamppb.Now()
	}
	if s.Agent != nil {
		if err := s.Agent.CheckAgentTenant(ctx, r.AgentId); err != nil {
			return nil, err
		}
		if err := s.Agent.verifyAgentSignature(ctx, r.AgentId, req); err != nil {
			return nil, err
		}
		s.Agent.recordResult(ctx, protoFromResult(r))
	} else {
		s.Store.SubmitResult(protoFromResult(r))
	}
	return r, nil
}

func (s *TaskServiceImpl) CancelTask(ctx context.Context, req *taskv1.CancelTaskRequest) (*emptypb.Empty, error) {
	if req == nil || req.TaskId == "" {
		return nil, taskInvalid(nil)
	}
	if err := s.authorizeManagement(); err != nil {
		return nil, err
	}
	if ok := s.Store.CancelTask(req.TaskId, req.TenantId); !ok {
		return nil, taskNotFound()
	}
	return &emptypb.Empty{}, nil
}

func (s *TaskServiceImpl) ApproveTask(ctx context.Context, req *taskv1.ApproveTaskRequest) (*taskv1.Task, error) {
	if req == nil || req.TaskId == "" {
		return nil, taskInvalid(nil)
	}
	if err := s.authorizeManagement(); err != nil {
		return nil, err
	}
	if ok := s.Store.ApproveTask(req.TaskId, req.TenantId, req.ApprovedBy); !ok {
		return nil, taskNotFound()
	}
	t := s.Store.TaskByID(req.TaskId)
	if t == nil {
		return nil, taskNotFound()
	}
	return taskToProto(t), nil
}

func (s *TaskServiceImpl) RejectTask(ctx context.Context, req *taskv1.RejectTaskRequest) (*taskv1.Task, error) {
	if req == nil || req.TaskId == "" {
		return nil, taskInvalid(nil)
	}
	if err := s.authorizeManagement(); err != nil {
		return nil, err
	}
	if ok := s.Store.RejectTask(req.TaskId, req.TenantId, req.RejectedBy); !ok {
		return nil, taskNotFound()
	}
	t := s.Store.TaskByID(req.TaskId)
	if t == nil {
		return nil, taskNotFound()
	}
	return taskToProto(t), nil
}

func (s *TaskServiceImpl) GetTaskStatus(ctx context.Context, req *taskv1.GetTaskStatusRequest) (*taskv1.TaskStatusResponse, error) {
	if req == nil || req.TaskId == "" {
		return nil, taskInvalid(nil)
	}
	t := s.Store.TaskByID(req.TaskId)
	if t == nil {
		return nil, taskNotFound()
	}
	if err := s.authorizeManagement(); err != nil {
		return nil, err
	}
	return &taskv1.TaskStatusResponse{
		TaskId:     t.TaskID,
		Status:     t.Status,
		ClaimedBy:  t.ClaimedBy,
		ClaimEpoch: t.ClaimEpoch,
		RetryCount: int32(t.RetryCount),
		DeadLetter: t.DeadLetter,
	}, nil
}

func protoFromTask(t *taskv1.Task) *proto.Task {
	return &proto.Task{
		TaskID:           t.TaskId,
		AgentID:          t.AgentId,
		TenantID:         t.TenantId,
		Type:             t.Type,
		Command:          t.Command,
		Content:          t.Content,
		Path:             t.Path,
		Status:           t.Status,
		ClaimedBy:        t.ClaimedBy,
		ClaimedAt:        t.ClaimedAt.AsTime(),
		ClaimEpoch:       t.ClaimEpoch,
		CreatedAt:        t.CreatedAt.AsTime(),
		RetryCount:       int(t.RetryCount),
		MaxRetries:       int(t.MaxRetries),
		DeadLetter:       t.DeadLetter,
		Timeout:          int(t.Timeout),
		RetryDelay:       int(t.RetryDelay),
		Schedule:         t.Schedule,
		ParentID:         t.ParentId,
		DependsOn:        t.DependsOn,
		ApprovalRequired: t.ApprovalRequired,
		ApprovedBy:       t.ApprovedBy,
		ApprovedAt:       t.ApprovedAt.AsTime(),
		BatchID:          t.BatchId,
		LastFiredAt:      t.LastFiredAt.AsTime(),
		Priority:         int(t.Priority),
		IdempotencyKey:   t.IdempotencyKey,
		StatusMachine:    t.StatusMachine,
		ScriptBody:       t.ScriptBody,
	}
}

func taskToProto(t *proto.Task) *taskv1.Task {
	return &taskv1.Task{
		TaskId:           t.TaskID,
		AgentId:          t.AgentID,
		TenantId:         t.TenantID,
		Type:             t.Type,
		Command:          t.Command,
		Content:          t.Content,
		Path:             t.Path,
		Status:           t.Status,
		ClaimedBy:        t.ClaimedBy,
		ClaimedAt:        timestamppb.New(t.ClaimedAt),
		ClaimEpoch:       t.ClaimEpoch,
		CreatedAt:        timestamppb.New(t.CreatedAt),
		RetryCount:       int32(t.RetryCount),
		MaxRetries:       int32(t.MaxRetries),
		DeadLetter:       t.DeadLetter,
		Timeout:          int32(t.Timeout),
		RetryDelay:       int32(t.RetryDelay),
		Schedule:         t.Schedule,
		ParentId:         t.ParentID,
		DependsOn:        t.DependsOn,
		ApprovalRequired: t.ApprovalRequired,
		ApprovedBy:       t.ApprovedBy,
		ApprovedAt:       timestamppb.New(t.ApprovedAt),
		BatchId:          t.BatchID,
		LastFiredAt:      timestamppb.New(t.LastFiredAt),
		Priority:         int32(t.Priority),
		IdempotencyKey:   t.IdempotencyKey,
		StatusMachine:    t.StatusMachine,
		ScriptBody:       t.ScriptBody,
	}
}

func protoFromResult(r *taskv1.TaskResult) *proto.TaskResult {
	return &proto.TaskResult{
		TaskID:     r.TaskId,
		AgentID:    r.AgentId,
		ExitCode:   int(r.ExitCode),
		Stdout:     r.Stdout,
		Stderr:     r.Stderr,
		DurationMs: r.DurationMs,
		FinishedAt: r.FinishedAt.AsTime(),
		ClaimEpoch: r.ClaimEpoch,
	}
}
