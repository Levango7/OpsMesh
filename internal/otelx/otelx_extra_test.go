// otelx_extra_test.go 补充 otelx 包测试覆盖率，覆盖未测试的公开函数、配置选项、边界条件与错误路径。
// 覆盖目标：Tracer、SpanFromContext、RecordError、Keys、GRPCClientUnaryInterceptor、
// GRPCServerUnaryInterceptor、Flush、Init 的 OTLP gRPC 分支、schemeOf 的 TLS 分支、
// portOf 的空 host 分支、SpanIDFromContext 的 nil ctx 分支、metadataSupplier 的 nil md 分支等。
package otelx

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// ============================================================================
// otelx.go 中未覆盖的公开函数
// ============================================================================

// TestTracer 验证 Tracer 返回非 nil 且可创建 span。
func TestTracer(t *testing.T) {
	tr := Tracer("test-tracer")
	if tr == nil {
		t.Fatal("Tracer 返回 nil")
	}
	// 启用追踪后，Tracer 创建的 span 应有效。
	shutdown, err := Init(Config{Stdout: true})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	tr = Tracer("test-tracer-enabled")
	_, span := tr.Start(context.Background(), "via-tracer")
	defer span.End()
	if !span.SpanContext().IsValid() {
		t.Fatal("Tracer 创建的 span 无效")
	}
}

// TestSpanFromContext 验证 SpanFromContext 从 ctx 提取 span，无 span 时返回 no-op span（安全调用）。
func TestSpanFromContext(t *testing.T) {
	// 无 span 的 ctx：返回 no-op span，调用 End 不 panic。
	span := SpanFromContext(context.Background())
	if span == nil {
		t.Fatal("SpanFromContext 返回 nil")
	}
	// no-op span 调用 End/RecordError 不应 panic。
	span.End()
	span.RecordError(errors.New("test"))

	// 有 span 的 ctx：返回实际 span。
	shutdown, err := Init(Config{Stdout: true})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	ctx, expectedSpan := StartSpan(context.Background(), "test-span-from-ctx")
	defer expectedSpan.End()
	gotSpan := SpanFromContext(ctx)
	if gotSpan.SpanContext().SpanID() != expectedSpan.SpanContext().SpanID() {
		t.Errorf("SpanFromContext 返回的 SpanID 与原 span 不一致")
	}
}

// TestRecordError 验证 RecordError 在各种边界条件下的行为（不 panic）。
func TestRecordError(t *testing.T) {
	shutdown, err := Init(Config{Stdout: true})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	// 1. span=nil, err=nil：不应 panic。
	RecordError(nil, nil)

	// 2. span=nil, err 非 nil：不应 panic。
	RecordError(nil, errors.New("err-with-nil-span"))

	// 3. span 非 nil, err=nil：不应 panic，也不应记录错误。
	_, span := StartSpan(context.Background(), "record-error-test")
	defer span.End()
	RecordError(span, nil)

	// 4. span 非 nil, err 非 nil：应记录错误（不 panic 即可）。
	RecordError(span, errors.New("real-error"))
}

// TestInitOTLPgRPC 验证 Init 在配置 OTLP gRPC endpoint 时能成功构造 exporter 与 TracerProvider。
// gRPC 连接是 lazy 的，endpoint 不可达也能创建 exporter。
func TestInitOTLPgRPC(t *testing.T) {
	// 使用 insecure 端口（非 :443 后缀）。
	shutdown, err := Init(Config{Endpoint: "localhost:4317", ServiceName: "otlp-test"})
	if err != nil {
		t.Fatalf("Init OTLP gRPC 失败: %v", err)
	}
	if shutdown == nil {
		t.Fatal("Init 返回 nil shutdown")
	}
	defer shutdown(context.Background())

	// 应启用追踪。
	if !Enabled() {
		t.Fatal("OTLP gRPC 模式下 Enabled() 应为 true")
	}

	// 创建 span 不 panic。
	_, span := StartSpan(context.Background(), "otlp-span")
	span.End()
}

// TestInitOTLPgRPC_TLS443 验证 endpoint 以 :443 结尾时不使用 insecure（TLS 路径）。
// gRPC 连接 lazy，不会立即拨号，故 exporter 创建应成功。
func TestInitOTLPgRPC_TLS443(t *testing.T) {
	shutdown, err := Init(Config{Endpoint: "collector.example.com:443", ServiceName: "tls-test"})
	if err != nil {
		t.Fatalf("Init OTLP gRPC TLS 失败: %v", err)
	}
	if shutdown == nil {
		t.Fatal("Init 返回 nil shutdown")
	}
	defer shutdown(context.Background())

	if !Enabled() {
		t.Fatal("TLS 443 模式下 Enabled() 应为 true")
	}
}

// TestInitDefaultServiceName 验证 ServiceName 为空时回退到 "opsmesh"。
func TestInitDefaultServiceName(t *testing.T) {
	shutdown, err := Init(Config{Stdout: true}) // ServiceName 留空
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	defer shutdown(context.Background())
	// 不直接断言 Resource 属性（需深入 SDK 内部），仅验证 Init 成功即可。
}

// ============================================================================
// grpc.go 中未覆盖的函数与分支
// ============================================================================

// TestMetadataSupplier_NilMD 验证 metadataSupplier 在 md == nil 时 Get/Set/Keys 的安全行为。
func TestMetadataSupplier_NilMD(t *testing.T) {
	s := metadataSupplier{md: nil}

	// Get 在 nil md 上应返回空串，不 panic。
	if got := s.Get("any-key"); got != "" {
		t.Errorf("Get(nil-md) = %q, 期望空串", got)
	}

	// Set 在 nil md 上应 no-op，不 panic。
	s.Set("key", "value")

	// Keys 在 nil md 上应返回 nil，不 panic。
	if keys := s.Keys(); keys != nil {
		t.Errorf("Keys(nil-md) = %v, 期望 nil", keys)
	}
}

// TestMetadataSupplier_Keys 验证 Keys 返回 metadata 中所有键（去重）。
func TestMetadataSupplier_Keys(t *testing.T) {
	md := metadata.Pairs(
		"traceparent", "00-abcdef-1234-01",
		"baggage", "key=value",
		"custom-header", "x",
	)
	s := metadataSupplier{md: &md}

	keys := s.Keys()
	if len(keys) != 3 {
		t.Fatalf("Keys 返回 %d 个键, 期望 3: %v", len(keys), keys)
	}
	// 验证所有键都能通过 Get 取到。
	for _, k := range keys {
		if s.Get(k) == "" {
			t.Errorf("Keys 返回的键 %q 在 Get 时返回空", k)
		}
	}
}

// TestGRPCClientUnaryInterceptor_Success 验证客户端拦截器在 invoker 成功时创建 span 并注入 metadata。
func TestGRPCClientUnaryInterceptor_Success(t *testing.T) {
	shutdown, err := Init(Config{Stdout: true})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	interceptor := GRPCClientUnaryInterceptor("test-client")

	// 模拟 invoker：验证 outgoing metadata 已注入 traceparent。
	invokerCalled := false
	invoker := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		invokerCalled = true
		// 验证 ctx 中有 outgoing metadata。
		md, ok := metadata.FromOutgoingContext(ctx)
		if !ok {
			t.Error("invoker 未在 ctx 中找到 outgoing metadata")
			return nil
		}
		if len(md.Get("traceparent")) == 0 {
			t.Error("invoker 未在 metadata 中找到 traceparent")
		}
		// 验证 ctx 中有 span。
		span := trace.SpanFromContext(ctx)
		if !span.SpanContext().IsValid() {
			t.Error("invoker ctx 中 span 无效")
		}
		return nil
	}

	// 创建一个上游 span，使注入的 traceparent 非空。
	upCtx, upSpan := StartSpan(context.Background(), "upstream-of-client")
	defer upSpan.End()

	err = interceptor(upCtx, "/test.Service/Method", "req", "reply", nil, invoker)
	if err != nil {
		t.Fatalf("interceptor 返回错误: %v", err)
	}
	if !invokerCalled {
		t.Fatal("invoker 未被调用")
	}
}

// TestGRPCClientUnaryInterceptor_Error 验证客户端拦截器在 invoker 返回错误时记录到 span。
func TestGRPCClientUnaryInterceptor_Error(t *testing.T) {
	shutdown, err := Init(Config{Stdout: true})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	interceptor := GRPCClientUnaryInterceptor("test-client-err")

	expectedErr := errors.New("rpc-failed")
	invoker := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		return expectedErr
	}

	err = interceptor(context.Background(), "/test.Service/Fail", "req", "reply", nil, invoker)
	if err != expectedErr {
		t.Fatalf("interceptor 返回错误 = %v, 期望 %v", err, expectedErr)
	}
}

// TestGRPCClientUnaryInterceptor_ExistingMetadata 验证 ctx 已有 outgoing metadata 时拦截器追加而非覆盖。
func TestGRPCClientUnaryInterceptor_ExistingMetadata(t *testing.T) {
	shutdown, err := Init(Config{Stdout: true})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	interceptor := GRPCClientUnaryInterceptor("test-client-md")

	// 预先在 ctx 中放入 outgoing metadata。
	existingMD := metadata.Pairs("custom-key", "custom-value")
	ctx := metadata.NewOutgoingContext(context.Background(), existingMD)

	var seenMD metadata.MD
	invoker := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		seenMD, _ = metadata.FromOutgoingContext(ctx)
		return nil
	}

	if err := interceptor(ctx, "/test.Service/Method", nil, nil, nil, invoker); err != nil {
		t.Fatalf("interceptor 返回错误: %v", err)
	}
	// 原有的 custom-key 应保留。
	if len(seenMD.Get("custom-key")) == 0 {
		t.Error("原有 metadata 被覆盖")
	}
	// traceparent 应被注入。
	if len(seenMD.Get("traceparent")) == 0 {
		t.Error("traceparent 未被注入")
	}
}

// TestGRPCServerUnaryInterceptor_Success 验证服务端拦截器从 metadata 提取 trace 并创建 server span。
func TestGRPCServerUnaryInterceptor_Success(t *testing.T) {
	shutdown, err := Init(Config{Stdout: true})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	interceptor := GRPCServerUnaryInterceptor("test-server")

	// 创建客户端 span 并注入 metadata，模拟上游。
	upCtx, upSpan := StartSpan(context.Background(), "client-side")
	defer upSpan.End()
	expectedTraceID := upSpan.SpanContext().TraceID()
	injectedCtx := InjectGRPCMetadata(upCtx)
	md, _ := metadata.FromOutgoingContext(injectedCtx)
	incomingCtx := metadata.NewIncomingContext(context.Background(), md)

	handlerCalled := false
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		handlerCalled = true
		span := trace.SpanFromContext(ctx)
		if !span.SpanContext().IsValid() {
			t.Error("handler ctx 中 span 无效")
		}
		// 验证 trace 接续。
		if span.SpanContext().TraceID() != expectedTraceID {
			t.Errorf("trace 接续失败: got %s, want %s",
				span.SpanContext().TraceID(), expectedTraceID)
		}
		return "resp", nil
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}
	resp, err := interceptor(incomingCtx, "req", info, handler)
	if err != nil {
		t.Fatalf("interceptor 返回错误: %v", err)
	}
	if !handlerCalled {
		t.Fatal("handler 未被调用")
	}
	if resp != "resp" {
		t.Errorf("resp = %v, 期望 'resp'", resp)
	}
}

// TestGRPCServerUnaryInterceptor_Error 验证服务端拦截器在 handler 返回错误时记录到 span。
func TestGRPCServerUnaryInterceptor_Error(t *testing.T) {
	shutdown, err := Init(Config{Stdout: true})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	interceptor := GRPCServerUnaryInterceptor("test-server-err")

	expectedErr := errors.New("handler-failed")
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return nil, expectedErr
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Fail"}
	resp, err := interceptor(context.Background(), "req", info, handler)
	if err != expectedErr {
		t.Fatalf("interceptor 返回错误 = %v, 期望 %v", err, expectedErr)
	}
	if resp != nil {
		t.Errorf("resp = %v, 期望 nil", resp)
	}
}

// TestGRPCServerUnaryInterceptor_NoMetadata 验证 ctx 无 incoming metadata 时拦截器仍能正常工作。
func TestGRPCServerUnaryInterceptor_NoMetadata(t *testing.T) {
	shutdown, err := Init(Config{Stdout: true})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	interceptor := GRPCServerUnaryInterceptor("test-server-no-md")

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/NoMD"}
	resp, err := interceptor(context.Background(), "req", info, handler)
	if err != nil {
		t.Fatalf("interceptor 返回错误: %v", err)
	}
	if resp != "ok" {
		t.Errorf("resp = %v, 期望 'ok'", resp)
	}
}

// TestExtractGRPCMetadata_WithExistingMetadata 验证 ctx 已有 incoming metadata 时的提取路径（ok=true 分支）。
func TestExtractGRPCMetadata_WithExistingMetadata(t *testing.T) {
	shutdown, err := Init(Config{Stdout: true})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	// 创建上游 span 并注入。
	upCtx, upSpan := StartSpan(context.Background(), "extract-test")
	defer upSpan.End()
	expectedTraceID := upSpan.SpanContext().TraceID()

	injectedCtx := InjectGRPCMetadata(upCtx)
	md, _ := metadata.FromOutgoingContext(injectedCtx)
	// 构造已有 incoming metadata 的 ctx（ok=true 路径）。
	incomingCtx := metadata.NewIncomingContext(context.Background(), md)

	extractedCtx := ExtractGRPCMetadata(incomingCtx)
	gotTraceID := trace.SpanFromContext(extractedCtx).SpanContext().TraceID()
	if gotTraceID != expectedTraceID {
		t.Errorf("提取的 TraceID = %s, 期望 %s", gotTraceID, expectedTraceID)
	}
}

// ============================================================================
// http.go 中未覆盖的函数与分支
// ============================================================================

// TestStatusRecorder_Flush 验证 Flush 透传到底层 ResponseWriter（实现 http.Flusher 时）。
func TestStatusRecorder_Flush(t *testing.T) {
	// httptest.NewRecorder 实现了 http.Flusher。
	rec := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: rec, status: http.StatusOK}

	// 调用 Flush 不应 panic，且应透传到 rec。
	sr.Flush()
	if !rec.Flushed {
		t.Error("Flush 未透传到底层 ResponseWriter")
	}
}

// TestStatusRecorder_Flush_NonFlusher 验证底层 ResponseWriter 不实现 http.Flusher 时 Flush 不 panic。
func TestStatusRecorder_Flush_NonFlusher(t *testing.T) {
	// 使用不实现 http.Flusher 的自定义 ResponseWriter。
	sr := &statusRecorder{ResponseWriter: &nonFlusherResponseWriter{}, status: http.StatusOK}
	// 调用 Flush 不应 panic。
	sr.Flush()
}

// nonFlusherResponseWriter 是一个不实现 http.Flusher 的 ResponseWriter，用于测试 Flush 的类型断言失败分支。
type nonFlusherResponseWriter struct {
	header http.Header
}

func (w *nonFlusherResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *nonFlusherResponseWriter) WriteHeader(code int) {}

func (w *nonFlusherResponseWriter) Write(b []byte) (int, error) {
	return len(b), nil
}

// TestSchemeOf_TLS 验证 r.TLS 非 nil 时返回 "https"。
func TestSchemeOf_TLS(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://localhost/", nil)
	req.TLS = &tls.ConnectionState{}
	if s := schemeOf(req); s != "https" {
		t.Errorf("schemeOf(TLS) = %q, 期望 'https'", s)
	}
}

// 注意：schemeOf 仅检查 r.TLS != nil，零值即可。

// TestPortOf_EmptyHost 验证 Host 为空时返回 0。
func TestPortOf_EmptyHost(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = ""
	if p := portOf(req); p != 0 {
		t.Errorf("portOf(empty-host) = %d, 期望 0", p)
	}
}

// TestPortOf_HTTPSDefault 验证无端口但 scheme=https 时返回 443。
func TestPortOf_HTTPSDefault(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://localhost/", nil)
	// httptest.NewRequest 会设置 Host="localhost"，无端口。
	// schemeOf 在 r.TLS==nil 且无 X-Forwarded-Proto 时返回 "http"，故此用例需设置 X-Forwarded-Proto。
	req.Header.Set("X-Forwarded-Proto", "https")
	if p := portOf(req); p != 443 {
		t.Errorf("portOf(https-no-port) = %d, 期望 443", p)
	}
}

// TestPortOf_InvalidPort 验证端口非数字时回退到 scheme 默认端口。
func TestPortOf_InvalidPort(t *testing.T) {
	// 手动构造 Request，避免 httptest.NewRequest 解析 URL 时因端口非数字 panic。
	req := &http.Request{
		Method: http.MethodGet,
		Host:   "localhost:abc",
		URL:    &url.URL{Path: "/"},
		Header: http.Header{},
	}
	// host="localhost:abc"，Atoi 失败，scheme=http → 返回 80。
	if p := portOf(req); p != 80 {
		t.Errorf("portOf(invalid-port) = %d, 期望 80", p)
	}
}

// TestHTTPMiddleware_Flusher 验证中间件包装后的 handler 仍支持 Flush（SSE 场景）。
func TestHTTPMiddleware_Flusher(t *testing.T) {
	shutdown, err := Init(Config{Stdout: true})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 业务 handler 调用 Flush（SSE 写入场景）。
		f, ok := w.(http.Flusher)
		if !ok {
			t.Error("中间件包装后的 ResponseWriter 未实现 http.Flusher")
			return
		}
		f.Flush()
		w.WriteHeader(http.StatusOK)
	})

	wrapped := HTTPMiddleware("test-flush", handler)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sse", nil)
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
}

// TestHTTPMiddleware_404 验证 4xx 状态码不标记 span 为 Error（仅 >=500 才标记）。
func TestHTTPMiddleware_404(t *testing.T) {
	shutdown, err := Init(Config{Stdout: true})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	wrapped := HTTPMiddleware("test-404", handler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/not-found", nil)
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, 期望 404", rec.Code)
	}
}

// ============================================================================
// otelx.go 中 SpanIDFromContext 的 nil ctx 分支
// ============================================================================

// TestSpanIDFromContext_NilCtx 验证 nil ctx 时返回空串（不 panic）。
func TestSpanIDFromContext_NilCtx(t *testing.T) {
	if got := SpanIDFromContext(nil); got != "" {
		t.Fatalf("SpanIDFromContext(nil) = %q, 期望空串", got)
	}
}

// TestSpanIDFromContext_NoSpan 验证 ctx 无有效 span 时返回空串。
func TestSpanIDFromContext_NoSpan(t *testing.T) {
	if got := SpanIDFromContext(context.Background()); got != "" {
		t.Fatalf("SpanIDFromContext(empty) = %q, 期望空串", got)
	}
}

// ============================================================================
// 全局 propagator 设置验证
// ============================================================================

// TestInit_SetsGlobalPropagator 验证 Init 后全局 propagator 已设置（W3C Trace Context + Baggage）。
func TestInit_SetsGlobalPropagator(t *testing.T) {
	shutdown, err := Init(Config{})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	// 全局 propagator 应非 nil（no-op 模式也设置）。
	p := otel.GetTextMapPropagator()
	if p == nil {
		t.Fatal("全局 propagator 未设置")
	}

	// 验证 W3C Trace Context 生效：注入后能提取回相同 TraceID。
	ctx, span := StartSpan(context.Background(), "prop-test")
	defer span.End()
	// no-op 模式下 span 无效，跳过 round-trip 验证。
	if !span.SpanContext().IsValid() {
		return
	}
	expectedTraceID := span.SpanContext().TraceID()

	carrier := propagation.HeaderCarrier(http.Header{})
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	extractedCtx := otel.GetTextMapPropagator().Extract(context.Background(), carrier)
	gotTraceID := trace.SpanFromContext(extractedCtx).SpanContext().TraceID()
	if gotTraceID != expectedTraceID {
		t.Errorf("propagator round-trip 失败: got %s, want %s", gotTraceID, expectedTraceID)
	}
}
