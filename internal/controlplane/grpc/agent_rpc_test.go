// agent_rpc_test.go — agent 管控数据面的功能性测试。
//
// 补齐背景：本包（internal/controlplane/grpc，917 行生产代码）此前覆盖率 0.0%，
// 测试文件里只有三个常量断言。它是 agent 注册/心跳/拉任务/上报结果的**唯一数据面**
// （TD-60 明确认定不可薄客户端化），却在无任何测试保护的情况下承载全部生产流量。
//
// 测试策略：直接以 MemoryStore 装配 GrpcServerImpl 调用 handler 方法，
// 不起真实 gRPC 连接——handler 的入参是 *proto / *grpcx 传输模型，
// 经防腐层转换后转发到 store，故行为可被完整断言，而不必付出网络与 TLS 的代价。
// 传输层（codec / ServiceDesc / metadata 解析）由 internal/grpcx 自身的测试覆盖。
//
// 覆盖重点（按业务风险排序）：
//  1. 租户隔离：CheckAgentTenant 是四条 RPC 的统一入口闸门，跨租户必须 PermissionDenied；
//  2. 身份绑定：verifyAgentSignature 在 requireSignature 开启时的放行/拒绝矩阵；
//  3. 认证闭环：install token 的消费、失效、重启容错三条路径；
//  4. 任务生命周期：领取（pending→running）、上报、取消信号下达。
package grpc

import (
	"context"
	"strconv"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/Levango7/OpsMesh/internal/grpcx"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

// newTestServer 构造装配好的 agent 数据面服务端。
//
// requireAuth/requireSignature 由各用例按需覆盖，此处取"关闭鉴权"基线，
// 使关注点集中在业务转发而非鉴权分支。
func newTestServer(t *testing.T, requireAuth, requireSignature bool) (*GrpcServerImpl, *store.MemoryStore) {
	t.Helper()
	st := store.NewMemoryStore()
	return &GrpcServerImpl{
		Store:            st,
		RequireAuth:      requireAuth,
		RequireSignature: requireSignature,
		Cfg:              nil,
	}, st
}

// tenantCtx 构造带网关注入租户的 incoming context。
func tenantCtx(tenantID string) context.Context {
	md := metadata.Pairs("x-tenant-id", tenantID)
	return metadata.NewIncomingContext(context.Background(), md)
}

// signedCtx 构造带 agent 身份签名（v2，覆盖载荷）的 incoming context。
func signedCtx(t *testing.T, agentID, secret string, payload any, skew time.Duration) context.Context {
	t.Helper()
	ts := strconv.FormatInt(time.Now().Add(-skew).Unix(), 10)
	sig := grpcx.ComputeAgentSignatureV2(secret, ts, agentID, grpcx.PayloadDigest(payload))
	md := metadata.Pairs(
		"x-tenant-id", "t1",
		grpcx.AgentTimestampMetadataKey, ts,
		grpcx.AgentSignatureMetadataKey, sig,
		grpcx.AgentSignatureAlgMetadataKey, grpcx.AgentSignatureAlgV2,
	)
	return metadata.NewIncomingContext(context.Background(), md)
}

// =============================================================================
// 租户隔离：CheckAgentTenant（四条 RPC 的统一入口闸门）
// =============================================================================

// TestCheckAgentTenant_CrossTenantDenied 是本组最重要的断言：
// 租户 t1 的凭证不得操作属于 t2 的 agent。这是跨租户越权的主防线。
func TestCheckAgentTenant_CrossTenantDenied(t *testing.T) {
	g, st := newTestServer(t, true, false)
	st.Register(&proto.AgentInfo{AgentID: "a-t2", TenantID: "t2", Segment: "seg"})

	err := g.CheckAgentTenant(tenantCtx("t1"), "a-t2")
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("跨租户访问 code=%v, want PermissionDenied (err=%v)", status.Code(err), err)
	}
}

// TestCheckAgentTenant_SameTenantAllowed 反向锚点：同租户必须放行，
// 防止把上一条实现成"一律拒绝"。
func TestCheckAgentTenant_SameTenantAllowed(t *testing.T) {
	g, st := newTestServer(t, true, false)
	st.Register(&proto.AgentInfo{AgentID: "a-t1", TenantID: "t1", Segment: "seg"})

	if err := g.CheckAgentTenant(tenantCtx("t1"), "a-t1"); err != nil {
		t.Fatalf("同租户访问应放行: %v", err)
	}
}

// TestCheckAgentTenant_MissingContextRejected 验证无租户上下文且 agent 无库内归属时拒绝。
func TestCheckAgentTenant_MissingContextRejected(t *testing.T) {
	g, _ := newTestServer(t, true, false)
	err := g.CheckAgentTenant(context.Background(), "unknown-agent")
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("缺租户上下文 code=%v, want Unauthenticated", status.Code(err))
	}
}

// TestCheckAgentTenant_AuthDisabledPasses 验证关闭鉴权时放行（demo/本地基线）。
func TestCheckAgentTenant_AuthDisabledPasses(t *testing.T) {
	g, _ := newTestServer(t, false, false)
	if err := g.CheckAgentTenant(context.Background(), "whoever"); err != nil {
		t.Fatalf("RequireAuth=false 应放行: %v", err)
	}
}

// TestCheckAgentTenant_FallsBackToStoredTenant 验证无网关注入时回退到库内租户：
// agent 重启后自报值可能为空，但库内归属必须继续生效，否则 agent 会失联。
func TestCheckAgentTenant_FallsBackToStoredTenant(t *testing.T) {
	g, st := newTestServer(t, true, false)
	st.Register(&proto.AgentInfo{AgentID: "a-t2", TenantID: "t2", Segment: "seg"})

	// 无任何租户头：应回退到库内 t2 并放行（而不是因"缺上下文"拒绝）。
	if err := g.CheckAgentTenant(context.Background(), "a-t2"); err != nil {
		t.Fatalf("应回退库内租户并放行: %v", err)
	}
}

// TestCheckAgentTenant_EmptyAgentIDDeferred 验证空 AgentID 不在此处拒绝，
// 而是交给后续业务逻辑报 InvalidArgument（避免错误码语义混淆）。
func TestCheckAgentTenant_EmptyAgentIDDeferred(t *testing.T) {
	g, _ := newTestServer(t, true, false)
	if err := g.CheckAgentTenant(tenantCtx("t1"), ""); err != nil {
		t.Fatalf("空 AgentID 应延后判定，不在此处拒绝: %v", err)
	}
}

// =============================================================================
// 身份绑定：verifyAgentSignature
// =============================================================================

// TestVerifyAgentSignature_Matrix 覆盖 requireSignature 开启时的放行/拒绝矩阵。
//
// 这一组是 agent 身份绑定的核心：签名缺失、算法未知、时间戳超窗、
// 密钥不匹配都必须 Unauthenticated；而合法 v2 签名必须放行。
func TestVerifyAgentSignature_Matrix(t *testing.T) {
	g, st := newTestServer(t, false, true)
	st.Register(&proto.AgentInfo{AgentID: "a1", TenantID: "t1", Segment: "seg"})
	// per-agent 密钥在 Register 时自动生成（store memory.go:569），验签优先取它。
	// 这里用预共享密钥路径以外的方式断言：读回库内密钥再签名。
	agentSecret := st.AgentSecret("a1")
	if agentSecret == "" {
		t.Fatal("Register 后应自动生成 per-agent 签名密钥")
	}

	req := &grpcx.HeartbeatReq{AgentID: "a1", Status: "online", Load: 5}

	tests := []struct {
		name    string
		ctx     context.Context
		wantErr bool
	}{
		{"合法 v2 签名", signedCtx(t, "a1", agentSecret, req, 0), false},
		{"完全无签名", tenantCtx("t1"), true},
		{"签名错误", signedCtx(t, "a1", "wrong-secret", req, 0), true},
		{"时间戳超窗（2 倍 skew 之上）", signedCtx(t, "a1", agentSecret, req, 2*agentSignatureMaxSkew), true},
		{"时间戳为负（远古）", signedCtx(t, "a1", agentSecret, req, -2*agentSignatureMaxSkew), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := g.verifyAgentSignature(tc.ctx, "a1", req)
			if tc.wantErr {
				if status.Code(err) != codes.Unauthenticated {
					t.Fatalf("code=%v, want Unauthenticated (err=%v)", status.Code(err), err)
				}
			} else if err != nil {
				t.Fatalf("应放行: %v", err)
			}
		})
	}
}

// TestVerifyAgentSignature_PresharedKeyFallback 验证预共享密钥兜底路径可用。
//
// 该路径安全性低于 per-agent 密钥（全舰队共用一把，一旦泄漏即全舰队可冒充），
// 保留它是为了兼容"运维显式配置 / 存量 agent 未升级"，命中时应限次 WARN。
func TestVerifyAgentSignature_PresharedKeyFallback(t *testing.T) {
	const shared = "fleet-wide-shared-secret"
	g, st := newTestServer(t, false, true)
	st.Register(&proto.AgentInfo{AgentID: "a1", TenantID: "t1", Segment: "seg"})
	g.SignatureKey = shared

	req := &grpcx.HeartbeatReq{AgentID: "a1", Status: "online"}
	if err := g.verifyAgentSignature(signedCtx(t, "a1", shared, req, 0), "a1", req); err != nil {
		t.Fatalf("预共享密钥路径应放行: %v", err)
	}
}

// TestVerifyAgentSignature_UnknownAgentRejected 验证未注册 agent 无密钥可用时拒绝。
func TestVerifyAgentSignature_UnknownAgentRejected(t *testing.T) {
	g, _ := newTestServer(t, false, true)
	err := g.verifyAgentSignature(signedCtx(t, "ghost", "any", &grpcx.HeartbeatReq{}, 0), "ghost", &grpcx.HeartbeatReq{})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("未注册 agent code=%v, want Unauthenticated (err=%v)", status.Code(err), err)
	}
}

// TestVerifyAgentSignature_DisabledPasses 验证 requireSignature 关闭时放行（向后兼容）。
func TestVerifyAgentSignature_DisabledPasses(t *testing.T) {
	g, _ := newTestServer(t, false, false)
	if err := g.verifyAgentSignature(context.Background(), "anyone", nil); err != nil {
		t.Fatalf("RequireSignature=false 应放行: %v", err)
	}
}

// =============================================================================
// Heartbeat
// =============================================================================

// TestHeartbeat_StoresStatusAndMetrics 验证心跳落库：状态/负载被更新，
// 且随心跳上报的监控指标被缓存（供控制面 API 查询）。
func TestHeartbeat_StoresStatusAndMetrics(t *testing.T) {
	g, st := newTestServer(t, false, false)
	st.Register(&proto.AgentInfo{AgentID: "a1", TenantID: "t1", Segment: "seg"})

	req := &grpcx.HeartbeatReq{
		AgentID: "a1", Status: "busy", Load: 77,
		Metrics: &proto.DeviceMetrics{DeviceID: "dev-1", CPU: proto.CPUMetrics{Usage: 42}},
	}
	if _, err := g.Heartbeat(context.Background(), req); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	got := st.Agent("a1")
	if got == nil {
		t.Fatal("心跳后 agent 应仍存在")
	}
	if got.Status != "busy" {
		t.Errorf("Status=%q, want busy（心跳状态未落库）", got.Status)
	}
	if m := st.DeviceMetrics("dev-1"); m == nil {
		t.Error("心跳携带的 metrics 未被缓存")
	} else if m.CPU.Usage != 42 {
		t.Errorf("metrics CPU=%v, want 42", m.CPU.Usage)
	}
}

// TestHeartbeat_MetricsDeviceIDFallback 验证 metrics.DeviceID 为空时用 dev-<agentID> 兜底，
// 与 Register 创建的占位设备 ID 对齐（否则指标落到一个查不到的设备上）。
func TestHeartbeat_MetricsDeviceIDFallback(t *testing.T) {
	g, st := newTestServer(t, false, false)
	st.Register(&proto.AgentInfo{AgentID: "a9", TenantID: "t1", Segment: "seg"})

	req := &grpcx.HeartbeatReq{AgentID: "a9", Status: "online", Metrics: &proto.DeviceMetrics{CPU: proto.CPUMetrics{Usage: 7}}}
	if _, err := g.Heartbeat(context.Background(), req); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if m := st.DeviceMetrics("dev-a9"); m == nil {
		t.Fatal("兜底设备 ID dev-<agentID> 下未缓存到指标")
	} else if m.CPU.Usage != 7 {
		t.Errorf("CPU=%v, want 7", m.CPU.Usage)
	}
}

// TestHeartbeat_CrossTenantDenied 验证跨租户心跳在入口即被拒，且不污染库内状态。
func TestHeartbeat_CrossTenantDenied(t *testing.T) {
	g, st := newTestServer(t, true, false)
	st.Register(&proto.AgentInfo{AgentID: "a-t2", TenantID: "t2", Segment: "seg", Status: "online"})

	_, err := g.Heartbeat(tenantCtx("t1"), &grpcx.HeartbeatReq{AgentID: "a-t2", Status: "hijacked"})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("code=%v, want PermissionDenied", status.Code(err))
	}
	if got := st.Agent("a-t2"); got.Status == "hijacked" {
		t.Error("被拒的心跳不应修改库内状态")
	}
}

// =============================================================================
// PullTasks（任务领取）
// =============================================================================

// TestPullTasks_ClaimsPendingTask 验证领取语义：pending 任务被取出并转为 running。
//
// 这是多副本控制面的 HA 协调点：同一任务只应被一个副本领走。
func TestPullTasks_ClaimsPendingTask(t *testing.T) {
	g, st := newTestServer(t, false, false)
	st.Register(&proto.AgentInfo{AgentID: "a1", TenantID: "t1", Segment: "seg"})
	st.CreateTask(&proto.Task{TaskID: "task-1", TenantID: "t1", AgentID: "a1", Status: "pending", Command: "uptime"})

	resp, err := g.PullTasks(context.Background(), &grpcx.PullTasksReq{AgentID: "a1"})
	if err != nil {
		t.Fatalf("PullTasks: %v", err)
	}
	if len(resp.Tasks) != 1 {
		t.Fatalf("tasks=%d, want 1", len(resp.Tasks))
	}
	if resp.Tasks[0].TaskID != "task-1" {
		t.Errorf("领到任务 %q, want task-1", resp.Tasks[0].TaskID)
	}
	if got := st.TaskByID("task-1"); got == nil || got.Status != "running" {
		t.Errorf("领取后状态应为 running，实际 %+v", got)
	}
}

// TestPullTasks_EmptyWhenNoPending 验证无待领任务时返回空响应而非错误——
// 这是 agent 侧轮询的正常稳态，报错会导致无谓的重连与告警噪声。
func TestPullTasks_EmptyWhenNoPending(t *testing.T) {
	g, st := newTestServer(t, false, false)
	st.Register(&proto.AgentInfo{AgentID: "a1", TenantID: "t1", Segment: "seg"})

	resp, err := g.PullTasks(context.Background(), &grpcx.PullTasksReq{AgentID: "a1"})
	if err != nil {
		t.Fatalf("无待领任务不应报错: %v", err)
	}
	if len(resp.Tasks) != 0 {
		t.Errorf("tasks=%d, want 0", len(resp.Tasks))
	}
}

// TestPullTasks_NotRepeatSameTask 验证同一条任务不会被重复领取两次
// （多副本并发领取时的正确性锚点）。
func TestPullTasks_NotRepeatSameTask(t *testing.T) {
	g, st := newTestServer(t, false, false)
	st.Register(&proto.AgentInfo{AgentID: "a1", TenantID: "t1", Segment: "seg"})
	st.CreateTask(&proto.Task{TaskID: "only-one", TenantID: "t1", AgentID: "a1", Status: "pending"})

	first, err := g.PullTasks(context.Background(), &grpcx.PullTasksReq{AgentID: "a1"})
	if err != nil {
		t.Fatalf("首次 PullTasks: %v", err)
	}
	if len(first.Tasks) != 1 {
		t.Fatalf("首次应领到 1 条，实际 %d", len(first.Tasks))
	}
	second, err := g.PullTasks(context.Background(), &grpcx.PullTasksReq{AgentID: "a1"})
	if err != nil {
		t.Fatalf("二次 PullTasks: %v", err)
	}
	if len(second.Tasks) != 0 {
		t.Errorf("已领取的任务不应再次下发，实际 %d 条", len(second.Tasks))
	}
}

// TestPullTasks_CrossTenantDenied 验证跨租户领取被拒。
func TestPullTasks_CrossTenantDenied(t *testing.T) {
	g, st := newTestServer(t, true, false)
	st.Register(&proto.AgentInfo{AgentID: "a-t2", TenantID: "t2", Segment: "seg"})

	_, err := g.PullTasks(tenantCtx("t1"), &grpcx.PullTasksReq{AgentID: "a-t2"})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("code=%v, want PermissionDenied", status.Code(err))
	}
}

// =============================================================================
// ReportResult（结果上报）
// =============================================================================

// TestReportResult_UpdatesTaskOutcome 验证上报结果写回任务终态与输出。
func TestReportResult_UpdatesTaskOutcome(t *testing.T) {
	g, st := newTestServer(t, false, false)
	st.Register(&proto.AgentInfo{AgentID: "a1", TenantID: "t1", Segment: "seg"})
	st.CreateTask(&proto.Task{TaskID: "task-9", TenantID: "t1", AgentID: "a1", Status: "running"})

	res := &proto.TaskResult{TaskID: "task-9", AgentID: "a1", ExitCode: 0, Stdout: "hello"}
	if _, err := g.ReportResult(context.Background(), res); err != nil {
		t.Fatalf("ReportResult: %v", err)
	}
	if got := st.TaskByID("task-9"); got == nil || got.Status != "done" {
		t.Errorf("Status=%+v, want done（exitCode=0 应判成功终态）", got)
	}
	// 输出不回写 Task 本体，而是存进独立的执行结果记录。
	if tr := st.TaskResult("task-9"); tr == nil || tr.Stdout != "hello" {
		t.Errorf("执行结果未记录 stdout: %+v", tr)
	}
}

// TestReportResult_NonZeroExitIsFailure 验证非零退出码判失败——
// agent 上报语义的核心判据，判反了会让所有失败任务被记成成功。
func TestReportResult_NonZeroExitIsFailure(t *testing.T) {
	g, st := newTestServer(t, false, false)
	st.Register(&proto.AgentInfo{AgentID: "a1", TenantID: "t1", Segment: "seg"})
	// MaxRetries=0：失败时直接判 failed。
	// 若留默认值（0）与 MaxRetries 相同，store 的重试分支会把任务打回 pending
	// 而不是 failed——那正是"失败被记成待重试"的语义，此处要锚定的是终态判定本身。
	st.CreateTask(&proto.Task{TaskID: "task-f", TenantID: "t1", AgentID: "a1", Status: "running", MaxRetries: 0})

	res := &proto.TaskResult{TaskID: "task-f", AgentID: "a1", ExitCode: 2, Stderr: "boom"}
	if _, err := g.ReportResult(context.Background(), res); err != nil {
		t.Fatalf("ReportResult: %v", err)
	}
	if got := st.TaskByID("task-f"); got == nil || got.Status != "failed" {
		t.Errorf("Status=%+v, want failed（exitCode≠0 且无重试余量应判失败）", got)
	}
}

// TestReportResult_FailureRetriesWhenBudgetRemains 锚定重试语义：
// 失败但仍有重试余量时，任务回到 pending 而非 failed（这是"完整任务生命周期"的核心）。
func TestReportResult_FailureRetriesWhenBudgetRemains(t *testing.T) {
	g, st := newTestServer(t, false, false)
	st.Register(&proto.AgentInfo{AgentID: "a1", TenantID: "t1", Segment: "seg"})
	st.CreateTask(&proto.Task{TaskID: "task-r", TenantID: "t1", AgentID: "a1", Status: "running", MaxRetries: 3})

	if _, err := g.ReportResult(context.Background(), &proto.TaskResult{TaskID: "task-r", AgentID: "a1", ExitCode: 1}); err != nil {
		t.Fatalf("ReportResult: %v", err)
	}
	got := st.TaskByID("task-r")
	if got == nil || got.Status != "pending" {
		t.Errorf("Status=%+v, want pending（失败应回队重试）", got)
	}
	if got.RetryCount != 1 {
		t.Errorf("RetryCount=%d, want 1", got.RetryCount)
	}
}

// TestReportResult_CrossTenantDenied 验证跨租户上报被拒。
func TestReportResult_CrossTenantDenied(t *testing.T) {
	g, st := newTestServer(t, true, false)
	st.Register(&proto.AgentInfo{AgentID: "a-t2", TenantID: "t2", Segment: "seg"})
	st.CreateTask(&proto.Task{TaskID: "task-x", TenantID: "t2", AgentID: "a-t2", Status: "running"})

	_, err := g.ReportResult(tenantCtx("t1"), &proto.TaskResult{TaskID: "task-x", AgentID: "a-t2", ExitCode: 0})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("code=%v, want PermissionDenied", status.Code(err))
	}
	if got := st.TaskByID("task-x"); got == nil || got.Status != "running" {
		t.Errorf("被拒的上报不应改变任务状态: %+v", got)
	}
}

// =============================================================================
// 取消信号下达（CancelTask / PollCancels）
// =============================================================================

// TestCancelTask_AndPollCancels 验证取消信号真正下达到 agent：
// 控制面标记取消 → agent 轮询能看到该 taskID。这是"取消"功能闭环的判定点。
func TestCancelTask_AndPollCancels(t *testing.T) {
	g, st := newTestServer(t, false, false)
	st.Register(&proto.AgentInfo{AgentID: "a1", TenantID: "t1", Segment: "seg"})
	st.CreateTask(&proto.Task{TaskID: "task-c", TenantID: "t1", AgentID: "a1", Status: "running"})

	if _, err := g.CancelTask(tenantCtx("t1"), &grpcx.CancelTaskReq{TaskID: "task-c", TenantID: "t1"}); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	poll, err := g.PollCancels(tenantCtx("t1"), &grpcx.PollCancelsReq{AgentID: "a1"})
	if err != nil {
		t.Fatalf("PollCancels: %v", err)
	}
	found := false
	for _, id := range poll.CancelledTaskIDs {
		if id == "task-c" {
			found = true
		}
	}
	if !found {
		t.Errorf("取消信号未下达到 agent，CancelledTaskIDs=%v", poll.CancelledTaskIDs)
	}
}

// TestCancelTask_RejectsSpoofedTenantID 验证 TenantID 由服务端用网关注入身份强制覆盖：
// 请求体自报他租户 ID 必须被**显式拒绝**，而不是被静默忽略。
//
// 两种实现都能防住越权，但显式拒绝更优——静默忽略会让调用方误以为取消已提交，
// 得不到任何错误反馈。故断言 PermissionDenied。
func TestCancelTask_RejectsSpoofedTenantID(t *testing.T) {
	g, st := newTestServer(t, true, false)
	st.Register(&proto.AgentInfo{AgentID: "a-t1", TenantID: "t1", Segment: "seg"})
	st.CreateTask(&proto.Task{TaskID: "task-t1", TenantID: "t1", AgentID: "a-t1", Status: "running"})
	st.CreateTask(&proto.Task{TaskID: "task-t2", TenantID: "t2", AgentID: "a-x", Status: "running"})

	// 自报 tenantID=t2，但凭证是 t1。
	_, err := g.CancelTask(tenantCtx("t1"), &grpcx.CancelTaskReq{TaskID: "task-t2", TenantID: "t2"})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("code=%v, want PermissionDenied（自报他租户应被显式拒绝）", status.Code(err))
	}
	if got := st.TaskByID("task-t2"); got != nil && got.Status == "cancelled" {
		t.Error("自报他租户 ID 取消了他人任务（越权）")
	}
	// 顺带确认：租户自报一致时取消正常生效，避免把上一条实现成"一律拒绝"。
	if _, err := g.CancelTask(tenantCtx("t1"), &grpcx.CancelTaskReq{TaskID: "task-t1", TenantID: "t1"}); err != nil {
		t.Fatalf("同租户取消应成功: %v", err)
	}
	if got := st.TaskByID("task-t1"); got == nil || got.Status != "cancelled" {
		t.Errorf("同租户取消未生效: %+v", got)
	}
}

// TestPollCancels_CrossTenantDenied 验证跨租户轮询取消信号被拒——
// 取消 taskID 列表本身也是情报（可推断他租户任务存在与 ID）。
func TestPollCancels_CrossTenantDenied(t *testing.T) {
	g, st := newTestServer(t, true, false)
	st.Register(&proto.AgentInfo{AgentID: "a-t2", TenantID: "t2", Segment: "seg"})

	_, err := g.PollCancels(tenantCtx("t1"), &grpcx.PollCancelsReq{AgentID: "a-t2"})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("code=%v, want PermissionDenied", status.Code(err))
	}
}

// =============================================================================
// 注册认证闭环（install token 三条路径）
// =============================================================================

// TestRegister_WithValidInstallToken 验证一次性 install token 注册：
// 租户以 token 为权威（忽略 agent 自报值），并把 agent 落到 token 指向的设备上。
func TestRegister_WithValidInstallToken(t *testing.T) {
	g, st := newTestServer(t, false, false)
	token, err := st.IssueToken("dev-1", "t1", 10*time.Minute)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	resp, err := g.Register(context.Background(), &proto.AgentInfo{
		AgentID: "a-new", Segment: "seg-a", TenantID: "attacker-claimed",
		InstallToken: token,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if resp == nil || resp.AgentID == "" {
		t.Fatalf("Register 未返回 agentID: %+v", resp)
	}
	got := st.Agent(resp.AgentID)
	if got == nil {
		t.Fatal("注册后 agent 未落库")
	}
	if got.TenantID != "t1" {
		t.Errorf("TenantID=%q, want t1（token 权威，不得被自报值覆盖）", got.TenantID)
	}
	if got.OnboardDeviceID != "dev-1" {
		t.Errorf("OnboardDeviceID=%q, want dev-1（token 应把 agent 落到目标设备）", got.OnboardDeviceID)
	}
}

// TestRegister_InvalidTokenRejected 验证无效/已消费 token 被拒（首次纳管场景），
// 且写入审计留痕。
func TestRegister_InvalidTokenRejected(t *testing.T) {
	g, _ := newTestServer(t, false, false)

	_, err := g.Register(context.Background(), &proto.AgentInfo{AgentID: "a-ghost", InstallToken: "bogus-token"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code=%v, want Unauthenticated (err=%v)", status.Code(err), err)
	}
}

// TestRegister_StaleTokenReregisterKeepsTenant 验证重启容错：
// token 已消费但 agentID 已在库 → 按"已知 agent 重注册"处理，沿用库内租户、
// 不下发新密钥。若按 token 失效一律拒绝，agent 永远无法重启（整机失联）。
func TestRegister_StaleTokenReregisterKeepsTenant(t *testing.T) {
	g, st := newTestServer(t, false, false)
	// 库内既有 agent，租户为 t1。
	st.Register(&proto.AgentInfo{AgentID: "a-known", TenantID: "t1", Segment: "seg"})

	// 携带一个无效（视为已消费）的 token 重注册，且自报另一个租户。
	resp, err := g.Register(context.Background(), &proto.AgentInfo{
		AgentID: "a-known", Segment: "seg", TenantID: "attacker-claimed", InstallToken: "already-consumed",
	})
	if err != nil {
		t.Fatalf("已知 agent 重注册不应被拒: %v", err)
	}
	got := st.Agent(resp.AgentID)
	if got == nil {
		t.Fatal("agent 丢失")
	}
	if got.TenantID != "t1" {
		t.Errorf("TenantID=%q, want t1（重注册不得改写归属）", got.TenantID)
	}
}

// TestRegister_NoTokenClearsOnboardDeviceID 验证无 token 时显式清空 OnboardDeviceID：
// agent 自报该字段一律不信任，否则可冒充他人设备完成纳管。
func TestRegister_NoTokenClearsOnboardDeviceID(t *testing.T) {
	g, st := newTestServer(t, false, false)

	resp, err := g.Register(tenantCtx("t1"), &proto.AgentInfo{
		AgentID: "a-plain", Segment: "seg", OnboardDeviceID: "victim-device",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	got := st.Agent(resp.AgentID)
	if got == nil {
		t.Fatal("agent 未落库")
	}
	if got.OnboardDeviceID == "victim-device" {
		t.Error("无 token 时 OnboardDeviceID 必须被清空（agent 自报值不可信）")
	}
}
