// agent_registration_secret_test.go — 注册密钥下发/缺失的启动语义（soak A4）。
//
// 背景：控制面 Register 仅在 install token 认证 + TLS 时返回一次 per-agent HMAC 密钥，
// 裸注册（无 token/无 TLS）响应不带 secret。此时 ABAC HTTP 通道（bundle/decisions）
// 不可能验签成功，agent 必须明确失败而不是以空密钥静默启动。
package agent

import (
	"context"
	"net"
	"strings"
	"testing"

	"google.golang.org/grpc"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/grpcx"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// fakeRegistrationServer 返回固定 RegisterResp，其余方法为空实现。
type fakeRegistrationServer struct {
	resp *grpcx.RegisterResp
}

func (f *fakeRegistrationServer) Register(context.Context, *proto.AgentInfo) (*grpcx.RegisterResp, error) {
	return f.resp, nil
}

func (f *fakeRegistrationServer) Heartbeat(context.Context, *grpcx.HeartbeatReq) (*grpcx.Empty, error) {
	return &grpcx.Empty{}, nil
}

func (f *fakeRegistrationServer) PullTasks(context.Context, *grpcx.PullTasksReq) (*grpcx.PullTasksResp, error) {
	return &grpcx.PullTasksResp{}, nil
}

func (f *fakeRegistrationServer) ReportResult(context.Context, *proto.TaskResult) (*grpcx.Empty, error) {
	return &grpcx.Empty{}, nil
}

func (f *fakeRegistrationServer) CancelTask(context.Context, *grpcx.CancelTaskReq) (*grpcx.Empty, error) {
	return &grpcx.Empty{}, nil
}

func (f *fakeRegistrationServer) PollCancels(context.Context, *grpcx.PollCancelsReq) (*grpcx.PollCancelsResp, error) {
	return &grpcx.PollCancelsResp{}, nil
}

func (f *fakeRegistrationServer) ReportLogs(context.Context, *grpcx.ReportLogsReq) (*grpcx.Empty, error) {
	return &grpcx.Empty{}, nil
}

// startFakeRegistration 启动一个仅承载 Registration 服务的本机 gRPC server。
func startFakeRegistration(t *testing.T, resp *grpcx.RegisterResp) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	gs := grpc.NewServer()
	gs.RegisterService(&grpcx.Registration_ServiceDesc, &fakeRegistrationServer{resp: resp})
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)
	return lis.Addr().String()
}

// newRegistrationTestAgent 构造只依赖注册通道的最小 Agent。
func newRegistrationTestAgent(t *testing.T, addr string, cfg *config.Config) *Agent {
	t.Helper()
	cli, err := NewGRPCClient([]string{addr}, "", "", "", 0)
	if err != nil {
		t.Fatalf("NewGRPCClient: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	return &Agent{
		cfg:     cfg,
		grpc:    cli,
		agentID: "reg-test-agent",
		dataDir: t.TempDir(),
	}
}

// 裸注册（无 secret）且 ABAC 启用：必须 fail-fast，错误信息说明如何修复，且不含密钥材料。
func TestAgentRegister_ABACEnabledWithoutSecretFailsClearly(t *testing.T) {
	addr := startFakeRegistration(t, &grpcx.RegisterResp{AgentID: "reg-test-agent"})
	a := newRegistrationTestAgent(t, addr, &config.Config{ABACBundleEnabled: true})

	err := a.register()
	if err == nil {
		t.Fatal("ABAC 启用但注册无 secret 时必须拒绝启动，不能以空密钥继续")
	}
	msg := err.Error()
	for _, want := range []string{"ABAC", "install token", "agent.key"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("错误信息应包含 %q，得到 %q", want, msg)
		}
	}
	if strings.Contains(msg, "secret=") || strings.Contains(msg, "agents.secret=") {
		t.Fatalf("错误信息不得包含密钥内容: %q", msg)
	}
	if a.abacSecret != "" {
		t.Fatalf("失败路径不得保留空以外的密钥，得到 %q", a.abacSecret)
	}
}

// 注册响应携带 secret（token+TLS 路径）时正常启动并采用该密钥。
func TestAgentRegister_UsesDeliveredSecret(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef"
	addr := startFakeRegistration(t, &grpcx.RegisterResp{AgentID: "reg-test-agent", Secret: secret})
	a := newRegistrationTestAgent(t, addr, &config.Config{ABACBundleEnabled: true})

	if err := a.register(); err != nil {
		t.Fatalf("带 secret 的注册应成功: %v", err)
	}
	if a.abacSecret != secret {
		t.Fatalf("abacSecret = %q, want delivered secret", a.abacSecret)
	}
	if got := loadAgentKey(a.dataDir); got != secret {
		t.Fatalf("注册密钥应落盘 agent.key，得到 %q", got)
	}
}

// 显式配置预共享密钥时不需要注册响应下发（控制面同样接受预共享密钥的部署）。
func TestAgentRegister_PreSharedKeySatisfiesABAC(t *testing.T) {
	addr := startFakeRegistration(t, &grpcx.RegisterResp{AgentID: "reg-test-agent"})
	a := newRegistrationTestAgent(t, addr, &config.Config{
		ABACBundleEnabled: true,
		GRPCSignatureKey:  "pre-shared-test-key",
	})

	if err := a.register(); err != nil {
		t.Fatalf("预共享密钥应满足 ABAC 启动条件: %v", err)
	}
	if a.abacSecret != "pre-shared-test-key" {
		t.Fatalf("abacSecret = %q, want pre-shared key", a.abacSecret)
	}
}
