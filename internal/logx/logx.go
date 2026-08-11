// Package logx 提供结构化日志（slog JSON）与 request/gRPC 级别的 traceID 透传（P1-2）。
// 替代散落的 log.Printf，满足：可检索、可关联、可接采集器。仅依赖标准库 log/slog。
//
// M1-4 分布式可观测性：Trace(ctx) 优先从 OTel span context 提取真实 trace_id，
// 使日志与 OTel 链路追踪自动关联；ctx 无有效 span 时回退到 WithTrace 显式注入的 traceID，
// 再回退到空串（向后兼容，不破坏无 OTel 场景）。
package logx

import (
	"context"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace"
)

type ctxKey int

const traceKey ctxKey = iota

// WithTrace 返回携带 traceID 的 context。
// 显式注入的 traceID 作为 fallback，当 ctx 无有效 OTel span 时由 Trace() 返回。
// 业务代码可在无 OTel 场景（如启动阶段、后台任务）用此方法手动关联一个伪 traceID。
func WithTrace(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceKey, traceID)
}

// Trace 从 context 取 traceID（无则空串）。
//
// M1-4 优先级：
//  1. OTel span context 的 TraceID（真实分布式 trace_id，与 Jaeger/OTLP 对齐）；
//  2. WithTrace 显式注入的 traceID（fallback，用于无 OTel 场景的手动关联）；
//  3. 空串（无任何 trace 信息）。
//
// 这样所有调用 logx.Info/Warn/Error 的代码自动关联 OTel trace_id，
// 无需修改调用点；同时保留 WithTrace 的向后兼容（无 OTel 时仍可用）。
func Trace(ctx context.Context) string {
	if ctx != nil {
		// 优先从 OTel span context 提取真实 trace_id。
		if sc := trace.SpanFromContext(ctx).SpanContext(); sc.IsValid() {
			return sc.TraceID().String()
		}
		// 回退到 WithTrace 显式注入的 traceID。
		if v, ok := ctx.Value(traceKey).(string); ok {
			return v
		}
	}
	return ""
}

// logger 默认以 JSON 输出到 stderr（可被采集器 tail / 转发）。
var logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

// Info 结构化信息日志（带 traceID）。
func Info(ctx context.Context, msg string, args ...any) {
	logger.Info(msg, append([]any{"traceID", Trace(ctx)}, args...)...)
}

// Warn 结构化告警日志（带 traceID）。
func Warn(ctx context.Context, msg string, args ...any) {
	logger.Warn(msg, append([]any{"traceID", Trace(ctx)}, args...)...)
}

// Error 结构化错误日志（带 traceID 与 err）。
func Error(ctx context.Context, msg string, err error, args ...any) {
	if err != nil {
		args = append(args, "error", err.Error())
	}
	logger.Error(msg, append([]any{"traceID", Trace(ctx)}, args...)...)
}
