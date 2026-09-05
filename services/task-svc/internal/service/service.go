package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	taskv1 "github.com/Levango7/OpsMesh/services/task-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/store"
	"opsmesh/pkg/circuit"
	"opsmesh/pkg/metrics"
)

// Errors returned by the service.
var (
	ErrTaskNotFound       = errors.New("task not found")
	ErrTaskInvalid        = errors.New("task invalid")
	ErrClaimEpochMismatch = errors.New("claim epoch mismatch")
	ErrScheduleNotFound   = errors.New("schedule not found")
	ErrBatchNotFound      = errors.New("batch not found")
	ErrResultNotFound     = errors.New("result not found")
)

// Service implements the task service business logic.
type Service struct {
	taskStore     store.TaskStore
	scheduleStore store.ScheduleStore
	resultStore   store.ResultStore
	batchStore    store.BatchStore
	breaker       *circuit.Breaker
}

// NewService creates a new Service.
func NewService(ts store.TaskStore, ss store.ScheduleStore, rs store.ResultStore, bs store.BatchStore) *Service {
	return &Service{
		taskStore:     ts,
		scheduleStore: ss,
		resultStore:   rs,
		batchStore:    bs,
	}
}

// SetCircuitBreaker sets the circuit breaker for task execution.
func (s *Service) SetCircuitBreaker(cb *circuit.Breaker) {
	s.breaker = cb
}

// CreateTask creates a new task.
func (s *Service) CreateTask(ctx context.Context, req *taskv1.CreateTaskRequest) (*taskv1.Task, error) {
	if req.Task == nil {
		return nil, ErrTaskInvalid
	}
	t := req.Task
	// 纵深防御：shell 类型任务在入队前校验命令内容（与 controlplane/server_tasks.go:149
	// 的 validateCommand 调用等价）。非 shell 类型（service/file）命令字段为空，
	// 走 maxCommandLen 上限与空检查即可，天然通过（service.go:type="" 时默认填
	// models.TaskTypeShell 见下方——所以这里校验始终是 shell 语义）。
	if err := ValidateCommand(t.Command); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTaskInvalid, err)
	}
	if t.TaskId == "" {
		t.TaskId = uuid.New().String()
	}
	if t.Type == "" {
		t.Type = models.TaskTypeShell
	}
	if t.MaxRetries == 0 {
		t.MaxRetries = 3
	}
	if t.ApprovalRequired {
		t.Status = models.TaskStatusPendingApproval
	} else {
		t.Status = models.TaskStatusPending
	}
	t.CreatedAt = timestamppb.Now()

	modelTask := protoToTask(t)
	s.taskStore.CreateTask(modelTask)
	return t, nil
}

// GetTask retrieves a task by ID.
func (s *Service) GetTask(ctx context.Context, req *taskv1.GetTaskRequest) (*taskv1.Task, error) {
	mt := s.taskStore.GetTask(req.TaskId)
	if mt == nil {
		return nil, ErrTaskNotFound
	}
	return taskToProto(mt), nil
}

// ListTasks lists tasks with optional filtering.
func (s *Service) ListTasks(ctx context.Context, req *taskv1.ListTasksRequest) (*taskv1.ListTasksResponse, error) {
	tasks := s.taskStore.ListTasks(req.TenantId, req.Status, req.AgentId, int(req.Limit))
	out := make([]*taskv1.Task, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, taskToProto(t))
	}
	return &taskv1.ListTasksResponse{Tasks: out}, nil
}

// ClaimTask atomically claims a pending task.
func (s *Service) ClaimTask(ctx context.Context, req *taskv1.ClaimTaskRequest) (*taskv1.Task, error) {
	if req.AgentId == "" {
		return nil, ErrTaskInvalid
	}
	var mt *models.Task
	var execErr error
	if s.breaker != nil {
		err := s.breaker.Execute(func() error {
			mt = s.taskStore.ClaimTask(req.AgentId)
			if mt == nil {
				execErr = ErrTaskNotFound
				return ErrTaskNotFound
			}
			return nil
		})
		if err != nil {
			metrics.RecordBusinessMetric("task_claim_failures", 1, map[string]string{"agent_id": req.AgentId})
			if execErr != nil {
				return nil, execErr
			}
			return nil, err
		}
	} else {
		mt = s.taskStore.ClaimTask(req.AgentId)
		if mt == nil {
			return nil, ErrTaskNotFound
		}
	}
	metrics.RecordBusinessMetric("task_claims_total", 1, map[string]string{"agent_id": req.AgentId})
	return taskToProto(mt), nil
}

// ReportResult reports a task execution result.
func (s *Service) ReportResult(ctx context.Context, req *taskv1.ReportResultRequest) (*taskv1.TaskResult, error) {
	if req.Result == nil {
		return nil, ErrTaskInvalid
	}
	r := req.Result
	if r.FinishedAt == nil {
		r.FinishedAt = timestamppb.Now()
	}

	var reportErr error
	if s.breaker != nil {
		err := s.breaker.Execute(func() error {
			modelResult := protoToResult(r)
			if err := s.taskStore.ReportResult(modelResult); err != nil {
				reportErr = err
				if errors.Is(err, store.ErrClaimEpochMismatch) {
					return nil // Don't retry epoch mismatch
				}
				return err
			}
			s.resultStore.SaveResult(modelResult)
			if r.TaskId != "" {
				s.resultStore.SaveLogs(r.TaskId, []models.LogLine{})
			}
			return nil
		})
		if err != nil {
			metrics.RecordBusinessMetric("task_report_failures", 1, map[string]string{"task_id": r.TaskId})
			if reportErr != nil {
				if errors.Is(reportErr, store.ErrClaimEpochMismatch) {
					return nil, ErrClaimEpochMismatch
				}
				return nil, reportErr
			}
			return nil, err
		}
	} else {
		modelResult := protoToResult(r)
		if err := s.taskStore.ReportResult(modelResult); err != nil {
			if errors.Is(err, store.ErrClaimEpochMismatch) {
				return nil, ErrClaimEpochMismatch
			}
			return nil, err
		}
		s.resultStore.SaveResult(modelResult)
		if r.TaskId != "" {
			s.resultStore.SaveLogs(r.TaskId, []models.LogLine{})
		}
	}

	metrics.RecordBusinessMetric("task_reports_total", 1, map[string]string{"task_id": r.TaskId})
	return r, nil
}

// CancelTask cancels a task.
func (s *Service) CancelTask(ctx context.Context, req *taskv1.CancelTaskRequest) error {
	if req.TaskId == "" {
		return ErrTaskInvalid
	}
	if !s.taskStore.CancelTask(req.TaskId, req.TenantId) {
		return ErrTaskNotFound
	}
	return nil
}

// ApproveTask approves a pending_approval task.
func (s *Service) ApproveTask(ctx context.Context, req *taskv1.ApproveTaskRequest) (*taskv1.Task, error) {
	if req.TaskId == "" {
		return nil, ErrTaskInvalid
	}
	if !s.taskStore.ApproveTask(req.TaskId, req.TenantId, req.ApprovedBy) {
		return nil, ErrTaskNotFound
	}
	mt := s.taskStore.GetTask(req.TaskId)
	return taskToProto(mt), nil
}

// RejectTask rejects a pending_approval task.
func (s *Service) RejectTask(ctx context.Context, req *taskv1.RejectTaskRequest) (*taskv1.Task, error) {
	if req.TaskId == "" {
		return nil, ErrTaskInvalid
	}
	if !s.taskStore.RejectTask(req.TaskId, req.TenantId, req.RejectedBy) {
		return nil, ErrTaskNotFound
	}
	mt := s.taskStore.GetTask(req.TaskId)
	return taskToProto(mt), nil
}

// GetTaskStatus gets the status of a task.
func (s *Service) GetTaskStatus(ctx context.Context, req *taskv1.GetTaskStatusRequest) (*taskv1.TaskStatusResponse, error) {
	mt := s.taskStore.GetTaskStatus(req.TaskId)
	if mt == nil {
		return nil, ErrTaskNotFound
	}
	return &taskv1.TaskStatusResponse{
		TaskId:     mt.TaskID,
		Status:     mt.Status,
		ClaimedBy:  mt.ClaimedBy,
		ClaimEpoch: mt.ClaimEpoch,
		RetryCount: int32(mt.RetryCount),
		DeadLetter: mt.DeadLetter,
	}, nil
}

// CreateSchedule creates a new schedule.
func (s *Service) CreateSchedule(ctx context.Context, req *taskv1.CreateScheduleRequest) (*taskv1.Schedule, error) {
	if req.Schedule == nil {
		return nil, ErrTaskInvalid
	}
	sched := req.Schedule
	if sched.Id == "" {
		sched.Id = uuid.New().String()
	}
	now := timestamppb.Now()
	sched.CreatedAt = now
	sched.UpdatedAt = now

	modelSched := protoToSchedule(sched)
	s.scheduleStore.CreateSchedule(modelSched)
	return sched, nil
}

// GetSchedule retrieves a schedule by ID.
func (s *Service) GetSchedule(ctx context.Context, req *taskv1.GetScheduleRequest) (*taskv1.Schedule, error) {
	ms := s.scheduleStore.GetSchedule(req.Id)
	if ms == nil {
		return nil, ErrScheduleNotFound
	}
	return scheduleToProto(ms), nil
}

// UpdateSchedule updates a schedule.
func (s *Service) UpdateSchedule(ctx context.Context, req *taskv1.UpdateScheduleRequest) (*taskv1.Schedule, error) {
	if req.Schedule == nil {
		return nil, ErrTaskInvalid
	}
	modelSched := protoToSchedule(req.Schedule)
	updated, err := s.scheduleStore.UpdateSchedule(modelSched)
	if err != nil {
		if errors.Is(err, store.ErrScheduleNotFound) {
			return nil, ErrScheduleNotFound
		}
		return nil, err
	}
	return scheduleToProto(updated), nil
}

// DeleteSchedule deletes a schedule.
func (s *Service) DeleteSchedule(ctx context.Context, req *taskv1.DeleteScheduleRequest) error {
	if !s.scheduleStore.DeleteSchedule(req.Id) {
		return ErrScheduleNotFound
	}
	return nil
}

// ListSchedules lists schedules.
func (s *Service) ListSchedules(ctx context.Context, req *taskv1.ListSchedulesRequest) (*taskv1.ListSchedulesResponse, error) {
	scheds := s.scheduleStore.ListSchedules(req.TenantId)
	out := make([]*taskv1.Schedule, 0, len(scheds))
	for _, sc := range scheds {
		out = append(out, scheduleToProto(sc))
	}
	return &taskv1.ListSchedulesResponse{Schedules: out}, nil
}

// GetTaskResult gets a task result.
func (s *Service) GetTaskResult(ctx context.Context, req *taskv1.GetTaskResultRequest) (*taskv1.TaskResult, error) {
	mr := s.resultStore.GetTaskResult(req.TaskId)
	if mr == nil {
		return nil, ErrResultNotFound
	}
	return resultToProto(mr), nil
}

// ListTaskResults lists task results.
func (s *Service) ListTaskResults(ctx context.Context, req *taskv1.ListTaskResultsRequest) (*taskv1.ListTaskResultsResponse, error) {
	results := s.resultStore.ListTaskResults(req.TenantId, req.AgentId, int(req.Limit))
	out := make([]*taskv1.TaskResult, 0, len(results))
	for _, r := range results {
		out = append(out, resultToProto(r))
	}
	return &taskv1.ListTaskResultsResponse{Results: out}, nil
}

// GetTaskLogs gets task logs.
func (s *Service) GetTaskLogs(ctx context.Context, req *taskv1.GetTaskLogsRequest) (*taskv1.TaskLogsResponse, error) {
	logs := s.resultStore.GetTaskLogs(req.TaskId)
	out := make([]*taskv1.LogLine, 0, len(logs))
	for _, l := range logs {
		out = append(out, &taskv1.LogLine{
			Timestamp: timestamppb.New(l.Timestamp),
			Level:     l.Level,
			Message:   l.Message,
		})
	}
	return &taskv1.TaskLogsResponse{TaskId: req.TaskId, Logs: out}, nil
}

// CreateBatchTask creates a batch of tasks.
func (s *Service) CreateBatchTask(ctx context.Context, req *taskv1.CreateBatchTaskRequest) (*taskv1.BatchTask, error) {
	if req.Name == "" {
		return nil, ErrTaskInvalid
	}

	batchID := uuid.New().String()
	agentIDs := req.AgentIds
	if len(agentIDs) == 0 {
		agentIDs = []string{req.AgentId}
	}

	batch := &models.BatchTask{
		BatchID:      batchID,
		TenantID:     req.TenantId,
		Name:         req.Name,
		TotalCount:   len(agentIDs),
		SuccessCount: 0,
		FailedCount:  0,
		PendingCount: len(agentIDs),
		Status:       models.BatchStatusPending,
		CreatedAt:    time.Now(),
	}
	s.batchStore.CreateBatch(batch)

	for _, agentID := range agentIDs {
		task := &taskv1.Task{
			TaskId:   uuid.New().String(),
			AgentId:  agentID,
			TenantId: req.TenantId,
			Type:     req.Type,
			Command:  req.Command,
			Content:  req.Content,
			Path:     req.Path,
			Status:   models.TaskStatusPending,
			BatchId:  batchID,
		}
		if task.Type == "" {
			task.Type = models.TaskTypeShell
		}
		modelTask := protoToTask(task)
		s.taskStore.CreateTask(modelTask)
		s.batchStore.AddTaskToBatch(batchID, task.TaskId)
	}

	return batchToProto(batch), nil
}

// GetBatchStatus gets batch status with associated tasks.
func (s *Service) GetBatchStatus(ctx context.Context, req *taskv1.GetBatchStatusRequest) (*taskv1.BatchStatusResponse, error) {
	batch := s.batchStore.GetBatch(req.BatchId)
	if batch == nil {
		return nil, ErrBatchNotFound
	}
	taskIDs := s.batchStore.GetBatchTasks(req.BatchId)
	tasks := make([]*taskv1.Task, 0, len(taskIDs))
	for _, tid := range taskIDs {
		mt := s.taskStore.GetTask(tid)
		if mt != nil {
			tasks = append(tasks, taskToProto(mt))
		}
	}
	return &taskv1.BatchStatusResponse{
		Batch: batchToProto(batch),
		Tasks: tasks,
	}, nil
}

// ListBatchTasks lists batch tasks.
func (s *Service) ListBatchTasks(ctx context.Context, req *taskv1.ListBatchTasksRequest) (*taskv1.ListBatchTasksResponse, error) {
	batches := s.batchStore.ListBatches(req.TenantId)
	out := make([]*taskv1.BatchTask, 0, len(batches))
	for _, b := range batches {
		out = append(out, batchToProto(b))
	}
	return &taskv1.ListBatchTasksResponse{Batches: out}, nil
}

// Mapping functions

func protoToTask(t *taskv1.Task) *models.Task {
	return &models.Task{
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
	}
}

func taskToProto(mt *models.Task) *taskv1.Task {
	return &taskv1.Task{
		TaskId:           mt.TaskID,
		AgentId:          mt.AgentID,
		TenantId:         mt.TenantID,
		Type:             mt.Type,
		Command:          mt.Command,
		Content:          mt.Content,
		Path:             mt.Path,
		Status:           mt.Status,
		ClaimedBy:        mt.ClaimedBy,
		ClaimedAt:        timestamppb.New(mt.ClaimedAt),
		ClaimEpoch:       mt.ClaimEpoch,
		CreatedAt:        timestamppb.New(mt.CreatedAt),
		RetryCount:       int32(mt.RetryCount),
		MaxRetries:       int32(mt.MaxRetries),
		DeadLetter:       mt.DeadLetter,
		Timeout:          int32(mt.Timeout),
		RetryDelay:       int32(mt.RetryDelay),
		Schedule:         mt.Schedule,
		ParentId:         mt.ParentID,
		DependsOn:        mt.DependsOn,
		ApprovalRequired: mt.ApprovalRequired,
		ApprovedBy:       mt.ApprovedBy,
		ApprovedAt:       timestamppb.New(mt.ApprovedAt),
		BatchId:          mt.BatchID,
	}
}

func protoToResult(r *taskv1.TaskResult) *models.TaskResult {
	return &models.TaskResult{
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

func resultToProto(mr *models.TaskResult) *taskv1.TaskResult {
	return &taskv1.TaskResult{
		TaskId:     mr.TaskID,
		AgentId:    mr.AgentID,
		ExitCode:   int32(mr.ExitCode),
		Stdout:     mr.Stdout,
		Stderr:     mr.Stderr,
		DurationMs: mr.DurationMs,
		FinishedAt: timestamppb.New(mr.FinishedAt),
		ClaimEpoch: mr.ClaimEpoch,
	}
}

func protoToSchedule(s *taskv1.Schedule) *models.Schedule {
	return &models.Schedule{
		ID:          s.Id,
		TenantID:    s.TenantId,
		Name:        s.Name,
		CronExpr:    s.CronExpr,
		TaskType:    s.TaskType,
		Command:     s.Command,
		Content:     s.Content,
		Path:        s.Path,
		AgentID:     s.AgentId,
		Enabled:     s.Enabled,
		LastFiredAt: s.LastFiredAt.AsTime(),
		CreatedAt:   s.CreatedAt.AsTime(),
		UpdatedAt:   s.UpdatedAt.AsTime(),
	}
}

func scheduleToProto(ms *models.Schedule) *taskv1.Schedule {
	return &taskv1.Schedule{
		Id:          ms.ID,
		TenantId:    ms.TenantID,
		Name:        ms.Name,
		CronExpr:    ms.CronExpr,
		TaskType:    ms.TaskType,
		Command:     ms.Command,
		Content:     ms.Content,
		Path:        ms.Path,
		AgentId:     ms.AgentID,
		Enabled:     ms.Enabled,
		LastFiredAt: timestamppb.New(ms.LastFiredAt),
		CreatedAt:   timestamppb.New(ms.CreatedAt),
		UpdatedAt:   timestamppb.New(ms.UpdatedAt),
	}
}

func batchToProto(b *models.BatchTask) *taskv1.BatchTask {
	return &taskv1.BatchTask{
		BatchId:      b.BatchID,
		TenantId:     b.TenantID,
		Name:         b.Name,
		TotalCount:   int32(b.TotalCount),
		SuccessCount: int32(b.SuccessCount),
		FailedCount:  int32(b.FailedCount),
		PendingCount: int32(b.PendingCount),
		Status:       b.Status,
		CreatedAt:    timestamppb.New(b.CreatedAt),
	}
}
