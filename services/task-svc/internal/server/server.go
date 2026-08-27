package server

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	taskv1 "github.com/Levango7/OpsMesh/services/task-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/service"
)

// Server implements the gRPC services.
type Server struct {
	taskv1.UnimplementedTaskServiceServer
	taskv1.UnimplementedScheduleServiceServer
	taskv1.UnimplementedResultServiceServer
	taskv1.UnimplementedBatchServiceServer
	svc *service.Service
}

// NewServer creates a new gRPC server.
func NewServer(svc *service.Service) *Server {
	return &Server{svc: svc}
}

// TaskService methods

func (s *Server) CreateTask(ctx context.Context, req *taskv1.CreateTaskRequest) (*taskv1.Task, error) {
	task, err := s.svc.CreateTask(ctx, req)
	if err != nil {
		if err == service.ErrTaskInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return task, nil
}

func (s *Server) GetTask(ctx context.Context, req *taskv1.GetTaskRequest) (*taskv1.Task, error) {
	task, err := s.svc.GetTask(ctx, req)
	if err != nil {
		if err == service.ErrTaskNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return task, nil
}

func (s *Server) ListTasks(ctx context.Context, req *taskv1.ListTasksRequest) (*taskv1.ListTasksResponse, error) {
	return s.svc.ListTasks(ctx, req)
}

func (s *Server) ClaimTask(ctx context.Context, req *taskv1.ClaimTaskRequest) (*taskv1.Task, error) {
	task, err := s.svc.ClaimTask(ctx, req)
	if err != nil {
		if err == service.ErrTaskInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		if err == service.ErrTaskNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return task, nil
}

func (s *Server) ReportResult(ctx context.Context, req *taskv1.ReportResultRequest) (*taskv1.TaskResult, error) {
	result, err := s.svc.ReportResult(ctx, req)
	if err != nil {
		if err == service.ErrTaskInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		if err == service.ErrClaimEpochMismatch {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return result, nil
}

func (s *Server) CancelTask(ctx context.Context, req *taskv1.CancelTaskRequest) (*emptypb.Empty, error) {
	if err := s.svc.CancelTask(ctx, req); err != nil {
		if err == service.ErrTaskInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		if err == service.ErrTaskNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &emptypb.Empty{}, nil
}

func (s *Server) ApproveTask(ctx context.Context, req *taskv1.ApproveTaskRequest) (*taskv1.Task, error) {
	task, err := s.svc.ApproveTask(ctx, req)
	if err != nil {
		if err == service.ErrTaskInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		if err == service.ErrTaskNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return task, nil
}

func (s *Server) RejectTask(ctx context.Context, req *taskv1.RejectTaskRequest) (*taskv1.Task, error) {
	task, err := s.svc.RejectTask(ctx, req)
	if err != nil {
		if err == service.ErrTaskInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		if err == service.ErrTaskNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return task, nil
}

func (s *Server) GetTaskStatus(ctx context.Context, req *taskv1.GetTaskStatusRequest) (*taskv1.TaskStatusResponse, error) {
	resp, err := s.svc.GetTaskStatus(ctx, req)
	if err != nil {
		if err == service.ErrTaskNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return resp, nil
}

// ScheduleService methods

func (s *Server) CreateSchedule(ctx context.Context, req *taskv1.CreateScheduleRequest) (*taskv1.Schedule, error) {
	sched, err := s.svc.CreateSchedule(ctx, req)
	if err != nil {
		if err == service.ErrTaskInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return sched, nil
}

func (s *Server) GetSchedule(ctx context.Context, req *taskv1.GetScheduleRequest) (*taskv1.Schedule, error) {
	sched, err := s.svc.GetSchedule(ctx, req)
	if err != nil {
		if err == service.ErrScheduleNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return sched, nil
}

func (s *Server) UpdateSchedule(ctx context.Context, req *taskv1.UpdateScheduleRequest) (*taskv1.Schedule, error) {
	sched, err := s.svc.UpdateSchedule(ctx, req)
	if err != nil {
		if err == service.ErrTaskInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		if err == service.ErrScheduleNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return sched, nil
}

func (s *Server) DeleteSchedule(ctx context.Context, req *taskv1.DeleteScheduleRequest) (*emptypb.Empty, error) {
	if err := s.svc.DeleteSchedule(ctx, req); err != nil {
		if err == service.ErrScheduleNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &emptypb.Empty{}, nil
}

func (s *Server) ListSchedules(ctx context.Context, req *taskv1.ListSchedulesRequest) (*taskv1.ListSchedulesResponse, error) {
	return s.svc.ListSchedules(ctx, req)
}

// ResultService methods

func (s *Server) GetTaskResult(ctx context.Context, req *taskv1.GetTaskResultRequest) (*taskv1.TaskResult, error) {
	result, err := s.svc.GetTaskResult(ctx, req)
	if err != nil {
		if err == service.ErrResultNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return result, nil
}

func (s *Server) ListTaskResults(ctx context.Context, req *taskv1.ListTaskResultsRequest) (*taskv1.ListTaskResultsResponse, error) {
	return s.svc.ListTaskResults(ctx, req)
}

func (s *Server) GetTaskLogs(ctx context.Context, req *taskv1.GetTaskLogsRequest) (*taskv1.TaskLogsResponse, error) {
	return s.svc.GetTaskLogs(ctx, req)
}

// BatchService methods

func (s *Server) CreateBatchTask(ctx context.Context, req *taskv1.CreateBatchTaskRequest) (*taskv1.BatchTask, error) {
	batch, err := s.svc.CreateBatchTask(ctx, req)
	if err != nil {
		if err == service.ErrTaskInvalid {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return batch, nil
}

func (s *Server) GetBatchStatus(ctx context.Context, req *taskv1.GetBatchStatusRequest) (*taskv1.BatchStatusResponse, error) {
	resp, err := s.svc.GetBatchStatus(ctx, req)
	if err != nil {
		if err == service.ErrBatchNotFound {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return resp, nil
}

func (s *Server) ListBatchTasks(ctx context.Context, req *taskv1.ListBatchTasksRequest) (*taskv1.ListBatchTasksResponse, error) {
	return s.svc.ListBatchTasks(ctx, req)
}
