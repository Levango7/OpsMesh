// agent_grpc_extra_test.go 通过本地 gRPC 服务器测试 agent.go 中需要 grpc 的函数。
//
// 覆盖：
//   - register 成功路径
//   - reportResult 成功路径
//   - claimTask 成功路径
//   - heartbeatLoop / dispatchLoop / cancelLoop / logCollectLoop ctx 取消路径
//   - worker 执行任务路径
//   - collectAndReportLogs 上报路径
package agent

import (
	"context"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"

	"opsmesh/internal/config"
	"opsmesh/internal/grpcx"
	"opsmesh/internal/proto"
)

// fakeAgentServer 是 grpcx.RegistrationServer 的测试假实现。
type fakeAgentServer struct {
	mu           sync.Mutex
	lastRegister *proto.AgentInfo
	lastHeart    *grpcx.HeartbeatReq
	lastResult   *proto.TaskResult
	lastReport   *grpcx.ReportLogsReq
	pullTasks    []proto.Task
	cancelledIDs []string
	registerResp *grpcx.RegisterResp
}

func (f *fakeAgentServer) Register(ctx context.Context, info *proto.AgentInfo) (*grpcx.RegisterResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastRegister = info
	if f.registerResp != nil {
		return f.registerResp, nil
	}
	return &grpcx.RegisterResp{AgentID: "agent-fake"}, nil
}

func (f *fakeAgentServer) Heartbeat(ctx context.Context, req *grpcx.HeartbeatReq) (*grpcx.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastHeart = req
	return &grpcx.Empty{}, nil
}

func (f *fakeAgentServer) PullTasks(ctx context.Context, req *grpcx.PullTasksReq) (*grpcx.PullTasksResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &grpcx.PullTasksResp{Tasks: f.pullTasks}, nil
}

func (f *fakeAgentServer) ReportResult(ctx context.Context, res *proto.TaskResult) (*grpcx.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastResult = res
	return &grpcx.Empty{}, nil
}

func (f *fakeAgentServer) CancelTask(ctx context.Context, req *grpcx.CancelTaskReq) (*grpcx.Empty, error) {
	return &grpcx.Empty{}, nil
}

func (f *fakeAgentServer) PollCancels(ctx context.Context, req *grpcx.PollCancelsReq) (*grpcx.PollCancelsResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &grpcx.PollCancelsResp{CancelledTaskIDs: f.cancelledIDs}, nil
}

func (f *fakeAgentServer) ReportLogs(ctx context.Context, req *grpcx.ReportLogsReq) (*grpcx.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastReport = req
	return &grpcx.Empty{}, nil
}

// startTestGRPCServer 启动本地 gRPC 服务器，返回客户端连接地址与清理函数。
func startTestGRPCServer(t *testing.T, srv grpcx.RegistrationServer) (addr string, cleanup func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	gs := grpc.NewServer()
	gs.RegisterService(&grpcx.Registration_ServiceDesc, srv)
	go func() { _ = gs.Serve(lis) }()
	port := lis.Addr().(*net.TCPAddr).Port
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), func() { gs.Stop() }
}

// newAgentWithServer 构造一个连接到本地 gRPC 服务器的 Agent。
func newAgentWithServer(t *testing.T, srv grpcx.RegistrationServer) (*Agent, *fakeAgentServer, func()) {
	t.Helper()
	fake, ok := srv.(*fakeAgentServer)
	if !ok {
		fake = &fakeAgentServer{}
		srv = fake
	}
	addr, cleanup := startTestGRPCServer(t, srv)
	cli, err := NewGRPCClient([]string{addr}, "", "", "", 9090)
	if err != nil {
		cleanup()
		t.Fatalf("NewGRPCClient: %v", err)
	}
	a := &Agent{
		cfg:            &config.Config{Segment: "test-seg"},
		grpc:           cli,
		agentID:        "test-agent",
		hostname:       "test-host",
		taskTimeout:    5 * time.Second,
		workers:        1,
		taskCh:         make(chan proto.Task, 4),
		running:        make(map[string]*runState),
		metricsHistory: NewMetricsHistory(MetricsHistoryDefaultCap),
		logOffsets:     make(map[string]int64),
	}
	fullCleanup := func() {
		cli.Close()
		cleanup()
	}
	return a, fake, fullCleanup
}

// --- register 成功路径 ---

func TestRegister_Success(t *testing.T) {
	fake := &fakeAgentServer{}
	a, _, cleanup := newAgentWithServer(t, fake)
	defer cleanup()
	a.agentID = "" // 清空，让 register 从响应拿
	err := a.register()
	if err != nil {
		t.Fatalf("register 应成功: %v", err)
	}
	if a.agentID != "agent-fake" {
		t.Fatalf("agentID 应为 agent-fake，得到 %q", a.agentID)
	}
}

func TestRegister_WithSignatureKey(t *testing.T) {
	fake := &fakeAgentServer{}
	a, _, cleanup := newAgentWithServer(t, fake)
	defer cleanup()
	a.cfg.GRPCSignatureKey = "preset-shared-key"
	a.agentID = ""
	if err := a.register(); err != nil {
		t.Fatalf("register 应成功: %v", err)
	}
}

func TestRegister_WithRequireSignatureButNoKey(t *testing.T) {
	fake := &fakeAgentServer{}
	a, _, cleanup := newAgentWithServer(t, fake)
	defer cleanup()
	a.cfg.GRPCRequireSignature = true
	a.agentID = ""
	// 应该注册成功但警告（控制面未启用签名验证）
	if err := a.register(); err != nil {
		t.Fatalf("register 应成功（仅警告）: %v", err)
	}
}

// --- reportResult 成功路径 ---

func TestReportResult_Success(t *testing.T) {
	fake := &fakeAgentServer{}
	a, _, cleanup := newAgentWithServer(t, fake)
	defer cleanup()
	res := proto.TaskResult{TaskID: "t1", AgentID: "test-agent", ExitCode: 0}
	a.reportResult(context.Background(), proto.Task{TaskID: "t1"}, res)
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.lastResult == nil || fake.lastResult.TaskID != "t1" {
		t.Fatalf("reportResult 应上报，lastResult = %+v", fake.lastResult)
	}
}

// --- claimTask 成功路径 ---

func TestClaimTask_WithTasks(t *testing.T) {
	fake := &fakeAgentServer{
		pullTasks: []proto.Task{{TaskID: "task-1", Type: proto.TaskTypeShell, Command: "echo hi"}},
	}
	a, _, cleanup := newAgentWithServer(t, fake)
	defer cleanup()
	task, err := a.claimTask(context.Background())
	if err != nil {
		t.Fatalf("claimTask 应成功: %v", err)
	}
	if task == nil || task.TaskID != "task-1" {
		t.Fatalf("claimTask 应返回 task-1，得到 %+v", task)
	}
}

func TestClaimTask_EmptyResponse(t *testing.T) {
	fake := &fakeAgentServer{pullTasks: nil}
	a, _, cleanup := newAgentWithServer(t, fake)
	defer cleanup()
	task, err := a.claimTask(context.Background())
	if err != nil {
		t.Fatalf("claimTask 应成功: %v", err)
	}
	if task != nil {
		t.Fatalf("无任务应返回 nil，得到 %+v", task)
	}
}

// --- heartbeatLoop ctx 取消 ---

func TestHeartbeatLoop_Cancelled(t *testing.T) {
	fake := &fakeAgentServer{}
	a, _, cleanup := newAgentWithServer(t, fake)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		a.heartbeatLoop(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("heartbeatLoop 应在 ctx 取消后退出")
	}
}

// --- dispatchLoop ctx 取消 ---

func TestDispatchLoop_Cancelled(t *testing.T) {
	fake := &fakeAgentServer{}
	a, _, cleanup := newAgentWithServer(t, fake)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		a.dispatchLoop(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("dispatchLoop 应在 ctx 取消后退出")
	}
}

// --- cancelLoop ctx 取消 ---

func TestCancelLoop_Cancelled(t *testing.T) {
	fake := &fakeAgentServer{}
	a, _, cleanup := newAgentWithServer(t, fake)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		a.cancelLoop(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("cancelLoop 应在 ctx 取消后退出")
	}
}

// --- logCollectLoop ctx 取消 ---

func TestLogCollectLoop_NoPaths(t *testing.T) {
	a := &Agent{logCollectPaths: nil, logOffsets: make(map[string]int64)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		a.logCollectLoop(ctx)
		close(done)
	}()
	// 无路径应立即返回
	select {
	case <-done:
		// ok
	case <-time.After(1 * time.Second):
		t.Fatal("logCollectLoop 无路径应立即返回")
	}
}

func TestLogCollectLoop_Cancelled(t *testing.T) {
	fake := &fakeAgentServer{}
	a, _, cleanup := newAgentWithServer(t, fake)
	defer cleanup()
	a.logCollectPaths = []string{"/nonexistent/log.log"}
	a.logCollectInterval = 1 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		a.logCollectLoop(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("logCollectLoop 应在 ctx 取消后退出")
	}
}

// --- worker 执行任务 ---

func TestWorker_ExecuteTask(t *testing.T) {
	fake := &fakeAgentServer{}
	a, _, cleanup := newAgentWithServer(t, fake)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.worker(ctx)
	// 发送一个简单任务
	a.taskCh <- proto.Task{TaskID: "task-exec-1", Type: proto.TaskTypeShell, Command: "echo hello"}
	// 等待上报
	time.Sleep(500 * time.Millisecond)
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.lastResult == nil {
		t.Fatal("worker 应上报结果")
	}
	if fake.lastResult.TaskID != "task-exec-1" {
		t.Fatalf("TaskID 应为 task-exec-1，得到 %q", fake.lastResult.TaskID)
	}
}

// --- collectAndReportLogs ---

func TestCollectAndReportLogs_FileNotExist(t *testing.T) {
	fake := &fakeAgentServer{}
	a, _, cleanup := newAgentWithServer(t, fake)
	defer cleanup()
	a.logCollectPaths = []string{"/nonexistent/log.log"}
	// 文件不存在应不 panic，跳过
	a.collectAndReportLogs(context.Background())
}

func TestCollectAndReportLogs_EmptyContent(t *testing.T) {
	fake := &fakeAgentServer{}
	a, _, cleanup := newAgentWithServer(t, fake)
	defer cleanup()
	// 创建空文件
	dir := t.TempDir()
	path := dir + "/empty.log"
	if err := writeTestFile(path, ""); err != nil {
		t.Fatal(err)
	}
	a.logCollectPaths = []string{path}
	// 空文件应跳过
	a.collectAndReportLogs(context.Background())
}

func TestCollectAndReportLogs_WithContent(t *testing.T) {
	fake := &fakeAgentServer{}
	a, _, cleanup := newAgentWithServer(t, fake)
	defer cleanup()
	dir := t.TempDir()
	path := dir + "/app.log"
	if err := writeTestFile(path, "ERROR something\nWARN careful\n"); err != nil {
		t.Fatal(err)
	}
	a.logCollectPaths = []string{path}
	a.collectAndReportLogs(context.Background())
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.lastReport == nil {
		t.Fatal("应上报日志")
	}
	if len(fake.lastReport.Report.Lines) != 2 {
		t.Fatalf("应上报 2 行，得到 %d", len(fake.lastReport.Report.Lines))
	}
}

// --- drainTasks 成功路径 ---

func TestDrainTasks_WithTasks(t *testing.T) {
	fake := &fakeAgentServer{
		pullTasks: []proto.Task{{TaskID: "task-1", Type: proto.TaskTypeShell, Command: "echo hi"}},
	}
	a, _, cleanup := newAgentWithServer(t, fake)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// drainTasks 会循环领取直到无任务或 ctx 取消
	// 由于 fake 总是返回同一个任务，会无限循环，所以用 timeout ctx
	a.drainTasks(ctx)
	// 不 panic 即可
}

// --- heartbeatLoop 一次心跳 ---

func TestHeartbeatLoop_OneBeat(t *testing.T) {
	fake := &fakeAgentServer{}
	a, _, cleanup := newAgentWithServer(t, fake)
	defer cleanup()
	// 让 collectCmdbReport 与 collectDeviceMetrics 都返回 nil（节流）
	a.cmdbLastCol = time.Now()
	a.metricsLastCol = time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	a.heartbeatLoop(ctx)
	// 12s 内应至少一次心跳
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.lastHeart == nil {
		t.Fatal("应至少一次心跳")
	}
}

// --- 辅助函数 ---

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
