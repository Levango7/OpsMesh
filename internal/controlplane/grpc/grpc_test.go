package grpc

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/grpcx"
	"github.com/Levango7/OpsMesh/internal/logstore"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

func TestSSEDefaultTenant(t *testing.T) {
	if sseDefaultTenant != "default" {
		t.Fatalf("sseDefaultTenant = %q, want default", sseDefaultTenant)
	}
}

func TestAgentSignatureMaxSkew(t *testing.T) {
	expected := 5 * 60
	if int(agentSignatureMaxSkew.Seconds()) != expected {
		t.Fatalf("agentSignatureMaxSkew = %v, want %ds", agentSignatureMaxSkew, expected)
	}
}

// metadata 键与签名算法常量已收敛到 internal/grpcx（agent 与控制面共用一套），
// 此处只断言控制面侧确实引用同一套常量，避免两端各自定义后漂移。
func TestSignatureConstantsSharedWithGrpcx(t *testing.T) {
	if grpcx.AgentSignatureMetadataKey != "agent-signature" {
		t.Fatalf("signature key = %q, want agent-signature", grpcx.AgentSignatureMetadataKey)
	}
	if grpcx.AgentTimestampMetadataKey != "agent-timestamp" {
		t.Fatalf("timestamp key = %q, want agent-timestamp", grpcx.AgentTimestampMetadataKey)
	}
	if grpcx.AgentSignatureAlgV2 != "v2" || grpcx.AgentSignatureAlgV1 != "v1" {
		t.Fatalf("alg constants = %q/%q, want v1/v2", grpcx.AgentSignatureAlgV1, grpcx.AgentSignatureAlgV2)
	}
}

func TestGrpcServerImpl_FieldsAccessible(t *testing.T) {
	g := &GrpcServerImpl{
		Store:       nil,
		RequireAuth: true,
		Cfg:         nil,
		Bus:         nil,
		Metrics:     nil,
		Cmdb:        nil,
		Logs:        nil,
		Publisher:   nil,
	}
	if !g.RequireAuth {
		t.Fatal("RequireAuth should be true")
	}
}

var _ EventPublisher = (*mockPublisher)(nil)

type mockPublisher struct{}

func (m *mockPublisher) PublishEvent(ctx context.Context, typ string, tenantID string, data interface{}) {
}

func TestEventPublisherInterface(t *testing.T) {
	var pub EventPublisher = &mockPublisher{}
	_ = pub
}

// appendCounter 统计被 Append 的次数：M6 转发循环是否真的执行，只能这样锚定
// （旧写法 `_ = ls.Append(...)` 把结果全吞了，读代码看不出写没写进去）。
type appendCounter struct {
	mu      sync.Mutex
	appends int
}

func (c *appendCounter) Append(_ context.Context, _ *logstore.Entry) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.appends++
	return nil
}

func (c *appendCounter) Query(context.Context, logstore.Query) ([]logstore.Entry, error) {
	return nil, nil
}

func (c *appendCounter) Close() error { return nil }

func (c *appendCounter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.appends
}

// readOnlyCounter 是 ES/Loki 形态的桩：自报只读，且 Append 一旦被抓到就计数。
type readOnlyCounter struct{ appendCounter }

func (c *readOnlyCounter) AppendUnsupported() bool { return true }

// reportLogsReq 构造 n 行 agent 日志上报请求。
func reportLogsReq(n int) *grpcx.ReportLogsReq {
	lines := make([]proto.LogLine, 0, n)
	base := time.Now()
	for i := 0; i < n; i++ {
		lines = append(lines, proto.LogLine{Timestamp: base.Add(time.Duration(i) * time.Second), Level: "INFO", Message: "line"})
	}
	return &grpcx.ReportLogsReq{Report: proto.LogReport{
		AgentID: "agent-1", LogName: "/var/log/syslog", Lines: lines,
	}}
}

// newReportLogsServer 构造带 M6 后端的 ReportLogs 服务端（demo store，不校验签名）。
func newReportLogsServer(t *testing.T, ls logstore.LogStore) *GrpcServerImpl {
	t.Helper()
	st := store.NewMemoryStore().WithDemo(true)
	st.Register(&proto.AgentInfo{AgentID: "agent-1", Segment: "seg-a", TenantID: "t1"})
	return &GrpcServerImpl{Store: st, RequireAuth: false, Logs: logstore.NewHandler(ls)}
}

// TestReportLogs_UnsupportedBackendSkipsForwarding 验证只读后端（ES/Loki 形态）下
// 整轮转发被跳过：旧实现逐行 `_ = ls.Append(...)`，每条 agent 日志都被丢弃，
// 而 agent 仍收到 OK —— 静默丢失且零痕迹。
func TestReportLogs_UnsupportedBackendSkipsForwarding(t *testing.T) {
	ls := &readOnlyCounter{}
	g := newReportLogsServer(t, ls)

	if _, err := g.ReportLogs(context.Background(), reportLogsReq(3)); err != nil {
		t.Fatalf("ReportLogs: %v", err)
	}
	if n := ls.count(); n != 0 {
		t.Fatalf("只读后端须整轮跳过写入，实际被 Append %d 次", n)
	}
}

// TestReportLogs_SupportedBackendForwardsEveryLine 反向锚点：可写后端仍逐行转发，
// 防止把"跳过只读后端"实现成"跳过全部转发"。
func TestReportLogs_SupportedBackendForwardsEveryLine(t *testing.T) {
	ls := &appendCounter{}
	g := newReportLogsServer(t, ls)

	if _, err := g.ReportLogs(context.Background(), reportLogsReq(3)); err != nil {
		t.Fatalf("ReportLogs: %v", err)
	}
	if n := ls.count(); n != 3 {
		t.Fatalf("可写后端应逐行转发 3 条，实际 %d 次", n)
	}
}
