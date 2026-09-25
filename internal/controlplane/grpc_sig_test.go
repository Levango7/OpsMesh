// grpc_sig_test.go — P1-2 gRPC agent 身份绑定的端到端护栏。
//
// 这里起真实 gRPC server（手写 ServiceDesc + JSON codec，可选 TLS）+ 真实 agent 客户端，覆盖：
//  1. 密钥下发门槛：只有 install token 认证 + TLS 才下发 per-agent 密钥；
//  2. 真 agent 用下发的密钥签名（v2，覆盖载荷）→ 请求通过；
//  3. 篡改载荷：v1 签名照旧通过（旧机制的真实缺陷），v2 签名被拒（本次修复核心）；
//  4. 错密钥 / 过期时间戳 / 未知算法 / 缺签名一律拒绝；
//  5. 同一 agentID 携他租户重注册被拒（防越权接管）。
package controlplane

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/Levango7/OpsMesh/internal/agent"
	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/grpcx"
	"github.com/Levango7/OpsMesh/internal/metrics"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
	"github.com/Levango7/OpsMesh/internal/tlsutil"

	grpcserver "github.com/Levango7/OpsMesh/internal/controlplane/grpc"
)

// sigTestEnv 测试环境：监听地址 + TLS 材料（agent 侧只需 CA 校验服务端）+ 指标句柄。
type sigTestEnv struct {
	addr     string
	certFile string
	keyFile  string
	caFile   string
	metrics  *metrics.M
}

// startSigTestServer 起一个启用签名验证的真实 gRPC server；tlsOn 决定是否挂 TLS 凭证。
// Cfg.TLSCert 非空 + 连接经 TLS 握手 = 控制面认定「加密信道」（见 transportIsTLS）。
func startSigTestServer(t *testing.T, st store.Store, tlsOn bool) sigTestEnv {
	t.Helper()
	env := sigTestEnv{}
	var certPEM []byte
	env.certFile, env.keyFile, certPEM = writeSelfSignedCert(t)
	env.caFile = filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(env.caFile, certPEM, 0o600); err != nil {
		t.Fatalf("写 CA 失败: %v", err)
	}

	cfg := &config.Config{}
	var opts []grpc.ServerOption
	if tlsOn {
		cfg.TLSCert = env.certFile
		creds, err := tlsutil.ServerCreds(env.certFile, env.keyFile, "")
		if err != nil {
			t.Fatalf("ServerCreds: %v", err)
		}
		opts = append(opts, grpc.Creds(creds))
	}

	env.metrics = metrics.New()
	impl := &grpcserver.GrpcServerImpl{
		Store: st,
		// RequireAuth=true 必须与出厂生产形态一致（compose 的 OPSMESH_REQUIRE_AUTH=true +
		// OPSMESH_GRPC_REQUIRE_SIGNATURE=true）。此前本夹具用 false，掩盖了「拉模型 agent
		// 直连 gRPC、无网关注入租户 → 所有业务 RPC 被 401」的真实缺陷（真机实测复现）。
		RequireAuth:      true,
		RequireSignature: true,
		Cfg:              cfg,
		Metrics:          env.metrics,
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	gs := grpc.NewServer(opts...)
	gs.RegisterService(&grpcx.Registration_ServiceDesc, impl)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)

	env.addr = lis.Addr().String()
	return env
}

// newSigTestClient 用真实 agent 客户端连接测试 server。
func newSigTestClient(t *testing.T, env sigTestEnv, tlsOn bool) *agent.GRPCClient {
	t.Helper()
	host, portStr, err := net.SplitHostPort(env.addr)
	if err != nil {
		t.Fatalf("拆分地址失败: %v", err)
	}
	port, _ := strconv.Atoi(portStr)
	ca := ""
	if tlsOn {
		ca = env.caFile // 仅校验服务端（服务端不要求客户端持证）
	}
	cli, err := agent.NewGRPCClient([]string{host + ":" + portStr}, "", "", ca, port)
	if err != nil {
		t.Fatalf("NewGRPCClient: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	return cli
}

// rawInvoke 用裸连接直接调 gRPC 方法（手工构造 metadata，模拟篡改/恶意 agent）。
func rawInvoke(ctx context.Context, t *testing.T, env sigTestEnv, tlsOn bool, method string, md map[string]string, req, resp any) error {
	t.Helper()
	opts := []grpc.DialOption{grpc.WithDefaultCallOptions(grpc.ForceCodec(grpcx.JSONCodec))}
	if tlsOn {
		creds, err := tlsutil.ClientCreds("", "", env.caFile)
		if err != nil {
			t.Fatalf("ClientCreds: %v", err)
		}
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	conn, err := grpc.DialContext(ctx, env.addr, opts...)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	cctx := ctx
	if len(md) > 0 {
		pairs := make([]string, 0, len(md)*2)
		for k, v := range md {
			pairs = append(pairs, k, v)
		}
		cctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(pairs...))
	}
	return conn.Invoke(cctx, method, req, resp, grpc.ForceCodec(grpcx.JSONCodec))
}

// v1MD 构造旧算法（不覆盖载荷）的签名 metadata。
func v1MD(secret, ts, identity string) map[string]string {
	return map[string]string{
		grpcx.AgentTimestampMetadataKey:    ts,
		grpcx.AgentSignatureMetadataKey:    grpcx.ComputeAgentSignatureV1(secret, ts, identity),
		grpcx.AgentSignatureAlgMetadataKey: grpcx.AgentSignatureAlgV1,
	}
}

// v2MD 构造新算法（覆盖载荷）的签名 metadata。
func v2MD(secret, ts, identity string, payload any) map[string]string {
	return map[string]string{
		grpcx.AgentTimestampMetadataKey:    ts,
		grpcx.AgentSignatureMetadataKey:    grpcx.ComputeAgentSignatureV2(secret, ts, identity, grpcx.PayloadDigest(payload)),
		grpcx.AgentSignatureAlgMetadataKey: grpcx.AgentSignatureAlgV2,
	}
}

// sigMetric 从 /metrics 文本中取出验签计数（algo 与 result 为固定标签值）。
// 返回 -1 表示时序缺失（渲染异常）。
func sigMetric(t *testing.T, m *metrics.M, alg, result string) int {
	t.Helper()
	pat := fmt.Sprintf(`opsmesh_agent_signature_verifications_total{alg="%s",result="%s"} (\d+)`, alg, result)
	re := regexp.MustCompile(pat)
	match := re.FindStringSubmatch(m.Render())
	if match == nil {
		return -1
	}
	n, err := strconv.Atoi(match[1])
	if err != nil {
		return -1
	}
	return n
}

// sigKeySourceMetric 从 /metrics 文本中取出「验签通过所用密钥来源」计数。
func sigKeySourceMetric(t *testing.T, m *metrics.M, source string) int {
	t.Helper()
	pat := fmt.Sprintf(`opsmesh_agent_signing_key_source_total{source="%s"} (\d+)`, source)
	re := regexp.MustCompile(pat)
	match := re.FindStringSubmatch(m.Render())
	if match == nil {
		return -1
	}
	n, err := strconv.Atoi(match[1])
	if err != nil {
		return -1
	}
	return n
}

// TestGRPCAgentIdentityBinding_TLS_EndToEnd ：被纳管 agent（install token + TLS）拿到 per-agent
// 密钥 → 真客户端签名（v2）通过 → 篡改载荷被拒（v1 通过、v2 拒绝，证明修复点）→ 各类非法签名被拒。
func TestGRPCAgentIdentityBinding_TLS_EndToEnd(t *testing.T) {
	st := store.NewMemoryStore()
	env := startSigTestServer(t, st, true)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// 1) 一次性 install token 签发（自动纳管闭环入口）。
	tok, err := st.IssueToken("dev-onb", "t1", 5*time.Minute)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	cli := newSigTestClient(t, env, true)
	reg, err := cli.Register(ctx, &proto.AgentInfo{
		AgentID: "agent-sig-e2e", Segment: "seg", InstallToken: tok,
		TenantID: "t-claim", // agent 自报租户应被 token 内租户覆盖
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if reg.Secret == "" {
		t.Fatal("token 认证 + TLS 下应下发 per-agent 密钥")
	}
	if want := st.AgentSecret(reg.AgentID); reg.Secret != want {
		t.Fatalf("下发的密钥与库内不一致：resp=%q store=%q", reg.Secret, want)
	}
	if a := st.Agent(reg.AgentID); a == nil || a.TenantID != "t1" {
		t.Fatalf("token 内租户应覆盖 agent 自报租户，得到 %+v", a)
	}

	// 2) 真 agent 签名（v2，覆盖载荷）→ 心跳 / 日志上报通过。
	cli.SetSecret(reg.Secret)
	if err := cli.Heartbeat(ctx, &grpcx.HeartbeatReq{AgentID: reg.AgentID, Status: "online", Load: 5}); err != nil {
		t.Fatalf("v2 签名心跳应通过: %v", err)
	}
	if err := cli.ReportLogs(ctx, &proto.LogReport{AgentID: reg.AgentID, LogName: "syslog", Lines: []proto.LogLine{{Level: "info", Message: "signed"}}}); err != nil {
		t.Fatalf("v2 签名日志上报应通过: %v", err)
	}

	// 3) 未签名请求 → 拒绝（签名验证已启用）。
	err = rawInvoke(ctx, t, env, true, "/opsmesh.v1.Registration/PullTasks",
		nil, &grpcx.PullTasksReq{AgentID: reg.AgentID}, &grpcx.PullTasksResp{})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("无签名请求应 Unauthenticated，得到 %v", err)
	}

	ts := strconv.FormatInt(time.Now().Unix(), 10)
	secret := reg.Secret

	// 4) v1 旧算法签名仍被接受（兼容存量 agent，控制面按限次 WARN 告警）。
	err = rawInvoke(ctx, t, env, true, "/opsmesh.v1.Registration/PullTasks",
		v1MD(secret, ts, reg.AgentID), &grpcx.PullTasksReq{AgentID: reg.AgentID}, &grpcx.PullTasksResp{})
	if err != nil {
		t.Fatalf("v1 签名应被接受（向后兼容）: %v", err)
	}

	// 5) 篡改载荷（同一签名换内容）：
	//    v1 不覆盖载荷 → 控制面无法察觉（旧机制的真实缺陷，此处如实固化）；
	//    v2 覆盖载荷 → 拒绝（本次修复的核心断言）。
	honest := &proto.TaskResult{TaskID: "t-forge", AgentID: reg.AgentID, ExitCode: 0, Stdout: "真实输出"}
	forged := &proto.TaskResult{TaskID: "t-forge", AgentID: reg.AgentID, ExitCode: 0, Stdout: "伪造输出"}
	err = rawInvoke(ctx, t, env, true, "/opsmesh.v1.Registration/ReportResult",
		v1MD(secret, ts, reg.AgentID), forged, &grpcx.Empty{})
	if err != nil {
		t.Fatalf("v1 签名不覆盖载荷，篡改本应被接受（若此处失败说明前提有误，需重写本断言）: %v", err)
	}
	err = rawInvoke(ctx, t, env, true, "/opsmesh.v1.Registration/ReportResult",
		v2MD(secret, ts, reg.AgentID, honest), forged, &grpcx.Empty{})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("v2 签名 + 篡改载荷应 Unauthenticated，得到 %v", err)
	}
	// 未篡改的 v2 签名 → 通过（正例，排除「v2 一律拒绝」的假阳性）。
	err = rawInvoke(ctx, t, env, true, "/opsmesh.v1.Registration/ReportResult",
		v2MD(secret, ts, reg.AgentID, honest), honest, &grpcx.Empty{})
	if err != nil {
		t.Fatalf("v2 签名 + 未篡改载荷应通过: %v", err)
	}

	// 6) 错密钥 / 过期时间戳 / 未知算法 → 一律拒绝。
	err = rawInvoke(ctx, t, env, true, "/opsmesh.v1.Registration/PullTasks",
		v2MD("wrong-secret", ts, reg.AgentID, &grpcx.PullTasksReq{AgentID: reg.AgentID}),
		&grpcx.PullTasksReq{AgentID: reg.AgentID}, &grpcx.PullTasksResp{})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("错密钥应 Unauthenticated，得到 %v", err)
	}
	stale := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
	err = rawInvoke(ctx, t, env, true, "/opsmesh.v1.Registration/PullTasks",
		v1MD(secret, stale, reg.AgentID), &grpcx.PullTasksReq{AgentID: reg.AgentID}, &grpcx.PullTasksResp{})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("过期时间戳应 Unauthenticated，得到 %v", err)
	}
	badAlg := v2MD(secret, ts, reg.AgentID, &grpcx.PullTasksReq{AgentID: reg.AgentID})
	badAlg[grpcx.AgentSignatureAlgMetadataKey] = "v9"
	err = rawInvoke(ctx, t, env, true, "/opsmesh.v1.Registration/PullTasks",
		badAlg, &grpcx.PullTasksReq{AgentID: reg.AgentID}, &grpcx.PullTasksResp{})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("未知算法应 Unauthenticated，得到 %v", err)
	}

	// 7) 可观测性（P1-2）：验签结果与密钥来源必须落指标，且算法标签收敛（v9 → unknown）。
	if got := sigMetric(t, env.metrics, "v2", "ok"); got < 2 {
		t.Fatalf("alg=v2,result=ok 计数应 ≥2（心跳 + 日志上报），得到 %d", got)
	}
	if got := sigMetric(t, env.metrics, "v1", "ok"); got < 1 {
		t.Fatalf("alg=v1,result=ok 计数应 ≥1（兼容路径），得到 %d", got)
	}
	if got := sigMetric(t, env.metrics, "none", "rejected"); got < 1 {
		t.Fatalf("alg=none,result=rejected 计数应 ≥1（缺签名拒绝），得到 %d", got)
	}
	if got := sigMetric(t, env.metrics, "unknown", "rejected"); got < 1 {
		t.Fatalf("alg=unknown,result=rejected 计数应 ≥1（v9 收敛为 unknown），得到 %d", got)
	}
	// per-agent 密钥来源计数应覆盖 v2/v1 的成功验签；预共享兜底不得被误计。
	if got := sigKeySourceMetric(t, env.metrics, "per_agent"); got < 3 {
		t.Fatalf("source=per_agent 计数应 ≥3（v2×2 + v1×1），得到 %d", got)
	}
	if got := sigKeySourceMetric(t, env.metrics, "fleet"); got != 0 {
		t.Fatalf("未配置预共享密钥，source=fleet 计数应为 0，得到 %d", got)
	}
}

// TestGRPCAgentSecretNotDeliveredOnPlaintext ：无 TLS 时即使 install token 有效也不下发密钥
// （明文信道下发 = 向同网段泄漏密钥）。
func TestGRPCAgentSecretNotDeliveredOnPlaintext(t *testing.T) {
	st := store.NewMemoryStore()
	env := startSigTestServer(t, st, false)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tok, err := st.IssueToken("dev-onb", "t1", 5*time.Minute)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	cli := newSigTestClient(t, env, false)
	reg, err := cli.Register(ctx, &proto.AgentInfo{AgentID: "agent-plain", Segment: "seg", InstallToken: tok})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if reg.Secret != "" {
		t.Fatalf("明文连接不得下发密钥，得到 %q", reg.Secret)
	}
	// 库内仍为该 agent 生成了密钥（只是没下发）：后续启用 TLS 时可再下发。
	if st.AgentSecret(reg.AgentID) == "" {
		t.Fatal("库内应仍生成 per-agent 密钥（供 TLS 路径下发）")
	}
}

// TestGRPCAgentRestartWithConsumedInstallToken ：agent 重启容错（P1-2 附带修复）。
//
// 背景：bootstrap 把一次性 install token 写入 <dataDir>/install.token（0600），agent 只读不删；
// 首轮注册消费该 token 后，systemd/机器重启会让 agent 携同一 token 再次注册。
// 旧行为：一律按「已消费」拒绝 → agent 侧 fail-fast 退出 → 纳管 agent 永远无法重启。
// 新行为：agentID 已在库 → 判为「已知 agent 重注册」，放行、租户沿用库内值、不下发密钥。
func TestGRPCAgentRestartWithConsumedInstallToken(t *testing.T) {
	st := store.NewMemoryStore()
	env := startSigTestServer(t, st, true)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tok, err := st.IssueToken("dev-restart", "t1", 5*time.Minute)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	cli := newSigTestClient(t, env, true)
	first, err := cli.Register(ctx, &proto.AgentInfo{AgentID: "agent-restart", Segment: "seg", InstallToken: tok})
	if err != nil {
		t.Fatalf("首轮注册应成功: %v", err)
	}
	if first.Secret == "" {
		t.Fatal("首轮注册（token + TLS）应下发 per-agent 密钥")
	}

	// 模拟重启：本机 install.token 文件仍在，agent 再次携同一（已消费）token 注册。
	second, err := cli.Register(ctx, &proto.AgentInfo{AgentID: first.AgentID, Segment: "seg", InstallToken: tok})
	if err != nil {
		t.Fatalf("重启重注册应放行（否则纳管 agent 无法重启）: %v", err)
	}
	if second.AgentID != first.AgentID {
		t.Fatalf("重注册返回的 agentID 漂移：%q vs %q", second.AgentID, first.AgentID)
	}
	if second.Secret != "" {
		t.Fatalf("已知 agent 重注册不得再次下发密钥（防死 token 换密钥），得到 %q", second.Secret)
	}
	if a := st.Agent(first.AgentID); a == nil || a.TenantID != "t1" {
		t.Fatalf("重注册改写了租户：%+v", a)
	}
	// 身份连续：用首轮下发的密钥（= agent 本机 agent.key）续签仍被接受。
	cli.SetSecret(first.Secret)
	if err := cli.Heartbeat(ctx, &grpcx.HeartbeatReq{AgentID: first.AgentID, Status: "online", Load: 1}); err != nil {
		t.Fatalf("重启后本机 agent.key 签名应继续被接受: %v", err)
	}

	// 未注册过的 agentID 携失效 token → 仍拒绝（不能拿死 token 做首次纳管）。
	unknown := newSigTestClient(t, env, true)
	_, err = unknown.Register(ctx, &proto.AgentInfo{AgentID: "agent-never-enrolled", Segment: "seg", InstallToken: tok})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("未知 agentID + 已消费 token 应 Unauthenticated，得到 %v", err)
	}

	// 无 token 重启（install.token 文件被运维清理的机器）：已知 agent 沿用库内租户放行，
	// 不写空租户、不触发跨租户拒绝。
	bare := newSigTestClient(t, env, true)
	third, err := bare.Register(ctx, &proto.AgentInfo{AgentID: first.AgentID, Segment: "seg"})
	if err != nil {
		t.Fatalf("无 token 的已知 agent 重注册应放行（沿用库内租户）: %v", err)
	}
	if third.Secret != "" {
		t.Fatalf("无 token 重注册不得下发密钥，得到 %q", third.Secret)
	}
	if a := st.Agent(first.AgentID); a == nil || a.TenantID != "t1" {
		t.Fatalf("无 token 重注册改写了租户：%+v", a)
	}
}

// TestGRPCAgentRegisterCrossTenantRefused ：同一 agentID 携他租户重注册 → 拒绝
// （防「注册不硬」时把他人 agent 连设备/任务一起划归自己租户）。
func TestGRPCAgentRegisterCrossTenantRefused(t *testing.T) {
	st := store.NewMemoryStore()
	env := startSigTestServer(t, st, false)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cli := newSigTestClient(t, env, false)
	// 首次注册：无 install token，经 metadata 注入租户（模拟网关盖章）。
	firstCtx := metadata.AppendToOutgoingContext(ctx, "x-tenant-id", "t-owner")
	first, err := cli.Register(firstCtx, &proto.AgentInfo{AgentID: "agent-locked", Segment: "seg"})
	if err != nil {
		t.Fatalf("首次注册应成功: %v", err)
	}
	// 换租户重注册同一 agentID → PermissionDenied。
	secondCtx := metadata.AppendToOutgoingContext(ctx, "x-tenant-id", "t-attacker")
	_, err = cli.Register(secondCtx, &proto.AgentInfo{AgentID: first.AgentID, Segment: "seg"})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("跨租户重注册应 PermissionDenied，得到 %v", err)
	}
	// 原租户与 owner 未被改动。
	if a := st.Agent(first.AgentID); a == nil || a.TenantID != "t-owner" {
		t.Fatalf("agent 租户被改写：%+v", a)
	}
}
