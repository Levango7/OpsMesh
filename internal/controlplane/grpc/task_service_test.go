package grpc

import (
	"context"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/grpcx"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
	taskv1 "github.com/Levango7/OpsMesh/services/task-svc/api/proto/v1"
)

func newTaskGRPCTestClient(t *testing.T) (taskv1.TaskServiceClient, func()) {
	t.Helper()
	return newTaskGRPCTestClientWithImpl(t, &TaskServiceImpl{Store: store.NewMemoryStore()})
}

func newTaskGRPCTestClientWithImpl(t *testing.T, impl *TaskServiceImpl) (taskv1.TaskServiceClient, func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	gs := grpc.NewServer()
	taskv1.RegisterTaskServiceServer(gs, impl)
	go func() { _ = gs.Serve(lis) }()
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		gs.Stop()
		t.Fatalf("client: %v", err)
	}
	return taskv1.NewTaskServiceClient(conn), func() {
		_ = conn.Close()
		gs.Stop()
	}
}

func TestTaskServiceRealGRPCTaskLifecycle(t *testing.T) {
	client, cleanup := newTaskGRPCTestClient(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	created, err := client.CreateTask(ctx, &taskv1.CreateTaskRequest{Task: &taskv1.Task{
		TaskId: "task-grpc-1", AgentId: "agent-1", TenantId: "default",
		Type: "shell", Command: "echo hello-opsmesh",
	}})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if created.Status != "pending" {
		t.Fatalf("created status=%q", created.Status)
	}

	claimed, err := client.ClaimTask(ctx, &taskv1.ClaimTaskRequest{AgentId: "agent-1"})
	if err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}
	if claimed.TaskId != created.TaskId || claimed.Status != "running" || claimed.ClaimEpoch < 1 {
		t.Fatalf("claimed=%+v", claimed)
	}

	if _, err := client.ReportResult(ctx, &taskv1.ReportResultRequest{Result: &taskv1.TaskResult{
		TaskId: created.TaskId, AgentId: "agent-1", ExitCode: 0,
		Stdout: "hello-opsmesh\n", ClaimEpoch: claimed.ClaimEpoch,
	}}); err != nil {
		t.Fatalf("ReportResult: %v", err)
	}
	taskStatus, err := client.GetTaskStatus(ctx, &taskv1.GetTaskStatusRequest{TaskId: created.TaskId})
	if err != nil {
		t.Fatalf("GetTaskStatus: %v", err)
	}
	if taskStatus.Status != "done" {
		t.Fatalf("status=%q, want done", taskStatus.Status)
	}
	if _, err := client.ClaimTask(ctx, &taskv1.ClaimTaskRequest{AgentId: "agent-1"}); status.Code(err) != codes.NotFound {
		t.Fatalf("空队列必须返回 NotFound，实际 err=%v code=%s", err, status.Code(err))
	}
}

func TestTaskServiceRejectsInvalidCommand(t *testing.T) {
	client, cleanup := newTaskGRPCTestClient(t)
	defer cleanup()
	_, err := client.CreateTask(context.Background(), &taskv1.CreateTaskRequest{Task: &taskv1.Task{
		TaskId: "bad-1", AgentId: "agent-1", TenantId: "default",
		Type: "shell", Command: "echo ok | sh",
	}})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("管道命令必须返回 InvalidArgument，实际 err=%v code=%s", err, status.Code(err))
	}
}

type taskEventRecorder struct {
	mu      sync.Mutex
	types   []string
	tenants []string
}

func (r *taskEventRecorder) PublishEvent(_ context.Context, typ, tenantID string, _ interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.types = append(r.types, typ)
	r.tenants = append(r.tenants, tenantID)
}

func (r *taskEventRecorder) snapshot() ([]string, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.types...), append([]string(nil), r.tenants...)
}

func signTaskRequest(ctx context.Context, secret, identity string, req any) context.Context {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	digest := grpcx.PayloadDigest(req)
	return metadata.AppendToOutgoingContext(ctx,
		grpcx.AgentSignatureMetadataKey, grpcx.ComputeAgentSignatureV2(secret, ts, identity, digest),
		grpcx.AgentTimestampMetadataKey, ts,
		grpcx.AgentSignatureAlgMetadataKey, grpcx.AgentSignatureAlgV2,
	)
}

// TestTaskServiceControlPlaneSignedLifecycle 走生产注入的 Agent 路径，而非只测 Store 直通：
// 签名、租户、preClaim、完整字段和结果事件必须在 taskv1 新链路上保持一致。
func TestTaskServiceControlPlaneSignedLifecycle(t *testing.T) {
	st := store.NewMemoryStore().WithDemo(true)
	st.Register(&proto.AgentInfo{AgentID: "agent-1", TenantID: "default"})
	secret := st.AgentSecret("agent-1")
	publisher := &taskEventRecorder{}
	preClaims := 0
	legacy := &GrpcServerImpl{
		Store: st,
		Svc: NewStoreAgentServiceWithHooks(st, func(context.Context, string) error {
			preClaims++
			return nil
		}),
		RequireAuth:      true,
		RequireSignature: true,
		Publisher:        publisher,
	}
	client, cleanup := newTaskGRPCTestClientWithImpl(t, &TaskServiceImpl{Store: st, Agent: legacy})
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	created, err := st.CreateTask(&proto.Task{
		TaskID: "signed-task", AgentID: "agent-1", TenantID: "default", Type: "shell",
		Command: "echo signed", Priority: 7, IdempotencyKey: "request-1",
		StatusMachine: "pending", ScriptBody: "printf signed",
	})
	if err != nil {
		t.Fatalf("CreateTask store: %v", err)
	}
	claimReq := &taskv1.ClaimTaskRequest{AgentId: "agent-1"}
	claimed, err := client.ClaimTask(signTaskRequest(ctx, secret, "agent-1", claimReq), claimReq)
	if err != nil {
		t.Fatalf("signed ClaimTask: %v", err)
	}
	if claimed.TaskId != created.TaskID || claimed.Priority != 7 || claimed.IdempotencyKey != "request-1" ||
		claimed.StatusMachine != "pending" || claimed.ScriptBody != "printf signed" {
		t.Fatalf("领取后字段丢失: %+v", claimed)
	}
	reportReq := &taskv1.ReportResultRequest{Result: &taskv1.TaskResult{
		TaskId: created.TaskID, AgentId: "agent-1", ExitCode: 0, ClaimEpoch: claimed.ClaimEpoch,
	}}
	if _, err := client.ReportResult(signTaskRequest(ctx, secret, "agent-1", reportReq), reportReq); err != nil {
		t.Fatalf("signed ReportResult: %v", err)
	}
	if got := st.TaskByID(created.TaskID); got == nil || got.Status != "done" {
		t.Fatalf("签名上报后任务未完成: %+v", got)
	}
	if preClaims != 1 {
		t.Fatalf("preClaim 调用次数=%d, want 1", preClaims)
	}
	types, tenants := publisher.snapshot()
	if len(types) != 1 || types[0] != "task_status" || len(tenants) != 1 || tenants[0] != "default" {
		t.Fatalf("结果事件丢失或租户错误: types=%v tenants=%v", types, tenants)
	}
}

func TestTaskServiceControlPlaneRejectsUnsignedRequests(t *testing.T) {
	st := store.NewMemoryStore().WithDemo(true)
	st.Register(&proto.AgentInfo{AgentID: "agent-1", TenantID: "default"})
	legacy := &GrpcServerImpl{Store: st, RequireAuth: true, RequireSignature: true}
	client, cleanup := newTaskGRPCTestClientWithImpl(t, &TaskServiceImpl{Store: st, Agent: legacy})
	defer cleanup()
	ctx := context.Background()

	createReq := &taskv1.CreateTaskRequest{Task: &taskv1.Task{
		TaskId: "must-not-create", AgentId: "agent-1", TenantId: "default", Type: "shell", Command: "echo no",
	}}
	if _, err := client.CreateTask(ctx, createReq); status.Code(err) != codes.Unimplemented {
		t.Fatalf("内嵌管理请求必须返回 Unimplemented: err=%v code=%s", err, status.Code(err))
	}
	if got := st.TaskByID("must-not-create"); got != nil {
		t.Fatalf("未签名请求不得落库: %+v", got)
	}
	if _, err := st.CreateTask(&proto.Task{TaskID: "pending-task", AgentID: "agent-1", TenantID: "default", Command: "echo ok"}); err != nil {
		t.Fatal(err)
	}
	claimReq := &taskv1.ClaimTaskRequest{AgentId: "agent-1"}
	if _, err := client.ClaimTask(ctx, claimReq); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("未签名领取必须拒绝: err=%v code=%s", err, status.Code(err))
	}
	if got := st.TaskByID("pending-task"); got == nil || got.Status != "pending" {
		t.Fatalf("未签名领取不得改变状态: %+v", got)
	}
}

func TestShouldEmbedTaskService(t *testing.T) {
	cases := []struct {
		name string
		cfg  *config.Config
		want bool
	}{
		{"nil", nil, true},
		{"empty", &config.Config{TaskSvcGRPCAddr: "", GRPCPort: 9090}, true},
		{"self localhost", &config.Config{TaskSvcGRPCAddr: "localhost:9090", GRPCPort: 9090}, true},
		{"self ipv4", &config.Config{TaskSvcGRPCAddr: "127.0.0.1:9090", GRPCPort: 9090}, true},
		{"external", &config.Config{TaskSvcGRPCAddr: "task-svc:50052", GRPCPort: 9090}, false},
		{"other port", &config.Config{TaskSvcGRPCAddr: "localhost:50052", GRPCPort: 9090}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldEmbedTaskService(tc.cfg); got != tc.want {
				t.Fatalf("ShouldEmbedTaskService=%v want %v", got, tc.want)
			}
		})
	}
}
