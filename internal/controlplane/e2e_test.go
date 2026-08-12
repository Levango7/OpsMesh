package controlplane

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"opsmesh/internal/agent"
	"opsmesh/internal/config"
	"opsmesh/internal/proto"
)

// TestE2E_TaskLifecycle 是「已编码未证实」缺口的真机闭环：
// 用真实 NewServer 拉起 gRPC(9090 通道) + 真实 HTTP 任务端点（非裸 conn.Invoke），
// 走完整链路：HTTP POST 建任务 → 真实 agent 客户端 Register → PullTasks 原子领取
// → 本地真实执行 echo（os/exec，与 agent 同款 shellForOS）→ ReportResult 上报
// → HTTP GET /api/v1/tasks 断言任务变 done 且 stdout 含执行结果。
// 全程不依赖 mysql/redis（memory store），在沙箱内可完整验证。
func TestE2E_TaskLifecycle(t *testing.T) {
	cfg := &config.Config{
		Mode:           "controlplane",
		Store:          "memory",
		Demo:           false,
		EventBus:       "noop",
		RequireAuth:    false,
		GRPCPort:       0, // 系统分配，读回真实端口
		HTTPPort:       0,
		MetricsPort:    0,
		TaskMaxRetries: 3,
	}
	s := NewServer(cfg)

	// 真实 gRPC server（与运行中的二进制同款 buildGRPC）
	gs, glis := s.buildGRPC()
	go func() { _ = gs.Serve(glis) }()
	grpcPort := glis.Addr().(*net.TCPAddr).Port

	// 真实 HTTP 任务端点（复用 s 的 handler，覆盖 HTTP→store→gRPC 全链路）
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tasks", s.handleListTasks)
	mux.HandleFunc("/api/v1/tasks/", s.handleTaskRouting)
	mux.HandleFunc("/healthz", s.handleHealthz)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("http listen: %v", err)
	}
	httpPort := lis.Addr().(*net.TCPAddr).Port
	hs := &http.Server{Handler: mux}
	go func() { _ = hs.Serve(lis) }()

	// 等服务就绪
	waitUp := func(url string) {
		for i := 0; i < 50; i++ {
			resp, e := http.Get(url)
			if e == nil && resp.StatusCode == http.StatusOK {
				resp.Body.Close()
				return
			}
			if resp != nil {
				resp.Body.Close()
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("service not up: %s", url)
	}
	waitUp(fmt.Sprintf("http://127.0.0.1:%d/healthz", httpPort))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 1) 真实 agent 客户端注册
	cli, err := agent.NewGRPCClient(
		[]string{fmt.Sprintf("127.0.0.1:%d", grpcPort)}, "", "", "", grpcPort)
	if err != nil {
		t.Fatalf("NewGRPCClient: %v", err)
	}
	regResp, err := cli.Register(ctx, &proto.AgentInfo{Segment: "seg-e2e", TenantID: "t1"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if regResp.AgentID == "" {
		t.Fatal("expected non-empty agentID from Register")
	}
	agentID := regResp.AgentID

	// 2) 经 HTTP API 下发任务（验证 HTTP 入口 + agent 校验）
	// H6 认证防御：非 demo 模式下必须携带 X-Tenant-ID 头，否则 400。
	createBody := fmt.Sprintf(
		`{"agentID":%q,"type":"shell","command":"echo hello-opsmesh-e2e"}`,
		agentID)
	creq, _ := http.NewRequest(http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d/api/v1/tasks", httpPort),
		strings.NewReader(createBody))
	creq.Header.Set("Content-Type", "application/json")
	creq.Header.Set("X-Tenant-ID", "t1")
	creq.Header.Set("X-User-Roles", "admin") // task 96：requireProd 网关注入路径放行（cache key 为短名）
	cresp, err := http.DefaultClient.Do(creq)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if cresp.StatusCode != http.StatusCreated {
		t.Fatalf("create task = %d, want 201", cresp.StatusCode)
	}
	cresp.Body.Close()

	// 3) agent 拉取任务（原子领取，pending→running）
	pulled, err := cli.PullTasks(ctx, agentID)
	if err != nil {
		t.Fatalf("PullTasks: %v", err)
	}
	if len(pulled) != 1 {
		t.Fatalf("PullTasks returned %d, want 1", len(pulled))
	}
	tk := pulled[0]

	// 4) 本地真实执行（与 agent.execute 同款 shell 选择，跨平台）
	cmd := shellForOS(tk.Command)
	execCtx, ecancel := context.WithTimeout(ctx, 10*time.Second)
	var stdout, stderr bytes.Buffer
	cmd2 := exec.CommandContext(execCtx, cmd[0], cmd[1:]...)
	cmd2.Stdout = &stdout
	cmd2.Stderr = &stderr
	runErr := cmd2.Run()
	ecancel()
	exitCode := 0
	if runErr != nil {
		if ee, ok := runErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			exitCode = -1
		}
	}

	// 5) 上报结果
	if err := cli.ReportResult(ctx, &proto.TaskResult{
		TaskID:     tk.TaskID,
		AgentID:    agentID,
		ExitCode:   exitCode,
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		DurationMs: 1,
	}); err != nil {
		t.Fatalf("ReportResult: %v", err)
	}

	// 6) 经 HTTP API 读回，断言终态 done + stdout 含执行结果
	greq, _ := http.NewRequest(http.MethodGet,
		fmt.Sprintf("http://127.0.0.1:%d/api/v1/tasks", httpPort), nil)
	greq.Header.Set("X-Tenant-ID", "t1")
	greq.Header.Set("X-User-Roles", "admin") // task 96：requireProd 网关注入路径放行
	gresp, err := http.DefaultClient.Do(greq)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	body, _ := io.ReadAll(gresp.Body)
	gresp.Body.Close()
	if !strings.Contains(string(body), `"status":"done"`) && !strings.Contains(string(body), `"status": "done"`) {
		t.Fatalf("task not done, body=%s", string(body))
	}
	if !strings.Contains(string(body), "hello-opsmesh-e2e") {
		t.Fatalf("task stdout missing echo result, body=%s", string(body))
	}

	// 干净关停
	_ = hs.Shutdown(context.Background())
	gs.Stop()
}

// shellForOS 复刻 agent.shellCommandContext 的跨平台 shell 选择，确保 e2e 真跑命令。
func shellForOS(command string) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/C", command}
	}
	return []string{"sh", "-c", command}
}
