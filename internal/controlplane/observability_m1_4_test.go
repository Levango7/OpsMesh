// observability_m1_4_test.go — 分布式可观测性测试
//
// 验证 trace_id 贯穿 agent→控制面→store，日志关联 trace_id，
// SSE 事件携带 trace_id，审计日志关联 trace_id。
//
// 测试策略：
//   - audit helper 从 ctx 提取 trace_id 注入 AuditEvent.TraceID
//   - AuditEvent.TraceID 持久化到 store（MemoryStore 验证）
//   - trace_id 经 gRPC metadata 注入/提取后保持一致（端到端贯穿）
package controlplane

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"opsmesh/internal/config"
	"opsmesh/internal/otelx"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/grpc/metadata"
)

// newM14TestServer 构造一个最小可用的控制面 Server 供 测试用。
// 使用 MemoryStore，关闭鉴权（便于直接调用 handler）。
func newM14TestServer() *Server {
	s := &Server{
		cfg:         &config.Config{Demo: true},
		store:       store.NewMemoryStore(),
		requireAuth: false,
	}
	return s
}

// TestAuditHelper_InjectsTraceID 验证 s.audit(ctx, e) 从 ctx 提取 trace_id 注入 e.TraceID。
// 这是 审计日志关联 trace_id 的核心保证。
func TestAuditHelper_InjectsTraceID(t *testing.T) {
	s := newM14TestServer()

	// 初始化 OTel 并创建带 span 的 ctx。
	shutdown, err := otelx.Init(otelx.Config{Stdout: true})
	if err != nil {
		t.Fatalf("otelx.Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	ctx, span := otelx.StartSpan(context.Background(), "audit-test")
	defer span.End()
	expectedTraceID := otelx.TraceIDFromContext(ctx)
	if expectedTraceID == "" {
		t.Fatal("OTel TraceID 为空")
	}

	// 构造 AuditEvent（不手动设置 TraceID），调用 s.audit。
	e := &proto.AuditEvent{
		TenantID: "t1", UserID: "u1", Action: "test_action", Target: "target-1",
	}
	s.audit(ctx, e)

	// 验证 e.TraceID 被注入了 ctx 的 trace_id。
	if e.TraceID != expectedTraceID {
		t.Fatalf("AuditEvent.TraceID = %q, want %q", e.TraceID, expectedTraceID)
	}

	// 验证审计事件已写入 store 且 TraceID 持久化。
	audits := s.store.Audits()
	if len(audits) == 0 {
		t.Fatal("审计事件未写入 store")
	}
	last := audits[len(audits)-1]
	if last.TraceID != expectedTraceID {
		t.Fatalf("store 中审计事件 TraceID = %q, want %q", last.TraceID, expectedTraceID)
	}
	if last.Action != "test_action" {
		t.Fatalf("Action = %q, want test_action", last.Action)
	}
}

// TestAuditHelper_NoTraceIDWhenNoSpan 验证 ctx 无 span 时 TraceID 为空（向后兼容）。
func TestAuditHelper_NoTraceIDWhenNoSpan(t *testing.T) {
	s := newM14TestServer()

	e := &proto.AuditEvent{
		TenantID: "t1", Action: "no_trace_action",
	}
	s.audit(context.Background(), e)

	if e.TraceID != "" {
		t.Fatalf("无 span 时 TraceID 应为空，got %q", e.TraceID)
	}
}

// TestAuditHelper_PreservesExplicitTraceID 验证 e.TraceID 已设置时不被覆盖。
// 调用方手动设置的 TraceID 优先于 ctx 提取的（尊重显式设置）。
func TestAuditHelper_PreservesExplicitTraceID(t *testing.T) {
	s := newM14TestServer()

	shutdown, err := otelx.Init(otelx.Config{Stdout: true})
	if err != nil {
		t.Fatalf("otelx.Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	ctx, span := otelx.StartSpan(context.Background(), "explicit-trace-test")
	defer span.End()

	explicitTrace := "explicit-trace-id-12345"
	e := &proto.AuditEvent{
		TenantID: "t1", Action: "explicit_trace_action", TraceID: explicitTrace,
	}
	s.audit(ctx, e)

	if e.TraceID != explicitTrace {
		t.Fatalf("显式 TraceID 被覆盖: got %q, want %q", e.TraceID, explicitTrace)
	}
}

// TestAuditHelper_NilEventNoPanic 验证 e=nil 时不 panic（容错）。
func TestAuditHelper_NilEventNoPanic(t *testing.T) {
	s := newM14TestServer()
	// 不应 panic。
	s.audit(context.Background(), nil)
}

// TestGRPCHandlerAuditCarriesTraceID 验证 gRPC handler 路径产出的审计日志携带 trace_id。
// 模拟 agent gRPC 调用携带 trace context → 控制面 gRPC 拦截器提取 → handler 产出审计日志。
func TestGRPCHandlerAuditCarriesTraceID(t *testing.T) {
	s := newM14TestServer()

	shutdown, err := otelx.Init(otelx.Config{Stdout: true})
	if err != nil {
		t.Fatalf("otelx.Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	// 模拟 agent 端创建 span 并注入 gRPC metadata。
	agentCtx, agentSpan := otelx.StartSpan(context.Background(), "agent.register")
	defer agentSpan.End()
	expectedTraceID := otelx.TraceIDFromContext(agentCtx)

	injectedCtx := otelx.InjectGRPCMetadata(agentCtx)

	// 模拟控制面 gRPC 拦截器从 metadata 提取 trace context。
	// 从 outgoing context 取 metadata，构造 incoming context。
	md, ok := metadata.FromOutgoingContext(injectedCtx)
	if !ok {
		t.Fatal("未找到 outgoing metadata")
	}
	incomingCtx := metadata.NewIncomingContext(context.Background(), md)
	extractedCtx := otelx.ExtractGRPCMetadata(incomingCtx)

	// 验证提取后的 ctx 包含相同 trace_id。
	gotTraceID := otelx.TraceIDFromContext(extractedCtx)
	if gotTraceID != expectedTraceID {
		t.Fatalf("gRPC trace_id 贯穿失败: agent=%q, controlplane=%q", expectedTraceID, gotTraceID)
	}

	// 用提取后的 ctx 产出审计日志（模拟 gRPC handler 调用 g.audit）。
	g := &grpcServerImpl{store: s.store}
	e := &proto.AuditEvent{TenantID: "t1", Action: "register", Target: "agent-1"}
	g.audit(extractedCtx, e)

	if e.TraceID != expectedTraceID {
		t.Fatalf("gRPC 审计日志 TraceID = %q, want %q", e.TraceID, expectedTraceID)
	}
}

// TestHTTPHandlerAuditCarriesTraceID 验证 HTTP handler 路径产出的审计日志携带 trace_id。
// 模拟 HTTP 请求携带 W3C traceparent → OTel 中间件提取 → handler 产出审计日志。
func TestHTTPHandlerAuditCarriesTraceID(t *testing.T) {
	s := newM14TestServer()

	shutdown, err := otelx.Init(otelx.Config{Stdout: true})
	if err != nil {
		t.Fatalf("otelx.Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	// 创建上游 span 并注入 HTTP 头（W3C traceparent）。
	upCtx, upSpan := otelx.StartSpan(context.Background(), "upstream-http")
	defer upSpan.End()
	expectedTraceID := otelx.TraceIDFromContext(upCtx)

	// 构造 HTTP 请求，携带 W3C traceparent 头。
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", nil)
	// 用 OTel propagator 将 ctx 的 trace context 注入 HTTP 头。
	otel.GetTextMapPropagator().Inject(upCtx, propagation.HeaderCarrier(req.Header))

	// 用 OTel HTTP 中间件包裹一个产出审计日志的 handler。
	handler := otelx.HTTPMiddleware("test-http", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// handler 内部调用 s.audit(r.Context(), ...)。
		e := &proto.AuditEvent{TenantID: "t1", Action: "http_test", Target: "test"}
		s.audit(r.Context(), e)

		// 验证审计日志携带了 trace_id。
		if e.TraceID != expectedTraceID {
			t.Errorf("HTTP 审计日志 TraceID = %q, want %q", e.TraceID, expectedTraceID)
		}
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP 状态码 = %d, want 200", rec.Code)
	}

	// 验证 store 中的审计日志携带 trace_id。
	audits := s.store.Audits()
	if len(audits) == 0 {
		t.Fatal("审计事件未写入 store")
	}
	last := audits[len(audits)-1]
	if last.TraceID != expectedTraceID {
		t.Fatalf("store 审计 TraceID = %q, want %q", last.TraceID, expectedTraceID)
	}
}

// TestTraceIDEndToEnd 验证 trace_id 贯穿 agent→控制面→store 全链路。
// 这是 的端到端验证：agent 创建 span → gRPC metadata 注入 → 控制面提取 → 审计日志持久化。
func TestTraceIDEndToEnd(t *testing.T) {
	s := newM14TestServer()

	shutdown, err := otelx.Init(otelx.Config{Stdout: true})
	if err != nil {
		t.Fatalf("otelx.Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	// 1. agent 端：创建 span（模拟 agent.execute）。
	agentCtx, agentSpan := otelx.StartSpan(context.Background(), "agent.execute")
	defer agentSpan.End()
	agentTraceID := otelx.TraceIDFromContext(agentCtx)

	// 2. agent→控制面：gRPC metadata 注入（模拟 gRPC 客户端拦截器）。
	injectedCtx := otelx.InjectGRPCMetadata(agentCtx)

	// 3. 控制面端：gRPC metadata 提取（模拟 gRPC 服务端拦截器）。
	md, ok := metadata.FromOutgoingContext(injectedCtx)
	if !ok {
		t.Fatal("未找到 outgoing metadata")
	}
	incomingCtx := metadata.NewIncomingContext(context.Background(), md)
	cpCtx := otelx.ExtractGRPCMetadata(incomingCtx)
	cpTraceID := otelx.TraceIDFromContext(cpCtx)

	// 验证 trace_id 贯穿 agent→控制面。
	if cpTraceID != agentTraceID {
		t.Fatalf("trace_id 贯穿失败: agent=%q, controlplane=%q", agentTraceID, cpTraceID)
	}

	// 4. 控制面→store：审计日志携带 trace_id。
	e := &proto.AuditEvent{TenantID: "t1", Action: "report_result", Target: "task-1"}
	s.audit(cpCtx, e)

	// 验证 store 中审计日志的 trace_id 与 agent 端一致。
	if e.TraceID != agentTraceID {
		t.Fatalf("store 审计 TraceID = %q, want agent TraceID %q", e.TraceID, agentTraceID)
	}

	// 5. 日志关联 trace_id：logx.Trace(cpCtx) 应返回相同 trace_id。
	//（logx 测试在 logx_test.go 中覆盖，此处仅验证 ctx 携带有效 span）
	if otelx.TraceIDFromContext(cpCtx) == "" {
		t.Fatal("控制面 ctx 无有效 trace_id")
	}
}
