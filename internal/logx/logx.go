// Package logx 提供结构化日志（slog JSON）与 request/gRPC 级别的 traceID 透传（P1-2）。
// 替代散落的 log.Printf，满足：可检索、可关联、可接采集器。仅依赖标准库 log/slog。
package logx

import (
	"context"
	"log/slog"
	"os"
)

type ctxKey int

const traceKey ctxKey = iota

// WithTrace 返回携带 traceID 的 context。
func WithTrace(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceKey, traceID)
}

// Trace 从 context 取 traceID（无则空串）。
func Trace(ctx context.Context) string {
	if v, ok := ctx.Value(traceKey).(string); ok {
		return v
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
