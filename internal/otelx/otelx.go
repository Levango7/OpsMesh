// Package otelx 封装 OpenTelemetry SDK 初始化与 helper，提供 gRPC + HTTP 自动埋点能力。
// M1-1 链路追踪集成：支持导出到 OTLP gRPC（Jaeger/OTLP collector）与 stdout（调试用）。
// endpoint 为空且 stdout=false 时 no-op（不启用追踪，零开销），保证 OTel 可选不破坏现有功能。
//
// 设计要点：
//   - 全局 propagator 统一为 W3C Trace Context + Baggage，使 HTTP/gRPC 跨进程 trace context 提取/注入一致。
//   - TracerProvider 用 BatchSpanProcessor（5s 批量上报），Shutdown 时 flush 残留 span。
//   - no-op 模式仍设置全局 propagator，使 context 提取/注入行为一致（不丢上游 traceparent）。
package otelx

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
)

// Config OTel 初始化配置。
type Config struct {
	Endpoint    string // OTLP gRPC 导出地址（如 "localhost:4317"）；空=不启用 OTLP
	ServiceName string // 服务名（如 "opsmesh-controlplane"）；空=回退 "opsmesh"
	Stdout      bool   // 是否导出到 stdout（调试用）；与 Endpoint 互斥，Stdout 优先
}

// ShutdownFunc 由 Init 返回，调用方在退出时调用以优雅关闭 TracerProvider（flush 残留 span）。
// 不调用会导致未上报的 span 丢失（BatchSpanProcessor 异步批量上报）。
type ShutdownFunc func(ctx context.Context) error

// noopShutdown 是 no-op 模式下的空关闭函数。
func noopShutdown(context.Context) error { return nil }

// Init 根据配置初始化 OTel SDK：构造 TracerProvider + 导出器 + 全局 propagator。
//   - endpoint 为空且 stdout=false：返回 no-op TracerProvider（不启用追踪，零开销）。
//   - stdout=true：导出到 stderr（调试用，PrettyPrint）。
//   - endpoint 非空：导出到 OTLP gRPC（Jaeger/OTLP collector）。
//
// 返回的 ShutdownFunc 必须在退出时调用以 flush 残留 span。
// 即使 no-op 模式也设置全局 propagator（W3C），使 context 提取/注入行为一致。
func Init(cfg Config) (ShutdownFunc, error) {
	// 统一设置全局 propagator（W3C Trace Context + Baggage），无论是否启用追踪。
	// 这样 no-op 模式下也能正确提取/注入 traceparent（虽不导出 span，但 context 透传不断裂）。
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// 未启用：no-op，返回空 shutdown。
	if cfg.Endpoint == "" && !cfg.Stdout {
		return noopShutdown, nil
	}

	serviceName := cfg.ServiceName
	if serviceName == "" {
		serviceName = "opsmesh"
	}

	// 构造 Resource（服务名 + 版本），附加到每个 span 上。
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String("unknown"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("otelx: 构造 resource 失败: %w", err)
	}

	// 构造导出器：stdout 优先（调试用），其次 OTLP gRPC。
	var exporter sdktrace.SpanExporter
	if cfg.Stdout {
		exporter, err = stdouttrace.New(
			stdouttrace.WithWriter(os.Stderr),
			stdouttrace.WithPrettyPrint(),
		)
		if err != nil {
			return nil, fmt.Errorf("otelx: 构造 stdout exporter 失败: %w", err)
		}
	} else {
		// OTLP gRPC 导出。endpoint 形如 "host:port"。
		opts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(cfg.Endpoint),
		}
		// TLS 策略：端口 443 视为标准 TLS 端口（用系统 TLS），其余用 insecure（内网/调试）。
		// 生产环境如需 TLS，应扩展 Config 携带 TLS 凭据（WithTLSCredentials）。
		if !strings.HasSuffix(cfg.Endpoint, ":443") {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		exporter, err = otlptracegrpc.New(context.Background(), opts...)
		if err != nil {
			return nil, fmt.Errorf("otelx: 构造 OTLP gRPC exporter 失败: %w", err)
		}
	}

	// 构造 TracerProvider：BatchSpanProcessor（5s 批量上报）+ Resource。
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(5*time.Second)),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	return func(ctx context.Context) error {
		return tp.Shutdown(ctx)
	}, nil
}

// Tracer 返回名为 name 的 Tracer（全局 TracerProvider 的句柄）。
// name 通常为调用方包名或模块名（如 "opsmesh/controlplane"）。
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

// StartSpan 是创建 span 的 helper，封装 otel.Tracer("opsmesh").Start。
// 返回的 span 必须在结束时调用 span.End()（通常 defer span.End()）。
//
// 用法：
//
//	ctx, span := otelx.StartSpan(ctx, "heartbeat")
//	defer span.End()
//	// ... 业务逻辑 ...
func StartSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	return otel.Tracer("opsmesh").Start(ctx, name)
}

// SpanFromContext 从 context 提取当前 span（无则返回 no-op span，安全调用）。
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// RecordError 在 span 上记录错误（封装 trace.Span.RecordError，便于调用方不直接依赖 trace 包）。
func RecordError(span trace.Span, err error) {
	if span != nil && err != nil {
		span.RecordError(err)
	}
}

// Enabled 返回 OTel 追踪是否启用（TracerProvider 非 no-op）。
// 通过检查全局 TracerProvider 是否为 *sdktrace.TracerProvider 实例判断。
// no-op 模式下返回 false，调用方可据此跳过不必要的 span 创建开销。
func Enabled() bool {
	_, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider)
	return ok
}

// TraceIDFromContext 从 ctx 提取当前 span 的 trace_id（32 字符 hex 串）。
// M1-4 分布式可观测性：供日志/SSE 事件/审计日志关联 trace_id 使用。
//
// 行为：
//   - ctx 无 span 或 span 无效（no-op 模式 / 未启用追踪）：返回空串。
//   - ctx 有有效 span：返回 span.SpanContext().TraceID().String()（32 字符 hex）。
//
// 空串语义：调用方应将空串视为"无 trace_id"，正常工作不阻断业务（向后兼容）。
// 该函数零开销：仅从 ctx 取 span + 读 SpanContext，不创建 span、不分配。
func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	sc := trace.SpanFromContext(ctx).SpanContext()
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}

// SpanIDFromContext 从 ctx 提取当前 span 的 span_id（16 字符 hex 串）。
// 与 TraceIDFromContext 配合使用，供日志输出精细定位单个 span。
// ctx 无有效 span 时返回空串。
func SpanIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	sc := trace.SpanFromContext(ctx).SpanContext()
	if !sc.IsValid() {
		return ""
	}
	return sc.SpanID().String()
}
