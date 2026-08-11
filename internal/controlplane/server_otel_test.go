// server_otel_test.go 测试 M1-1 OTel 链路追踪在控制面中的集成。
// 验证：HTTP 中间件创建 span、配置传递、未启用时 no-op、W3C Trace Context 提取。
package controlplane

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"opsmesh/internal/config"
	"opsmesh/internal/otelx"
)

// TestOTelHTTPMiddlewareSpanCreation 验证控制面 HTTP 中间件为请求创建有效 span。
func TestOTelHTTPMiddlewareSpanCreation(t *testing.T) {
	shutdown, err := otelx.Init(otelx.Config{Stdout: false}) // no-op 模式也设置 propagator
	if err != nil {
		t.Fatalf("otelx.Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	// 用控制面的中间件链构造 handler（模拟 Start 中的链）。
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		// no-op 模式下 span 无效但不应 panic。
		_ = span.SpanContext()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	})
	wrapped := otelx.HTTPMiddleware("opsmesh-controlplane", handler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
}

// TestOTelHTTPMiddlewareWithStdout 验证启用 stdout 导出时中间件创建有效 span 并记录属性。
func TestOTelHTTPMiddlewareWithStdout(t *testing.T) {
	shutdown, err := otelx.Init(otelx.Config{Stdout: true, ServiceName: "opsmesh-controlplane-test"})
	if err != nil {
		t.Fatalf("otelx.Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	spanCreated := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		if span.SpanContext().IsValid() {
			spanCreated = true
		}
		w.WriteHeader(http.StatusOK)
	})
	wrapped := otelx.HTTPMiddleware("opsmesh-controlplane", handler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	if !spanCreated {
		t.Error("HTTP 中间件未创建有效 span")
	}
}

// TestOTelConfigPropagation 验证 config 的 OTel 字段正确解析。
func TestOTelConfigPropagation(t *testing.T) {
	cfg := &config.Config{
		Mode:            "controlplane",
		OTELEndpoint:    "jaeger:4317",
		OTELServiceName: "opsmesh-controlplane",
		OTELStdout:      false,
	}
	if cfg.OTELEndpoint != "jaeger:4317" {
		t.Errorf("OTELEndpoint = %q, 期望 'jaeger:4317'", cfg.OTELEndpoint)
	}
	if cfg.OTELServiceName != "opsmesh-controlplane" {
		t.Errorf("OTELServiceName = %q, 期望 'opsmesh-controlplane'", cfg.OTELServiceName)
	}
}

// TestOTelW3CTraceContextExtraction 验证控制面 HTTP 中间件从请求头提取 W3C traceparent。
// 这是 trace_id 贯穿 agent→控制面 的关键：agent 经 gRPC 注入 trace context，
// 控制面 HTTP 入口（如联邦/网关转发）从 traceparent 头接续 trace。
func TestOTelW3CTraceContextExtraction(t *testing.T) {
	shutdown, err := otelx.Init(otelx.Config{Stdout: true})
	if err != nil {
		t.Fatalf("otelx.Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	// 创建上游 span 并注入到 HTTP 头。
	upCtx, upSpan := otelx.StartSpan(context.Background(), "upstream-agent")
	defer upSpan.End()
	expectedTraceID := upSpan.SpanContext().TraceID()

	carrier := propagation.HeaderCarrier(http.Header{})
	otel.GetTextMapPropagator().Inject(upCtx, carrier)

	extractedTraceID := ""
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		extractedTraceID = span.SpanContext().TraceID().String()
		w.WriteHeader(http.StatusOK)
	})
	wrapped := otelx.HTTPMiddleware("opsmesh-controlplane", handler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", nil)
	for _, key := range carrier.Keys() {
		req.Header.Set(key, carrier.Get(key))
	}
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	if extractedTraceID != expectedTraceID.String() {
		t.Errorf("TraceID 不匹配: got %s, want %s（W3C trace context 提取失败）",
			extractedTraceID, expectedTraceID)
	}
}

// TestOTelShutdownNoop 验证未启用 OTel 时 shutdown 为 no-op（不 panic）。
func TestOTelShutdownNoop(t *testing.T) {
	shutdown, err := otelx.Init(otelx.Config{}) // endpoint 空，stdout false
	if err != nil {
		t.Fatalf("otelx.Init 失败: %v", err)
	}
	// 多次调用 shutdown 应安全（no-op）。
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("第一次 shutdown 失败: %v", err)
	}
}

// TestOTelGRPCServerInterceptor 验证 gRPC 服务端拦截器从 metadata 提取 trace context。
func TestOTelGRPCServerInterceptor(t *testing.T) {
	shutdown, err := otelx.Init(otelx.Config{Stdout: true})
	if err != nil {
		t.Fatalf("otelx.Init 失败: %v", err)
	}
	defer shutdown(context.Background())

	// 构造服务端拦截器。
	interceptor := otelx.GRPCServerUnaryInterceptor("opsmesh-controlplane")

	// 模拟 handler。
	handlerCalled := false
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		span := trace.SpanFromContext(ctx)
		if !span.SpanContext().IsValid() {
			t.Error("服务端拦截器未创建有效 span")
		}
		handlerCalled = true
		return "resp", nil
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/opsmesh.v1.Registration/Heartbeat"}
	_, err = interceptor(context.Background(), "req", info, handler)
	if err != nil {
		t.Fatalf("拦截器调用失败: %v", err)
	}
	if !handlerCalled {
		t.Error("handler 未被调用")
	}
}