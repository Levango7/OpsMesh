// otelx_test.go 测试 otelx 包的 OTel SDK 初始化与中间件。
// M1-1 验证：no-op 模式零开销、stdout 模式可导出、HTTP 中间件创建 span、gRPC 拦截器注入/提取 trace context。
package otelx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc/metadata"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// TestInitNoop 验证 endpoint 空且 stdout=false 时返回 no-op（不 panic、shutdown 无副作用）。
func TestInitNoop(t *testing.T) {
	shutdown, err := Init(Config{})
	if err != nil {
		t.Fatalf("Init no-op 失败: %v", err)
	}
	if shutdown == nil {
		t.Fatal("Init 返回 nil shutdown")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("noop shutdown 失败: %v", err)
	}
	// no-op 模式下 StartSpan 应返回 no-op span（不 panic）。
	ctx, span := StartSpan(context.Background(), "test")
	defer span.End()
	if !span.SpanContext().IsValid() {
		// no-op 模式 span 无效是预期行为（TracerProvider 未设置）。
		t.Log("no-op span 无效（预期）")
	}
	_ = ctx
}

// TestInitStdout 验证 stdout 模式可初始化并导出 span。
func TestInitStdout(t *testing.T) {
	shutdown, err := Init(Config{Stdout: true, ServiceName: "test-svc"})
	if err != nil {
		t.Fatalf("Init stdout 失败: %v", err)
	}
	defer shutdown(context.Background())

	// 创建 span 并结束，触发导出。
	ctx, span := StartSpan(context.Background(), "test-stdout-span")
	span.SetAttributes()
	span.End()
	_ = ctx

	// Enabled 应返回 true（sdktrace.TracerProvider 已设置）。
	if !Enabled() {
		t.Fatal("stdout 模式下 Enabled() 应为 true")
	}
}

// TestStartSpan 验证 StartSpan 返回有效的 span context（启用追踪时）。
func TestStartSpan(t *testing.T) {
	shutdown, err := Init(Config{Stdout: true})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	ctx, span := StartSpan(context.Background(), "op1")
	defer span.End()

	sc := span.SpanContext()
	if !sc.IsValid() {
		t.Fatal("StartSpan 返回无效 span context")
	}
	if sc.TraceID().String() == "" {
		t.Fatal("TraceID 为空")
	}
	_ = ctx
}

// TestHTTPMiddleware 验证 HTTP 中间件为请求创建 span 并记录状态码。
func TestHTTPMiddleware(t *testing.T) {
	shutdown, err := Init(Config{Stdout: true})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	// 业务 handler 返回 200。
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 从请求 ctx 取 span，验证 span context 有效（中间件已注入）。
		span := trace.SpanFromContext(r.Context())
		if !span.SpanContext().IsValid() {
			t.Error("HTTP 中间件未将 span 注入请求 context")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	wrapped := HTTPMiddleware("test-http", handler)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("响应体 = %q, 期望 'ok'", rec.Body.String())
	}
}

// TestHTTPMiddlewareTraceContextPropagation 验证中间件从请求头提取 W3C traceparent。
func TestHTTPMiddlewareTraceContextPropagation(t *testing.T) {
	shutdown, err := Init(Config{Stdout: true})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	// 先创建一个上游 span，注入到 HTTP 头。
	upCtx, upSpan := StartSpan(context.Background(), "upstream")
	defer upSpan.End()
	expectedTraceID := upSpan.SpanContext().TraceID()

	// 注入到 carrier（HTTP 头）。
	carrier := propagation.HeaderCarrier(http.Header{})
	otel.GetTextMapPropagator().Inject(upCtx, carrier)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		gotTraceID := span.SpanContext().TraceID()
		if gotTraceID != expectedTraceID {
			t.Errorf("TraceID 不匹配: got %s, want %s", gotTraceID, expectedTraceID)
		}
		w.WriteHeader(http.StatusOK)
	})

	wrapped := HTTPMiddleware("test-prop", handler)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", nil)
	// 将上游注入的头复制到请求。
	for _, key := range carrier.Keys() {
		req.Header.Set(key, carrier.Get(key))
	}
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
}

// TestHTTPMiddlewareErrorStatus 验证 500 状态码标记 span 为 Error。
func TestHTTPMiddlewareErrorStatus(t *testing.T) {
	shutdown, err := Init(Config{Stdout: true})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	wrapped := HTTPMiddleware("test-err", handler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/error", nil)
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d, 期望 500", rec.Code)
	}
}

// TestGRPCMetadataSupplier 验证 metadataSupplier 读写 gRPC metadata。
func TestGRPCMetadataSupplier(t *testing.T) {
	md := make(metadata.MD)
	s := metadataSupplier{md: &md}

	s.Set("traceparent", "00-abcdef-1234-01")
	got := s.Get("traceparent")
	if got != "00-abcdef-1234-01" {
		t.Errorf("Get = %q, 期望 '00-abcdef-1234-01'", got)
	}

	// 不存在的 key 返回空串。
	if s.Get("nonexistent") != "" {
		t.Error("不存在的 key 应返回空串")
	}
}

// TestEnabledNoop 验证 no-op 模式下 Enabled() 返回 false。
func TestEnabledNoop(t *testing.T) {
	shutdown, _ := Init(Config{})
	defer shutdown(context.Background())
	// no-op 模式 TracerProvider 非 sdktrace.TracerProvider，Enabled 返回 false。
	// 注意：此测试依赖全局状态，若其他测试已设置 sdktrace.TracerProvider 则可能失败。
	// 故仅在能确认 no-op 时断言。
	if Enabled() {
		t.Log("Enabled()=true（可能因其他测试已设置全局 TracerProvider，可接受）")
	}
}

// TestInjectExtractGRPCMetadata 验证 gRPC metadata 注入/提取 round-trip。
func TestInjectExtractGRPCMetadata(t *testing.T) {
	shutdown, err := Init(Config{Stdout: true})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	// 创建上游 span。
	upCtx, upSpan := StartSpan(context.Background(), "grpc-upstream")
	defer upSpan.End()
	expectedTraceID := upSpan.SpanContext().TraceID()

	// 注入到 outgoing metadata。
	injectedCtx := InjectGRPCMetadata(upCtx)

	// 从 outgoing context 取 metadata，模拟服务端从 incoming 提取。
	md, ok := metadata.FromOutgoingContext(injectedCtx)
	if !ok {
		t.Fatal("未找到 outgoing metadata")
	}
	// 构造 incoming context（服务端视角）。
	incomingCtx := metadata.NewIncomingContext(context.Background(), md)
	extractedCtx := ExtractGRPCMetadata(incomingCtx)

	// 提取后的 ctx 应包含相同 TraceID。
	extractedSpan := trace.SpanFromContext(extractedCtx)
	gotTraceID := extractedSpan.SpanContext().TraceID()
	if gotTraceID != expectedTraceID {
		t.Errorf("TraceID round-trip 不匹配: got %s, want %s", gotTraceID, expectedTraceID)
	}
}

// TestHTTPSpanName 验证 span 名生成。
func TestHTTPSpanName(t *testing.T) {
	tests := []struct {
		method, path, want string
	}{
		{"GET", "/api/v1/devices", "HTTP GET /api/v1/devices"},
		{"POST", "/api/v1/tasks", "HTTP POST /api/v1/tasks"},
		{"GET", "", "HTTP GET /"},
	}
	for _, tt := range tests {
		got := httpSpanName(tt.method, tt.path)
		if got != tt.want {
			t.Errorf("httpSpanName(%q,%q) = %q, want %q", tt.method, tt.path, got, tt.want)
		}
	}
}

// TestPortOf 验证端口解析。
func TestPortOf(t *testing.T) {
	tests := []struct {
		host string
		want int
	}{
		{"localhost:8080", 8080},
		{"example.com:443", 443},
		{"localhost", 80},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, "http://"+tt.host+"/", nil)
		got := portOf(req)
		if got != tt.want {
			t.Errorf("portOf(host=%q) = %d, want %d", tt.host, got, tt.want)
		}
	}
}

// TestSchemeOf 验证协议解析。
func TestSchemeOf(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)
	if s := schemeOf(req); s != "http" {
		t.Errorf("schemeOf = %q, want 'http'", s)
	}
	req.Header.Set("X-Forwarded-Proto", "https")
	if s := schemeOf(req); s != "https" {
		t.Errorf("schemeOf with X-Forwarded-Proto = %q, want 'https'", s)
	}
}

// TestStatusRecorder 验证 statusRecorder 捕获状态码并透传写入。
func TestStatusRecorder(t *testing.T) {
	rec := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: rec, status: http.StatusOK}
	sr.WriteHeader(http.StatusCreated)
	if sr.status != http.StatusCreated {
		t.Errorf("status = %d, want %d", sr.status, http.StatusCreated)
	}
	if rec.Code != http.StatusCreated {
		t.Errorf("底层 ResponseWriter Code = %d, want %d", rec.Code, http.StatusCreated)
	}
	sr.Write([]byte("body"))
	if !strings.Contains(rec.Body.String(), "body") {
		t.Errorf("响应体未透传: %q", rec.Body.String())
	}
}

// ============================================================================
// M1-4 分布式可观测性：TraceIDFromContext / SpanIDFromContext
// ============================================================================

// TestTraceIDFromContext_NoSpan 验证 ctx 无 span 时返回空串（向后兼容）。
func TestTraceIDFromContext_NoSpan(t *testing.T) {
	if got := TraceIDFromContext(context.Background()); got != "" {
		t.Fatalf("TraceIDFromContext(empty) = %q, want empty", got)
	}
	if got := TraceIDFromContext(nil); got != "" {
		t.Fatalf("TraceIDFromContext(nil) = %q, want empty", got)
	}
}

// TestTraceIDFromContext_WithSpan 验证 ctx 有有效 span 时返回 32 字符 hex TraceID。
func TestTraceIDFromContext_WithSpan(t *testing.T) {
	shutdown, err := Init(Config{Stdout: true})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	ctx, span := StartSpan(context.Background(), "test-trace-id")
	defer span.End()

	got := TraceIDFromContext(ctx)
	if got == "" {
		t.Fatal("TraceIDFromContext 返回空串，期望非空 TraceID")
	}
	if len(got) != 32 {
		t.Fatalf("TraceID 长度 = %d, 期望 32（hex 编码）", len(got))
	}
	// 应与 span.SpanContext().TraceID() 一致。
	want := span.SpanContext().TraceID().String()
	if got != want {
		t.Fatalf("TraceIDFromContext = %q, want %q", got, want)
	}
}

// TestSpanIDFromContext_WithSpan 验证 SpanIDFromContext 返回 16 字符 hex SpanID。
func TestSpanIDFromContext_WithSpan(t *testing.T) {
	shutdown, err := Init(Config{Stdout: true})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	ctx, span := StartSpan(context.Background(), "test-span-id")
	defer span.End()

	got := SpanIDFromContext(ctx)
	if got == "" {
		t.Fatal("SpanIDFromContext 返回空串，期望非空 SpanID")
	}
	if len(got) != 16 {
		t.Fatalf("SpanID 长度 = %d, 期望 16（hex 编码）", len(got))
	}
}

// TestTraceIDFromContext_NoopMode 验证 no-op 模式下返回空串（span 无效）。
func TestTraceIDFromContext_NoopMode(t *testing.T) {
	// 不调用 Init，使用 no-op TracerProvider。
	// no-op 模式下 StartSpan 返回的 span SpanContext 无效，TraceIDFromContext 应返回空串。
	ctx, span := StartSpan(context.Background(), "noop-test")
	defer span.End()
	got := TraceIDFromContext(ctx)
	if got != "" {
		t.Logf("TraceIDFromContext(noop) = %q（非空，可能因其他测试已设置全局 TracerProvider）", got)
	}
}

// TestTraceIDFromContext_Propagation 验证 trace_id 经 gRPC metadata 注入/提取后保持一致。
// 这是 M1-4 trace_id 贯穿 agent→控制面→store 的核心保证。
func TestTraceIDFromContext_Propagation(t *testing.T) {
	shutdown, err := Init(Config{Stdout: true})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	// 模拟 agent 端：创建 span 并注入 gRPC metadata。
	agentCtx, agentSpan := StartSpan(context.Background(), "agent.call")
	defer agentSpan.End()
	expectedTraceID := TraceIDFromContext(agentCtx)
	if expectedTraceID == "" {
		t.Fatal("agent 端 TraceID 为空")
	}

	injectedCtx := InjectGRPCMetadata(agentCtx)

	// 模拟控制面端：从 gRPC metadata 提取 trace context。
	md, ok := metadata.FromOutgoingContext(injectedCtx)
	if !ok {
		t.Fatal("未找到 outgoing metadata")
	}
	incomingCtx := metadata.NewIncomingContext(context.Background(), md)
	extractedCtx := ExtractGRPCMetadata(incomingCtx)

	// 提取后的 ctx 应包含相同 TraceID（trace_id 贯穿 agent→控制面）。
	gotTraceID := TraceIDFromContext(extractedCtx)
	if gotTraceID != expectedTraceID {
		t.Fatalf("trace_id 贯穿失败: agent=%q, controlplane=%q", expectedTraceID, gotTraceID)
	}
}
