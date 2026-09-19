package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Levango7/OpsMesh/pkg/circuit"
	"github.com/Levango7/OpsMesh/pkg/metrics"
	taskv1 "github.com/Levango7/OpsMesh/services/task-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/events"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/store"
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
	canaryStore   store.CanaryStore // 灰度发布内存索引（可选注入，nil 时 canary API 返回 503）
	breaker       *circuit.Breaker
	// 事件总线/审计/SSE 桥接（A-2 阶段：可选注入，nil 时跳过发射，向后兼容）。
	bus       events.EventBus
	auditSink events.AuditSink
	sseBridge events.SSEBridge
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

// SetEventBus 注入事件总线（可选：nil 时跳过事件发射，向后兼容）。
// 生产环境由 main 注入真实实现（Kafka/日志），测试/单机可不注入。
func (s *Service) SetEventBus(bus events.EventBus) {
	s.bus = bus
}

// SetAuditSink 注入审计日志写入器（可选：nil 时跳过审计写入，向后兼容）。
// 生产环境由 main 注入 SQL 审计存储适配器，测试/单机可不注入。
func (s *Service) SetAuditSink(sink events.AuditSink) {
	s.auditSink = sink
}

// SetSSEBridge 注入 SSE 桥接器（可选：nil 时跳过 SSE 转发，向后兼容）。
// 生产环境由 main 注入真实 SSE client（转发到 controlplane SSE 通道），
// 测试/单机可不注入或注入 StubSSEBridge（日志记录）。
func (s *Service) SetSSEBridge(bridge events.SSEBridge) {
	s.sseBridge = bridge
}

// SetCanaryStore 注入灰度发布存储（可选：nil 时 canary API 返回 503，向后兼容）。
// 生产环境由 main 注入 MemoryStore（同进程内存索引），与 controlplane batchStore.canaries 对齐。
func (s *Service) SetCanaryStore(cs store.CanaryStore) {
	s.canaryStore = cs
}

// emitAudit 发射审计事件 + 事件总线事件 + SSE 桥接（A-2 阶段统一 helper）。
//
// 三者均为可选注入：nil 时对应通道跳过，不破坏现有 API/测试。
// 对齐 controlplane server_tasks.go 的三连发模式：
//   - s.audit(ctx, &proto.AuditEvent{...})  → 审计日志（等保留痕）
//   - s.bus.Publish(ctx, events.Event{...})  → 事件总线（Kafka/告警管道）
//   - s.publishEvent(ctx, "task_status", ..) → SSE（前端实时推送）
//
// sseData 为 nil 时不转发 SSE（部分事件不需要 SSE 推送）。
func (s *Service) emitAudit(ctx context.Context, tenantID, userID, action, target, detail string, level events.Level, sseData interface{}) {
	// 审计日志（等保三级：操作 100% 留痕）
	if s.auditSink != nil {
		s.auditSink.Audit(events.NewAuditEvent(ctx, tenantID, userID, action, target, detail))
	}
	// 事件总线（Kafka/告警管道）
	if s.bus != nil {
		_ = s.bus.Publish(ctx, events.Event{
			TenantID: tenantID,
			UserID:   userID,
			Action:   action,
			Target:   target,
			Detail:   detail,
			Level:    level,
		})
	}
	// SSE 桥接（前端实时推送）
	if s.sseBridge != nil && sseData != nil {
		s.sseBridge.Forward(ctx, "task_status", tenantID, sseData)
	}
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
	if _, err := s.taskStore.CreateTask(modelTask); err != nil {
		return nil, fmt.Errorf("create task: %w", err)
	}
	// A-2 阶段：任务生命周期事件审计（创建）
	s.emitAudit(ctx, t.TenantId, "", "create_task", t.TaskId, t.Command, events.LevelInfo, map[string]string{
		"taskID":  t.TaskId,
		"status":  t.Status,
		"agentID": t.AgentId,
	})
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
	tasks, err := s.taskStore.ListTasks(req.TenantId, req.Status, req.AgentId, int(req.Limit))
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
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
	// A-2 阶段：任务生命周期事件审计（分配/领取）
	s.emitAudit(ctx, mt.TenantID, req.AgentId, "claim_task", mt.TaskID, "claimed by agent", events.LevelInfo, map[string]string{
		"taskID":  mt.TaskID,
		"status":  mt.Status,
		"agentID": req.AgentId,
	})
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
	// A-2 阶段：任务生命周期事件审计（完成/失败）
	// ExitCode==0 → 完成（info）；ExitCode!=0 → 失败（warn）
	action := "report_result"
	level := events.LevelInfo
	detail := fmt.Sprintf("exit=%d", r.ExitCode)
	if r.ExitCode != 0 {
		action = "fail_task"
		level = events.LevelWarn
	}
	// 从 store 拿当前任务状态用于 SSE 载荷（不额外引入 req 字段）
	var sseStatus string
	if mt := s.taskStore.GetTask(r.TaskId); mt != nil {
		sseStatus = mt.Status
	}
	s.emitAudit(ctx, "", r.AgentId, action, r.TaskId, detail, level, map[string]string{
		"taskID":   r.TaskId,
		"status":   sseStatus,
		"agentID":  r.AgentId,
		"exitCode": fmt.Sprintf("%d", r.ExitCode),
	})
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
	// A-2 阶段：任务生命周期事件审计（取消）
	s.emitAudit(ctx, req.TenantId, "", "cancel_task", req.TaskId, "task cancelled", events.LevelInfo, map[string]string{
		"taskID": req.TaskId,
		"status": models.TaskStatusCancelled,
	})
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
	result, err := s.scheduleStore.CreateSchedule(modelSched)
	if err != nil {
		return nil, fmt.Errorf("create schedule: %w", err)
	}
	if result == nil {
		return nil, ErrScheduleNotFound
	}
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
		if _, err := s.taskStore.CreateTask(modelTask); err != nil {
			return nil, fmt.Errorf("create batch task: %w", err)
		}
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

// ============================================================================
// 灰度发布（canary）— 对齐 controlplane server_batch.go L265-615
//
// task-svc 不管理 agent/device，直接为每个 deviceID 创建任务（无需 lookupAgent 检查）。
// 灰度状态仅内存索引（重启后丢失），任务实例本身持久化在 taskStore 中。
// ============================================================================

// Canary 相关错误。
var (
	ErrCanaryNotFound          = errors.New("canary not found")
	ErrCanaryStoreNotAvailable = errors.New("canary store not available")
	ErrCanaryNoPendingPhase    = errors.New("no pending phase to advance")
)

// CanaryCreateRequest 灰度发布创建请求（service 层 DTO，不经 proto）。
type CanaryCreateRequest struct {
	DeviceIDs  []string
	TaskType   string
	Command    string
	Content    string
	Path       string
	Strategy   string // percentage/group/label
	Percentage int    // strategy=percentage 时有效
	Groups     []string
	Labels     map[string]string
	TenantID   string
	UserID     string
}

// genCanaryID 生成灰度 ID（canary-<8 字节 hex>）。
func genCanaryID() string {
	var b [8]byte
	// crypto/rand 失败仅见于系统熵源故障的极端环境：占位 ID 保持非空可用。
	if _, err := rand.Read(b[:]); err != nil {
		log.Printf("[task-svc] genCanaryID: crypto/rand 读取失败（使用零值占位 ID）: %v", err)
	}
	return "canary-" + hex.EncodeToString(b[:])
}

// planCanaryPhases 按策略划分灰度阶段（对齐 controlplane planCanaryPhases）。
func planCanaryPhases(devices []string, strategy string, percentage int, groups []string, labels map[string]string) []models.CanaryPhase {
	switch strategy {
	case "percentage":
		// 按比例分两阶段：第一阶段 percentage%，第二阶段剩余。
		if percentage <= 0 {
			percentage = 10
		}
		if percentage > 100 {
			percentage = 100
		}
		n := len(devices) * percentage / 100
		if n < 1 && len(devices) > 0 {
			n = 1
		}
		phases := []models.CanaryPhase{
			{Phase: 1, DeviceIDs: append([]string(nil), devices[:n]...), Status: "pending"},
		}
		if n < len(devices) {
			phases = append(phases, models.CanaryPhase{
				Phase: 2, DeviceIDs: append([]string(nil), devices[n:]...), Status: "pending",
			})
		}
		return phases
	case "group":
		// 按分组多阶段：每个分组一阶段。
		// 简化实现：分组仅作为标签，实际设备划分由调用方在 deviceIDs 中已指定；
		// 这里按 groups 数量等分 deviceIDs。
		nGroups := len(groups)
		if nGroups == 0 {
			nGroups = 1
		}
		phases := make([]models.CanaryPhase, nGroups)
		chunkSize := (len(devices) + nGroups - 1) / nGroups
		for i := 0; i < nGroups; i++ {
			start := i * chunkSize
			end := start + chunkSize
			if start > len(devices) {
				start = len(devices)
			}
			if end > len(devices) {
				end = len(devices)
			}
			phases[i] = models.CanaryPhase{
				Phase:     i + 1,
				DeviceIDs: append([]string(nil), devices[start:end]...),
				Status:    "pending",
			}
		}
		return phases
	case "label":
		// 按标签单阶段（标签筛选由调用方在 deviceIDs 中已完成）。
		return []models.CanaryPhase{{Phase: 1, DeviceIDs: append([]string(nil), devices...), Status: "pending"}}
	}
	return []models.CanaryPhase{{Phase: 1, DeviceIDs: append([]string(nil), devices...), Status: "pending"}}
}

// execCanaryPhase 执行灰度的某一阶段：为每个设备下发任务。
// task-svc 不管理 agent/device，直接为每个 deviceID 创建任务（无需 lookupAgent 检查）。
func (s *Service) execCanaryPhase(ctx context.Context, canary *models.CanaryRelease, phase *models.CanaryPhase,
	taskType, command, content, path, tenantID, userID string) {
	phase.StartedAt = time.Now()
	phase.Status = "running"
	phase.Tasks = make([]models.BatchTaskItem, 0, len(phase.DeviceIDs))
	for _, devID := range phase.DeviceIDs {
		task := &models.Task{
			TaskID:   uuid.New().String(),
			AgentID:  devID,
			TenantID: tenantID,
			Type:     taskType,
			Command:  command,
			Content:  content,
			Path:     path,
			Status:   models.TaskStatusPending,
		}
		if task.Type == "" {
			task.Type = models.TaskTypeShell
		}
		if task.MaxRetries == 0 {
			task.MaxRetries = 3
		}
		created, err := s.taskStore.CreateTask(task)
		if err != nil {
			phase.Tasks = append(phase.Tasks, models.BatchTaskItem{
				DeviceID: devID, Status: "failed", Error: err.Error(),
			})
			continue
		}
		phase.Tasks = append(phase.Tasks, models.BatchTaskItem{
			DeviceID: devID, TaskID: created.TaskID, Status: created.Status,
		})
		// 审计 + 事件总线 + SSE（对齐 controlplane 三连发模式）
		s.emitAudit(ctx, tenantID, userID, "canary_exec", created.TaskID,
			fmt.Sprintf("canary:%s:phase%d", canary.CanaryID, phase.Phase),
			events.LevelInfo, map[string]string{
				"taskID":  created.TaskID,
				"status":  created.Status,
				"agentID": devID,
			})
	}
}

// CreateCanary 创建灰度发布，按策略划分阶段，执行第一阶段。
func (s *Service) CreateCanary(ctx context.Context, req *CanaryCreateRequest) (*models.CanaryRelease, error) {
	if s.canaryStore == nil {
		return nil, ErrCanaryStoreNotAvailable
	}
	if len(req.DeviceIDs) == 0 {
		return nil, fmt.Errorf("%w: deviceIDs is required", ErrTaskInvalid)
	}
	if req.Command == "" {
		return nil, fmt.Errorf("%w: command is required", ErrTaskInvalid)
	}
	taskType := req.TaskType
	if taskType == "" {
		taskType = models.TaskTypeShell
	}
	// shell 类型命令校验（对齐 controlplane validateCommand）
	if taskType == models.TaskTypeShell {
		if err := ValidateCommand(req.Command); err != nil {
			return nil, fmt.Errorf("%w: command validation failed: %v", ErrTaskInvalid, err)
		}
	}
	switch req.Strategy {
	case "percentage", "group", "label":
	default:
		return nil, fmt.Errorf("%w: strategy must be percentage/group/label", ErrTaskInvalid)
	}

	canaryID := genCanaryID()
	phases := planCanaryPhases(req.DeviceIDs, req.Strategy, req.Percentage, req.Groups, req.Labels)

	now := time.Now()
	canary := &models.CanaryRelease{
		CanaryID:   canaryID,
		TenantID:   req.TenantID,
		TaskType:   taskType,
		Command:    req.Command,
		Strategy:   req.Strategy,
		Percentage: req.Percentage,
		Groups:     req.Groups,
		Labels:     req.Labels,
		CreatedAt:  now,
		CreatedBy:  req.UserID,
		Phases:     phases,
	}

	// 立即执行第一阶段，其余阶段标记 pending（需手动推进）。
	if len(phases) > 0 {
		s.execCanaryPhase(ctx, canary, &canary.Phases[0], taskType, req.Command, req.Content, req.Path, req.TenantID, req.UserID)
	}

	s.canaryStore.CreateCanary(canary)
	return canary, nil
}

// GetCanaryStatus 查询灰度发布状态（实时刷新每阶段任务状态）。
func (s *Service) GetCanaryStatus(ctx context.Context, canaryID, tenantID string) (*models.CanaryRelease, error) {
	if s.canaryStore == nil {
		return nil, ErrCanaryStoreNotAvailable
	}
	canary := s.canaryStore.GetCanary(canaryID)
	if canary == nil {
		return nil, ErrCanaryNotFound
	}
	if tenantID != "" && canary.TenantID != tenantID {
		return nil, ErrCanaryNotFound
	}
	// 实时刷新每阶段每个任务的状态（对齐 controlplane handleCanaryStatus 刷新逻辑）。
	for i := range canary.Phases {
		for j := range canary.Phases[i].Tasks {
			taskID := canary.Phases[i].Tasks[j].TaskID
			if taskID == "" {
				continue
			}
			t := s.taskStore.GetTask(taskID)
			if t != nil {
				canary.Phases[i].Tasks[j].Status = t.Status
			}
		}
	}
	return canary, nil
}

// AdvanceCanary 推进灰度发布到下一阶段。
func (s *Service) AdvanceCanary(ctx context.Context, canaryID, tenantID, userID string) (*models.CanaryRelease, error) {
	if s.canaryStore == nil {
		return nil, ErrCanaryStoreNotAvailable
	}
	canary := s.canaryStore.GetCanary(canaryID)
	if canary == nil {
		return nil, ErrCanaryNotFound
	}
	if tenantID != "" && canary.TenantID != tenantID {
		return nil, ErrCanaryNotFound
	}
	// 找到下一个 pending 阶段并执行。
	for i := range canary.Phases {
		if canary.Phases[i].Status == "pending" {
			s.execCanaryPhase(ctx, canary, &canary.Phases[i], canary.TaskType, canary.Command, "", "", canary.TenantID, userID)
			s.canaryStore.UpdateCanary(canary)
			return canary, nil
		}
	}
	return nil, ErrCanaryNoPendingPhase
}
