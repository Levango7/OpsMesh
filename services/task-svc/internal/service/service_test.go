package service

import (
	"context"
	"testing"

	taskv1 "github.com/Levango7/OpsMesh/services/task-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/store"
)

func newTestService() *Service {
	st := store.NewMemoryStore()
	return NewService(st, st, st, st)
}

func TestCreateTask(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	req := &taskv1.CreateTaskRequest{
		Task: &taskv1.Task{
			AgentId:  "agent-1",
			TenantId: "tenant-1",
			Type:     models.TaskTypeShell,
			Command:  "echo hello",
		},
	}

	task, err := svc.CreateTask(ctx, req)
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	if task.TaskId == "" {
		t.Error("expected task ID to be set")
	}
	if task.Status != models.TaskStatusPending {
		t.Errorf("expected status %s, got %s", models.TaskStatusPending, task.Status)
	}
}

func TestCreateTaskWithApproval(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	req := &taskv1.CreateTaskRequest{
		Task: &taskv1.Task{
			AgentId:          "agent-1",
			TenantId:         "tenant-1",
			Type:             models.TaskTypeShell,
			Command:          "rm -rf /",
			ApprovalRequired: true,
		},
	}

	task, err := svc.CreateTask(ctx, req)
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	if task.Status != models.TaskStatusPendingApproval {
		t.Errorf("expected status %s, got %s", models.TaskStatusPendingApproval, task.Status)
	}
}

func TestCreateTaskNil(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.CreateTask(ctx, &taskv1.CreateTaskRequest{})
	if err != ErrTaskInvalid {
		t.Fatalf("expected ErrTaskInvalid, got: %v", err)
	}
}

func TestGetTask(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.CreateTask(ctx, &taskv1.CreateTaskRequest{
		Task: &taskv1.Task{
			AgentId:  "agent-1",
			TenantId: "tenant-1",
			Command:  "echo test",
		},
	})
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	got, err := svc.GetTask(ctx, &taskv1.GetTaskRequest{TaskId: created.TaskId})
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}

	if got.TaskId != created.TaskId {
		t.Errorf("expected ID %s, got %s", created.TaskId, got.TaskId)
	}
}

func TestGetTaskNotFound(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.GetTask(ctx, &taskv1.GetTaskRequest{TaskId: "nonexistent"})
	if err != ErrTaskNotFound {
		t.Fatalf("expected ErrTaskNotFound, got: %v", err)
	}
}

func TestListTasks(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := svc.CreateTask(ctx, &taskv1.CreateTaskRequest{
			Task: &taskv1.Task{
				AgentId:  "agent-1",
				TenantId: "tenant-1",
				Command:  "echo test",
			},
		})
		if err != nil {
			t.Fatalf("CreateTask failed: %v", err)
		}
	}

	resp, err := svc.ListTasks(ctx, &taskv1.ListTasksRequest{TenantId: "tenant-1"})
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}

	if len(resp.Tasks) != 3 {
		t.Errorf("expected 3 tasks, got %d", len(resp.Tasks))
	}
}

func TestClaimTask(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.CreateTask(ctx, &taskv1.CreateTaskRequest{
		Task: &taskv1.Task{
			AgentId:  "agent-1",
			TenantId: "tenant-1",
			Command:  "echo test",
		},
	})
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	claimed, err := svc.ClaimTask(ctx, &taskv1.ClaimTaskRequest{AgentId: "agent-1"})
	if err != nil {
		t.Fatalf("ClaimTask failed: %v", err)
	}

	if claimed.TaskId != created.TaskId {
		t.Errorf("expected task ID %s, got %s", created.TaskId, claimed.TaskId)
	}
	if claimed.Status != models.TaskStatusClaimed {
		t.Errorf("expected status %s, got %s", models.TaskStatusClaimed, claimed.Status)
	}
	if claimed.ClaimedBy != "agent-1" {
		t.Errorf("expected claimedBy agent-1, got %s", claimed.ClaimedBy)
	}
}

func TestReportResult(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.CreateTask(ctx, &taskv1.CreateTaskRequest{
		Task: &taskv1.Task{
			AgentId:  "agent-1",
			TenantId: "tenant-1",
			Command:  "echo test",
		},
	})
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	_, err = svc.ClaimTask(ctx, &taskv1.ClaimTaskRequest{AgentId: "agent-1"})
	if err != nil {
		t.Fatalf("ClaimTask failed: %v", err)
	}

	result, err := svc.ReportResult(ctx, &taskv1.ReportResultRequest{
		Result: &taskv1.TaskResult{
			TaskId:   created.TaskId,
			AgentId:  "agent-1",
			ExitCode: 0,
			Stdout:   "hello",
		},
	})
	if err != nil {
		t.Fatalf("ReportResult failed: %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}

	status, err := svc.GetTaskStatus(ctx, &taskv1.GetTaskStatusRequest{TaskId: created.TaskId})
	if err != nil {
		t.Fatalf("GetTaskStatus failed: %v", err)
	}
	if status.Status != models.TaskStatusDone {
		t.Errorf("expected status %s, got %s", models.TaskStatusDone, status.Status)
	}
}

func TestReportResultFailure(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.CreateTask(ctx, &taskv1.CreateTaskRequest{
		Task: &taskv1.Task{
			AgentId:    "agent-1",
			TenantId:   "tenant-1",
			Command:    "false",
			MaxRetries: 1,
		},
	})
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	_, err = svc.ClaimTask(ctx, &taskv1.ClaimTaskRequest{AgentId: "agent-1"})
	if err != nil {
		t.Fatalf("ClaimTask failed: %v", err)
	}

	_, err = svc.ReportResult(ctx, &taskv1.ReportResultRequest{
		Result: &taskv1.TaskResult{
			TaskId:   created.TaskId,
			AgentId:  "agent-1",
			ExitCode: 1,
			Stderr:   "error",
		},
	})
	if err != nil {
		t.Fatalf("ReportResult failed: %v", err)
	}

	status, err := svc.GetTaskStatus(ctx, &taskv1.GetTaskStatusRequest{TaskId: created.TaskId})
	if err != nil {
		t.Fatalf("GetTaskStatus failed: %v", err)
	}
	if !status.DeadLetter {
		t.Error("expected dead letter to be true after max retries")
	}
}

func TestCancelTask(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.CreateTask(ctx, &taskv1.CreateTaskRequest{
		Task: &taskv1.Task{
			AgentId:  "agent-1",
			TenantId: "tenant-1",
			Command:  "echo test",
		},
	})
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	err = svc.CancelTask(ctx, &taskv1.CancelTaskRequest{TaskId: created.TaskId, TenantId: "tenant-1"})
	if err != nil {
		t.Fatalf("CancelTask failed: %v", err)
	}

	status, err := svc.GetTaskStatus(ctx, &taskv1.GetTaskStatusRequest{TaskId: created.TaskId})
	if err != nil {
		t.Fatalf("GetTaskStatus failed: %v", err)
	}
	if status.Status != models.TaskStatusCancelled {
		t.Errorf("expected status %s, got %s", models.TaskStatusCancelled, status.Status)
	}
}

func TestApproveTask(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.CreateTask(ctx, &taskv1.CreateTaskRequest{
		Task: &taskv1.Task{
			AgentId:          "agent-1",
			TenantId:         "tenant-1",
			Command:          "echo dangerous",
			ApprovalRequired: true,
		},
	})
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	approved, err := svc.ApproveTask(ctx, &taskv1.ApproveTaskRequest{
		TaskId:     created.TaskId,
		TenantId:   "tenant-1",
		ApprovedBy: "admin",
	})
	if err != nil {
		t.Fatalf("ApproveTask failed: %v", err)
	}

	if approved.Status != models.TaskStatusPending {
		t.Errorf("expected status %s, got %s", models.TaskStatusPending, approved.Status)
	}
	if approved.ApprovedBy != "admin" {
		t.Errorf("expected approvedBy admin, got %s", approved.ApprovedBy)
	}
}

func TestRejectTask(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.CreateTask(ctx, &taskv1.CreateTaskRequest{
		Task: &taskv1.Task{
			AgentId:          "agent-1",
			TenantId:         "tenant-1",
			Command:          "rm -rf /",
			ApprovalRequired: true,
		},
	})
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	rejected, err := svc.RejectTask(ctx, &taskv1.RejectTaskRequest{
		TaskId:     created.TaskId,
		TenantId:   "tenant-1",
		RejectedBy: "admin",
	})
	if err != nil {
		t.Fatalf("RejectTask failed: %v", err)
	}

	if rejected.Status != models.TaskStatusRejected {
		t.Errorf("expected status %s, got %s", models.TaskStatusRejected, rejected.Status)
	}
}

func TestCreateSchedule(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	req := &taskv1.CreateScheduleRequest{
		Schedule: &taskv1.Schedule{
			TenantId: "tenant-1",
			Name:     "Daily Backup",
			CronExpr: "0 2 * * *",
			TaskType: models.TaskTypeShell,
			Command:  "backup.sh",
			AgentId:  "agent-1",
			Enabled:  true,
		},
	}

	sched, err := svc.CreateSchedule(ctx, req)
	if err != nil {
		t.Fatalf("CreateSchedule failed: %v", err)
	}

	if sched.Id == "" {
		t.Error("expected schedule ID to be set")
	}
	if sched.CreatedAt == nil {
		t.Error("expected CreatedAt to be set")
	}
}

func TestGetSchedule(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.CreateSchedule(ctx, &taskv1.CreateScheduleRequest{
		Schedule: &taskv1.Schedule{
			TenantId: "tenant-1",
			Name:     "Test Schedule",
			CronExpr: "*/5 * * * *",
			TaskType: models.TaskTypeShell,
			Command:  "echo test",
		},
	})
	if err != nil {
		t.Fatalf("CreateSchedule failed: %v", err)
	}

	got, err := svc.GetSchedule(ctx, &taskv1.GetScheduleRequest{Id: created.Id})
	if err != nil {
		t.Fatalf("GetSchedule failed: %v", err)
	}

	if got.Id != created.Id {
		t.Errorf("expected ID %s, got %s", created.Id, got.Id)
	}
	if got.Name != "Test Schedule" {
		t.Errorf("expected name Test Schedule, got %s", got.Name)
	}
}

func TestDeleteSchedule(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.CreateSchedule(ctx, &taskv1.CreateScheduleRequest{
		Schedule: &taskv1.Schedule{
			TenantId: "tenant-1",
			Name:     "ToDelete",
			CronExpr: "0 0 * * *",
		},
	})
	if err != nil {
		t.Fatalf("CreateSchedule failed: %v", err)
	}

	err = svc.DeleteSchedule(ctx, &taskv1.DeleteScheduleRequest{Id: created.Id})
	if err != nil {
		t.Fatalf("DeleteSchedule failed: %v", err)
	}

	_, err = svc.GetSchedule(ctx, &taskv1.GetScheduleRequest{Id: created.Id})
	if err != ErrScheduleNotFound {
		t.Fatalf("expected ErrScheduleNotFound, got: %v", err)
	}
}

func TestCreateBatchTask(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	req := &taskv1.CreateBatchTaskRequest{
		TenantId: "tenant-1",
		Name:     "Deploy Config",
		Type:     models.TaskTypeShell,
		Command:  "deploy.sh",
		AgentIds: []string{"agent-1", "agent-2", "agent-3"},
	}

	batch, err := svc.CreateBatchTask(ctx, req)
	if err != nil {
		t.Fatalf("CreateBatchTask failed: %v", err)
	}

	if batch.BatchId == "" {
		t.Error("expected batch ID to be set")
	}
	if batch.TotalCount != 3 {
		t.Errorf("expected total count 3, got %d", batch.TotalCount)
	}
	if batch.PendingCount != 3 {
		t.Errorf("expected pending count 3, got %d", batch.PendingCount)
	}
}

func TestGetBatchStatus(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.CreateBatchTask(ctx, &taskv1.CreateBatchTaskRequest{
		TenantId: "tenant-1",
		Name:     "Test Batch",
		Command:  "echo test",
		AgentIds: []string{"agent-1", "agent-2"},
	})
	if err != nil {
		t.Fatalf("CreateBatchTask failed: %v", err)
	}

	resp, err := svc.GetBatchStatus(ctx, &taskv1.GetBatchStatusRequest{BatchId: created.BatchId})
	if err != nil {
		t.Fatalf("GetBatchStatus failed: %v", err)
	}

	if resp.Batch.BatchId != created.BatchId {
		t.Errorf("expected batch ID %s, got %s", created.BatchId, resp.Batch.BatchId)
	}
	if len(resp.Tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(resp.Tasks))
	}
}

func TestGetTaskResult(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.CreateTask(ctx, &taskv1.CreateTaskRequest{
		Task: &taskv1.Task{
			AgentId:  "agent-1",
			TenantId: "tenant-1",
			Command:  "echo test",
		},
	})
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	_, err = svc.ClaimTask(ctx, &taskv1.ClaimTaskRequest{AgentId: "agent-1"})
	if err != nil {
		t.Fatalf("ClaimTask failed: %v", err)
	}

	_, err = svc.ReportResult(ctx, &taskv1.ReportResultRequest{
		Result: &taskv1.TaskResult{
			TaskId:   created.TaskId,
			AgentId:  "agent-1",
			ExitCode: 0,
			Stdout:   "output",
		},
	})
	if err != nil {
		t.Fatalf("ReportResult failed: %v", err)
	}

	result, err := svc.GetTaskResult(ctx, &taskv1.GetTaskResultRequest{TaskId: created.TaskId})
	if err != nil {
		t.Fatalf("GetTaskResult failed: %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if result.Stdout != "output" {
		t.Errorf("expected stdout 'output', got %s", result.Stdout)
	}
}

func TestGetTaskLogs(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	resp, err := svc.GetTaskLogs(ctx, &taskv1.GetTaskLogsRequest{TaskId: "nonexistent"})
	if err != nil {
		t.Fatalf("GetTaskLogs failed: %v", err)
	}

	if len(resp.Logs) != 0 {
		t.Errorf("expected 0 logs, got %d", len(resp.Logs))
	}
}
