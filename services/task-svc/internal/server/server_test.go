package server

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	taskv1 "github.com/Levango7/OpsMesh/services/task-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/store"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	mem := store.NewMemoryStore()
	return NewServer(service.NewService(mem, mem, mem, mem))
}

func requireCode(t *testing.T, err error, want codes.Code, what string) {
	t.Helper()
	if got := status.Code(err); got != want {
		t.Fatalf("%s: code = %v, want %v (err=%v)", what, got, want, err)
	}
}

func TestCreateAndGetTask(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	created, err := srv.CreateTask(ctx, &taskv1.CreateTaskRequest{Task: &taskv1.Task{
		TenantId: "t1",
		AgentId:  "a1",
		Command:  "echo hi",
	}})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if created.GetTaskId() == "" {
		t.Fatal("CreateTask 未生成 TaskId")
	}
	if created.GetType() != "shell" {
		t.Fatalf("默认 Type = %q, want shell", created.GetType())
	}
	if created.GetMaxRetries() != 3 {
		t.Fatalf("默认 MaxRetries = %d, want 3", created.GetMaxRetries())
	}
	if created.GetStatus() != "pending" {
		t.Fatalf("默认 Status = %q, want pending", created.GetStatus())
	}

	got, err := srv.GetTask(ctx, &taskv1.GetTaskRequest{TaskId: created.GetTaskId()})
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.GetTaskId() != created.GetTaskId() || got.GetCommand() != "echo hi" {
		t.Fatalf("GetTask = %+v", got)
	}

	_, err = srv.GetTask(ctx, &taskv1.GetTaskRequest{TaskId: "missing"})
	requireCode(t, err, codes.NotFound, "GetTask(missing)")
}

func TestCreateTaskInvalidRequest(t *testing.T) {
	srv := newTestServer(t)
	_, err := srv.CreateTask(context.Background(), &taskv1.CreateTaskRequest{})
	requireCode(t, err, codes.InvalidArgument, "CreateTask(nil Task)")
}

func TestClaimLifecycle(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	_, err := srv.ClaimTask(ctx, &taskv1.ClaimTaskRequest{})
	requireCode(t, err, codes.InvalidArgument, "ClaimTask(empty agent)")

	created, err := srv.CreateTask(ctx, &taskv1.CreateTaskRequest{Task: &taskv1.Task{
		TenantId: "t1", AgentId: "a1", Command: "echo hi",
	}})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	claimed, err := srv.ClaimTask(ctx, &taskv1.ClaimTaskRequest{AgentId: "a1"})
	if err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}
	if claimed.GetTaskId() != created.GetTaskId() || claimed.GetStatus() != "claimed" {
		t.Fatalf("ClaimTask = %+v", claimed)
	}
	if claimed.GetClaimEpoch() != 1 {
		t.Fatalf("ClaimEpoch = %d, want 1", claimed.GetClaimEpoch())
	}

	_, err = srv.ClaimTask(ctx, &taskv1.ClaimTaskRequest{AgentId: "a1"})
	requireCode(t, err, codes.NotFound, "重复 Claim")
}

func TestCancelTask(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	_, err := srv.CancelTask(ctx, &taskv1.CancelTaskRequest{})
	requireCode(t, err, codes.InvalidArgument, "CancelTask(empty id)")

	created, err := srv.CreateTask(ctx, &taskv1.CreateTaskRequest{Task: &taskv1.Task{
		TenantId: "t1", AgentId: "a1", Command: "echo hi",
	}})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if _, err := srv.CancelTask(ctx, &taskv1.CancelTaskRequest{TaskId: created.GetTaskId(), TenantId: "t1"}); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	got, err := srv.GetTask(ctx, &taskv1.GetTaskRequest{TaskId: created.GetTaskId()})
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.GetStatus() != "cancelled" {
		t.Fatalf("Status = %q, want cancelled", got.GetStatus())
	}

	_, err = srv.CancelTask(ctx, &taskv1.CancelTaskRequest{TaskId: "missing"})
	requireCode(t, err, codes.NotFound, "CancelTask(missing)")
}

func TestApproveAndReject(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	_, err := srv.ApproveTask(ctx, &taskv1.ApproveTaskRequest{})
	requireCode(t, err, codes.InvalidArgument, "ApproveTask(empty id)")
	_, err = srv.RejectTask(ctx, &taskv1.RejectTaskRequest{})
	requireCode(t, err, codes.InvalidArgument, "RejectTask(empty id)")

	created, err := srv.CreateTask(ctx, &taskv1.CreateTaskRequest{Task: &taskv1.Task{
		TenantId: "t1", AgentId: "a1", Command: "echo hi", ApprovalRequired: true,
	}})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if created.GetStatus() != "pending_approval" {
		t.Fatalf("Status = %q, want pending_approval", created.GetStatus())
	}

	approved, err := srv.ApproveTask(ctx, &taskv1.ApproveTaskRequest{
		TaskId: created.GetTaskId(), TenantId: "t1", ApprovedBy: "alice",
	})
	if err != nil {
		t.Fatalf("ApproveTask: %v", err)
	}
	if approved.GetStatus() != "pending" || approved.GetApprovedBy() != "alice" {
		t.Fatalf("ApproveTask = %+v", approved)
	}

	second, err := srv.CreateTask(ctx, &taskv1.CreateTaskRequest{Task: &taskv1.Task{
		TenantId: "t1", AgentId: "a2", Command: "echo hi", ApprovalRequired: true,
	}})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	rejected, err := srv.RejectTask(ctx, &taskv1.RejectTaskRequest{
		TaskId: second.GetTaskId(), TenantId: "t1", RejectedBy: "bob",
	})
	if err != nil {
		t.Fatalf("RejectTask: %v", err)
	}
	if rejected.GetStatus() != "rejected" {
		t.Fatalf("Status = %q, want rejected", rejected.GetStatus())
	}

	_, err = srv.ApproveTask(ctx, &taskv1.ApproveTaskRequest{TaskId: "missing"})
	requireCode(t, err, codes.NotFound, "ApproveTask(missing)")
}

func TestReportResultMapping(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	_, err := srv.ReportResult(ctx, &taskv1.ReportResultRequest{})
	requireCode(t, err, codes.InvalidArgument, "ReportResult(nil Result)")

	created, err := srv.CreateTask(ctx, &taskv1.CreateTaskRequest{Task: &taskv1.Task{
		TenantId: "t1", AgentId: "a1", Command: "echo hi",
	}})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	claimed, err := srv.ClaimTask(ctx, &taskv1.ClaimTaskRequest{AgentId: "a1"})
	if err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}

	_, err = srv.ReportResult(ctx, &taskv1.ReportResultRequest{Result: &taskv1.TaskResult{
		TaskId:     created.GetTaskId(),
		AgentId:    "a1",
		ExitCode:   0,
		ClaimEpoch: claimed.GetClaimEpoch() + 1,
	}})
	requireCode(t, err, codes.FailedPrecondition, "ReportResult(epoch mismatch)")

	result, err := srv.ReportResult(ctx, &taskv1.ReportResultRequest{Result: &taskv1.TaskResult{
		TaskId:     created.GetTaskId(),
		AgentId:    "a1",
		ExitCode:   0,
		ClaimEpoch: claimed.GetClaimEpoch(),
	}})
	if err != nil {
		t.Fatalf("ReportResult: %v", err)
	}
	if result.GetTaskId() != created.GetTaskId() {
		t.Fatalf("ReportResult = %+v", result)
	}

	got, err := srv.GetTaskResult(ctx, &taskv1.GetTaskResultRequest{TaskId: created.GetTaskId()})
	if err != nil {
		t.Fatalf("GetTaskResult: %v", err)
	}
	if got.GetExitCode() != 0 {
		t.Fatalf("GetTaskResult = %+v", got)
	}
	_, err = srv.GetTaskResult(ctx, &taskv1.GetTaskResultRequest{TaskId: "missing"})
	requireCode(t, err, codes.NotFound, "GetTaskResult(missing)")

	logs, err := srv.GetTaskLogs(ctx, &taskv1.GetTaskLogsRequest{TaskId: created.GetTaskId()})
	if err != nil {
		t.Fatalf("GetTaskLogs: %v", err)
	}
	if logs.GetTaskId() != created.GetTaskId() {
		t.Fatalf("GetTaskLogs = %+v", logs)
	}
}

func TestScheduleCRUDMapping(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	_, err := srv.CreateSchedule(ctx, &taskv1.CreateScheduleRequest{})
	requireCode(t, err, codes.InvalidArgument, "CreateSchedule(nil)")

	created, err := srv.CreateSchedule(ctx, &taskv1.CreateScheduleRequest{Schedule: &taskv1.Schedule{
		Id: "sch-1", TenantId: "t1", Name: "nightly", CronExpr: "0 3 * * *",
	}})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	if created.GetId() != "sch-1" {
		t.Fatalf("CreateSchedule = %+v", created)
	}
	if _, err := srv.GetSchedule(ctx, &taskv1.GetScheduleRequest{Id: "sch-1"}); err != nil {
		t.Fatalf("GetSchedule: %v", err)
	}
	_, err = srv.GetSchedule(ctx, &taskv1.GetScheduleRequest{Id: "missing"})
	requireCode(t, err, codes.NotFound, "GetSchedule(missing)")

	_, err = srv.UpdateSchedule(ctx, &taskv1.UpdateScheduleRequest{})
	requireCode(t, err, codes.InvalidArgument, "UpdateSchedule(nil)")
	_, err = srv.UpdateSchedule(ctx, &taskv1.UpdateScheduleRequest{Schedule: &taskv1.Schedule{Id: "missing"}})
	requireCode(t, err, codes.NotFound, "UpdateSchedule(missing)")

	list, err := srv.ListSchedules(ctx, &taskv1.ListSchedulesRequest{TenantId: "t1"})
	if err != nil {
		t.Fatalf("ListSchedules: %v", err)
	}
	if len(list.GetSchedules()) != 1 {
		t.Fatalf("ListSchedules = %d 条, want 1", len(list.GetSchedules()))
	}

	if _, err := srv.DeleteSchedule(ctx, &taskv1.DeleteScheduleRequest{Id: "sch-1"}); err != nil {
		t.Fatalf("DeleteSchedule: %v", err)
	}
	_, err = srv.DeleteSchedule(ctx, &taskv1.DeleteScheduleRequest{Id: "sch-1"})
	requireCode(t, err, codes.NotFound, "重复 DeleteSchedule")
}

func TestBatchMapping(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	_, err := srv.CreateBatchTask(ctx, &taskv1.CreateBatchTaskRequest{})
	requireCode(t, err, codes.InvalidArgument, "CreateBatchTask(empty name)")

	batch, err := srv.CreateBatchTask(ctx, &taskv1.CreateBatchTaskRequest{
		Name: "b1", TenantId: "t1", AgentIds: []string{"a1", "a2"}, Command: "echo hi",
	})
	if err != nil {
		t.Fatalf("CreateBatchTask: %v", err)
	}
	if batch.GetTotalCount() != 2 || batch.GetStatus() != "pending" {
		t.Fatalf("CreateBatchTask = %+v", batch)
	}

	statusResp, err := srv.GetBatchStatus(ctx, &taskv1.GetBatchStatusRequest{BatchId: batch.GetBatchId()})
	if err != nil {
		t.Fatalf("GetBatchStatus: %v", err)
	}
	if len(statusResp.GetTasks()) != 2 {
		t.Fatalf("GetBatchStatus tasks = %d, want 2", len(statusResp.GetTasks()))
	}

	_, err = srv.GetBatchStatus(ctx, &taskv1.GetBatchStatusRequest{BatchId: "missing"})
	requireCode(t, err, codes.NotFound, "GetBatchStatus(missing)")

	list, err := srv.ListBatchTasks(ctx, &taskv1.ListBatchTasksRequest{TenantId: "t1"})
	if err != nil {
		t.Fatalf("ListBatchTasks: %v", err)
	}
	if len(list.GetBatches()) != 1 {
		t.Fatalf("ListBatchTasks = %d 条, want 1", len(list.GetBatches()))
	}
}
