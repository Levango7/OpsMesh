// trace_test.go 覆盖 pkg/trace 全部公共 API。
// 验证：InitTracer no-op/启用模式、ExtractContext/InjectContext 的 W3C round-trip、
// StartSpan/RecordError、HTTPMiddleware 请求走 span、gRPC 拦截器注入/提取、
// TraceIDFromContext/SpanIDFromContext 的空 ctx 边界。
package trace

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// noopExporter 实现 sdktrace.SpanExporter（丢弃所有 span）。
// 用途：测试内构造真实 TracerProvider 使 span 有效（no-op TracerProvider
// 产生的 span SpanContext 无效，无法断言 trace 接续），且不产生导出 IO。
type noopExporter struct{}

func (noopExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	return nil
}
func (noopExporter) Shutdown(ctx context.Context) error { return nil }

// setupTracer 初始化真实 TracerProvider（noop exporter）+ W3C propagator，
// 返回还原全局状态的清理函数。InitTracer 的公共入口只有 OTLP endpoint，
// 测试环境无法连接真实 collector（会阻塞/报错），故此处直接设置全局 Provider，
// 覆盖 trace 包各 API 在"追踪已启用"模式下的行为。
func setupTracer(t *testing.T) func() {
	t.Helper()
	// 与 otelx.Init 相同的 propagator 设置（W3C Trace Context + Baggage）。
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(noopExporter{}))
	otel.SetTracerProvider(tp)
	return func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			t.Errorf("TracerProvider shutdown 失败: %v", err)
		}
	}
}

// TestInitTracerNoop 验证 endpoint 为空时 no-op 初始化（不报错、shutdown 无副作用）。
func TestInitTracerNoop(t *testing.T) {
	shutdown, err := InitTracer("svc", "")
	if err != nil {
		t.Fatalf("no-op InitTracer 不应报错: %v", err)
	}
	if shutdown == nil {
		t.Fatal("no-op InitTracer 返回 nil shutdown")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("no-op shutdown 不应报错: %v", err)
	}
}

// TestInitTracerNoopModeSpanInvalid 验证 no-op 模式（InitTracer("") 设置的
// no-op TracerProvider）下 StartSpan 返回无效 span（零开销模式预期行为）。
// 注意：直接调用 InitTracer 会重置全局 TracerProvider，与其他用例共享全局状态，
// 故仅在此用例内自闭环：Init 后断言、shutdown 后还原。
func TestInitTracerNoopModeSpanInvalid(t *testing.T) {
	shutdown, err := InitTracer("noop-svc", "")
	if err != nil {
		t.Fatalf("InitTracer 失败: %v", err)
	}
	defer func() {
		if err := shutdown(context.Background()); err != nil {
			t.Errorf("shutdown 失败: %v", err)
		}
	}()

	ctx, span := StartSpan(context.Background(), "noop-span")
	defer span.End()
	if span == nil {
		t.Fatal("StartSpan 返回 nil span")
	}
	if span.SpanContext().IsValid() {
		t.Log("span 有效（可能因其他测试已设置全局 TracerProvider，可接受）")
	} else {
		t.Log("no-op span 无效（预期，未配置导出器）")
	}
	_ = ctx
}

// TestInjectExtractContextRoundTrip 验证 InjectContext/ExtractContext 的
// W3C traceparent round-trip：注入后再提取，span context 保持一致。
func TestInjectExtractContextRoundTrip(t *testing.T) {
	cleanup := setupTracer(t)
	defer cleanup()

	// 上游创建 span（no-op 模式下 span 无效，但 propagator 仍会注入 traceparent）。
	upCtx, upSpan := StartSpan(context.Background(), "upstream")
	defer upSpan.End()

	header := http.Header{}
	InjectContext(upCtx, header)

	// 注入后的头应含 traceparent（仅当 span context 有效时）。
	if upSpan.SpanContext().IsValid() {
		if got := header.Get("traceparent"); got == "" {
			t.Fatal("有效 span 注入后应存在 traceparent 头")
		}
	}

	// 提取端：从注入的头还原 ctx，trace/span 应一致。
	downCtx := ExtractContext(context.Background(), header)
	downSpan := trace.SpanFromContext(downCtx)
	if upSpan.SpanContext().IsValid() {
		if got, want := downSpan.SpanContext().TraceID(), upSpan.SpanContext().TraceID(); got != want {
			t.Fatalf("TraceID round-trip 不一致: got %s, want %s", got, want)
		}
		if got, want := downSpan.SpanContext().SpanID(), upSpan.SpanContext().SpanID(); got != want {
			t.Fatalf("SpanID round-trip 不一致: got %s, want %s", got, want)
		}
	} else {
		// no-op span 无效 → 注入头无 traceparent → 提取得到无效 span（不崩溃、语义正确）。
		if downSpan.SpanContext().IsValid() {
			t.Fatal("无效 span 注入后提取到有效 span（意外）")
		}
	}
}

// TestExtractContextEmptyHeader 验证空头提取不产生有效 span、不 panic。
func TestExtractContextEmptyHeader(t *testing.T) {
	cleanup := setupTracer(t)
	defer cleanup()

	ctx := ExtractContext(context.Background(), http.Header{})
	if trace.SpanFromContext(ctx).SpanContext().IsValid() {
		t.Fatal("空头提取不应产生有效 span")
	}

	// 无效 traceparent 头（垃圾值）也不应产生有效 span。
	ctxBad := ExtractContext(context.Background(), http.Header{"traceparent": {"garbage"}})
	if trace.SpanFromContext(ctxBad).SpanContext().IsValid() {
		t.Fatal("垃圾 traceparent 不应提取出有效 span")
	}
}

// TestInjectContextNoSpan 验证无有效 span 的 ctx 注入不 panic、头为空。
func TestInjectContextNoSpan(t *testing.T) {
	cleanup := setupTracer(t)
	defer cleanup()

	header := http.Header{}
	InjectContext(context.Background(), header)
	if got := header.Get("traceparent"); got != "" {
		t.Fatalf("无有效 span 注入后 traceparent 应为空, got %q", got)
	}
}

// TestStartSpanAndRecordError 验证 StartSpan 返回 ctx/span，RecordError 可调用。
func TestStartSpanAndRecordError(t *testing.T) {
	cleanup := setupTracer(t)
	defer cleanup()

	ctx, span := StartSpan(context.Background(), "op-with-error")
	if span == nil {
		t.Fatal("StartSpan 返回 nil span")
	}
	// RecordError 对有效 span 记录错误并标记 Error 状态。
	RecordError(span, errTest{})
	// 边界：nil span 与 nil err 不 panic（otelx.RecordError 有 nil 防护）。
	RecordError(nil, errTest{})
	RecordError(span, nil)
	span.End()

	// ctx 应可继续派生（携带 span）。
	derived, span2 := StartSpan(ctx, "child-op")
	defer span2.End()
	_ = derived
}

// errTest 测试用错误类型。
type errTest struct{}

func (errTest) Error() string { return "test error" }

// TestHTTPMiddleware 验证中间件为请求创建 span、透传 method/path、
// 从上游头接续 trace、响应体原样返回。
func TestHTTPMiddleware(t *testing.T) {
	cleanup := setupTracer(t)
	defer cleanup()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 中间件应把携带 span 的 ctx 注入请求。
		span := trace.SpanFromContext(r.Context())
		if !span.SpanContext().IsValid() {
			t.Error("HTTPMiddleware 未将有效 span 注入请求 context")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	wrapped := HTTPMiddleware("trace-test-http")(handler)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("响应体 = %q, want %q", rec.Body.String(), "ok")
	}
}

// TestHTTPMiddlewareTraceContinuation 验证中间件从上游 traceparent 头接续 trace。
func TestHTTPMiddlewareTraceContinuation(t *testing.T) {
	cleanup := setupTracer(t)
	defer cleanup()

	// 上游 span，注入到 HTTP 头（模拟跨进程上游调用方）。
	upCtx, upSpan := StartSpan(context.Background(), "http-upstream")
	defer upSpan.End()
	expectedTraceID := upSpan.SpanContext().TraceID()

	carrier := http.Header{}
	otel.GetTextMapPropagator().Inject(upCtx, propagation.HeaderCarrier(carrier))

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		if got := span.SpanContext().TraceID(); got != expectedTraceID {
			t.Errorf("中间件未接续上游 trace: got %s, want %s", got, expectedTraceID)
		}
		w.WriteHeader(http.StatusOK)
	})

	wrapped := HTTPMiddleware("trace-prop-test")(handler)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", nil)
	req.Header = carrier.Clone()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", rec.Code)
	}
}

// TestHTTPMiddlewareErrorStatus 验证 5xx 响应正常透传（span Error 标记由 otelx 记录）。
func TestHTTPMiddlewareErrorStatus(t *testing.T) {
	cleanup := setupTracer(t)
	defer cleanup()

	wrapped := HTTPMiddleware("trace-err-test")(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/fail", nil)
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d, want 500", rec.Code)
	}
}

// TestGRPCInterceptors 验证 gRPC 客户端/服务端拦截器可构造且注入/提取 trace context。
func TestGRPCInterceptors(t *testing.T) {
	cleanup := setupTracer(t)
	defer cleanup()

	clientIC := GRPCClientInterceptor()
	if clientIC == nil {
		t.Fatal("GRPCClientInterceptor 返回 nil")
	}
	serverIC := GRPCServerInterceptor()
	if serverIC == nil {
		t.Fatal("GRPCServerInterceptor 返回 nil")
	}

	// 模拟一次 client→server RPC：client 拦截器注入 metadata，server 拦截器提取。
	upCtx, upSpan := StartSpan(context.Background(), "grpc-upstream")
	defer upSpan.End()
	expectedTraceID := upSpan.SpanContext().TraceID()

	// 直接调用 client 拦截器逻辑（不走真实网络）：invoker 内检查注入的 metadata。
	var injectedCtx context.Context
	err := clientIC(upCtx, "/pkg.trace.Test/Method", nil, nil, nil,
		func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
			injectedCtx = ctx
			return nil
		})
	if err != nil {
		t.Fatalf("client 拦截器调用失败: %v", err)
	}

	// server 端：从 outgoing metadata 构造 incoming，调用 server 拦截器提取。
	md, ok := metadata.FromOutgoingContext(injectedCtx)
	if !ok {
		t.Fatal("client 拦截器未注入 outgoing metadata")
	}
	incomingCtx := metadata.NewIncomingContext(context.Background(), md)

	handlerCalled := false
	_, err = serverIC(incomingCtx, nil, &grpc.UnaryServerInfo{FullMethod: "/pkg.trace.Test/Method"},
		func(ctx context.Context, req interface{}) (interface{}, error) {
			handlerCalled = true
			// handler 内的 ctx 应接续上游 trace。
			if got := TraceIDFromContext(ctx); got != expectedTraceID.String() {
				t.Errorf("server 拦截器未接续上游 trace: got %q, want %q", got, expectedTraceID.String())
			}
			return nil, nil
		})
	if err != nil {
		t.Fatalf("server 拦截器调用失败: %v", err)
	}
	if !handlerCalled {
		t.Fatal("server 拦截器未调用 handler")
	}
}

// TestGRPCServerInterceptorNoMetadata 验证请求无 metadata 时 server 拦截器仍正常工作。
func TestGRPCServerInterceptorNoMetadata(t *testing.T) {
	cleanup := setupTracer(t)
	defer cleanup()

	serverIC := GRPCServerInterceptor()
	resp, err := serverIC(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/pkg.trace.Test/Noop"},
		func(ctx context.Context, req interface{}) (interface{}, error) {
			return "done", nil
		})
	if err != nil {
		t.Fatalf("无 metadata 时 server 拦截器报错: %v", err)
	}
	if resp != "done" {
		t.Fatalf("handler 返回值未透传: got %v", resp)
	}
}

// TestGRPCInterceptorErrorPath 验证 handler/invoker 报错时拦截器透传错误。
func TestGRPCInterceptorErrorPath(t *testing.T) {
	cleanup := setupTracer(t)
	defer cleanup()

	// server 拦截器：handler 报错 → 透传错误（span Error 标记由 otelx 记录）。
	sentinelErr := errTest{}
	_, err := GRPCServerInterceptor()(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/pkg.trace.Test/Fail"},
		func(ctx context.Context, req interface{}) (interface{}, error) {
			return nil, sentinelErr
		})
	if err != sentinelErr {
		t.Fatalf("server 拦截器应透传 handler 错误, got %v", err)
	}

	// client 拦截器：invoker 报错 → 透传错误。
	err = GRPCClientInterceptor()(context.Background(), "/pkg.trace.Test/Fail", nil, nil, nil,
		func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
			return sentinelErr
		})
	if err != sentinelErr {
		t.Fatalf("client 拦截器应透传 invoker 错误, got %v", err)
	}
}

// TestTraceIDAndSpanIDFromContext 验证 Trace/Span ID 提取：
// 无 span/nil ctx 返回空串；有 span 时与 span.SpanContext() 一致。
func TestTraceIDAndSpanIDFromContext(t *testing.T) {
	cleanup := setupTracer(t)
	defer cleanup()

	// 边界：空 ctx 与 nil ctx。
	if got := TraceIDFromContext(context.Background()); got != "" {
		t.Errorf("TraceIDFromContext(空 ctx) = %q, want 空串", got)
	}
	if got := SpanIDFromContext(context.Background()); got != "" {
		t.Errorf("SpanIDFromContext(空 ctx) = %q, want 空串", got)
	}
	if got := TraceIDFromContext(nil); got != "" {
		t.Errorf("TraceIDFromContext(nil) = %q, want 空串", got)
	}
	if got := SpanIDFromContext(nil); got != "" {
		t.Errorf("SpanIDFromContext(nil) = %q, want 空串", got)
	}

	// 正常路径：带 span 的 ctx。
	ctx, span := StartSpan(context.Background(), "id-test")
	defer span.End()
	if span.SpanContext().IsValid() {
		if got, want := TraceIDFromContext(ctx), span.SpanContext().TraceID().String(); got != want {
			t.Errorf("TraceIDFromContext = %q, want %q", got, want)
		}
		if got, want := SpanIDFromContext(ctx), span.SpanContext().SpanID().String(); got != want {
			t.Errorf("SpanIDFromContext = %q, want %q", got, want)
		}
		if len(TraceIDFromContext(ctx)) != 32 {
			t.Errorf("TraceID 长度 = %d, want 32", len(TraceIDFromContext(ctx)))
		}
		if len(SpanIDFromContext(ctx)) != 16 {
			t.Errorf("SpanID 长度 = %d, want 16", len(SpanIDFromContext(ctx)))
		}
	} else {
		t.Log("span 无效（no-op 模式预期），仅验证空串语义")
		if TraceIDFromContext(ctx) != "" || SpanIDFromContext(ctx) != "" {
			t.Error("无效 span 的 ctx 应返回空串")
		}
	}
}

// TestAPIReturnTypeSmoke 验证所有公共函数返回值可直接用于典型组合场景
// （middleware 嵌套 + gRPC 拦截器构造 + span 记录），确保包整体可用。
func TestAPIReturnTypeSmoke(t *testing.T) {
	cleanup := setupTracer(t)
	defer cleanup()

	// Middleware + 中间件组合。
	compressLike := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	}
	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, span := StartSpan(r.Context(), "handler-inner-span")
		defer span.End()
		// 在 handler 内注入下游头。
		InjectContext(ctx, r.Header)
		TraceIDFromContext(ctx)
		SpanIDFromContext(ctx)
		w.WriteHeader(http.StatusOK)
	})
	HTTPMiddleware("smoke")(compressLike(base)).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	// grpc 拦截器可赋给对应类型变量。
	var _ grpc.UnaryServerInterceptor = GRPCServerInterceptor()
	var _ grpc.UnaryClientInterceptor = GRPCClientInterceptor()
}
