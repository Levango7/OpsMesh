// Package log provides structured JSON logging with trace context integration
// for OpsMesh services. It wraps internal/logx to expose a public API that
// automatically associates log entries with OTel trace_id and span_id.
//
// Features:
//   - JSON format output (compatible with log aggregators)
//   - Automatic trace_id/span_id injection from context
//   - Structured field support (WithField/WithFields)
//   - Log level control (debug, info, warn, error)
//   - Logger instance with context binding
//
// 实现约定（P1-6 结构化日志统一）：本包**不再自建 slog handler**，一律转发到
// internal/logx —— 进程只有一份日志实现、一个级别来源、一个输出目标。
// 历史缺陷（2026-09-26 修复）：① ContextLogger.* 每次调用都 slog.New 一个 JSON
// handler（每条日志一次分配，且级别硬编码、忽略配置）；② Logger.Debug 与
// FieldLogger.Debug 走 logx.Info，debug 日志被记成 INFO 级；③ Config 的 level
// 只作用于 Logger.Debug，设了 error 仍然照打 info/warn。
package log

import (
	"context"
	"log/slog"

	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/otelx"
)

// Logger provides structured logging with trace context.
type Logger struct {
	serviceName string
}

// Config returns a Logger for the given service and applies the process log level.
// level can be "debug", "info", "warn", or "error" (empty defaults to "info").
//
// 级别是**进程级**的（由 logx 持有，来源 --log-level / OPSMESH_LOG_LEVEL）：
// 原先每个 Logger 实例各持一份 level，只作用于 Debug 一处，导致「设了 error
// 仍打 info」；统一到 logx 后所有级别都按同一来源过滤。
// 本函数无 error 返回值（保持既有签名），非法级别退回 info 并打一条 WARN。
func Config(serviceName, level string) *Logger {
	lv, err := logx.ParseLevel(level)
	if err != nil {
		logx.SetLevel(slog.LevelInfo)
		logx.Warn(context.Background(), "日志级别非法，已退回 info", "service", serviceName, "level", level)
		return &Logger{serviceName: serviceName}
	}
	logx.SetLevel(lv)
	return &Logger{serviceName: serviceName}
}

// WithContext returns a context-aware logger wrapper that automatically
// includes trace_id and span_id from the OTel span context.
// traceID 不在此处固化：由 logx 在**写日志时**从 ctx 取，保证与实际写入时刻一致。
func (l *Logger) WithContext(ctx context.Context) *ContextLogger {
	return &ContextLogger{
		ctx:         ctx,
		serviceName: l.serviceName,
		spanID:      spanIDFromContext(ctx),
	}
}

// WithField returns a fieldLogger with a single key-value pair.
func (l *Logger) WithField(key, value string) *FieldLogger {
	return &FieldLogger{
		fields: map[string]string{key: value},
	}
}

// WithFields returns a fieldLogger with multiple key-value pairs.
func (l *Logger) WithFields(m map[string]string) *FieldLogger {
	return &FieldLogger{
		fields: m,
	}
}

// Info logs an info-level message.
func (l *Logger) Info(ctx context.Context, msg string, args ...any) {
	logx.Info(ctx, msg, l.withService(args)...)
}

// Error logs an error-level message.
func (l *Logger) Error(ctx context.Context, msg string, err error, args ...any) {
	logx.Error(ctx, msg, err, l.withService(args)...)
}

// Warn logs a warn-level message.
func (l *Logger) Warn(ctx context.Context, msg string, args ...any) {
	logx.Warn(ctx, msg, l.withService(args)...)
}

// Debug logs a debug-level message（仅在 --log-level=debug / OPSMESH_LOG_LEVEL=debug 时输出）。
func (l *Logger) Debug(ctx context.Context, msg string, args ...any) {
	logx.Debug(ctx, msg, l.withService(args)...)
}

// withService 在字段最前插入 service（日志可归属到具体微服务）。
func (l *Logger) withService(args []any) []any {
	return append([]any{"service", l.serviceName}, args...)
}

// ContextLogger wraps a context to provide trace-aware logging.
type ContextLogger struct {
	ctx         context.Context
	serviceName string
	spanID      string
}

// fields 返回附加字段（service + spanID）。
// traceID 由 logx 从 ctx 取，此处不再重复输出（重复键会产生歧义）。
func (c *ContextLogger) fields() []any {
	return []any{"service", c.serviceName, "spanID", c.spanID}
}

// Info logs with trace context.
func (c *ContextLogger) Info(msg string, args ...any) {
	logx.Info(c.ctx, msg, append(c.fields(), args...)...)
}

// Error logs with trace context.
func (c *ContextLogger) Error(msg string, err error, args ...any) {
	logx.Error(c.ctx, msg, err, append(c.fields(), args...)...)
}

// Warn logs with trace context.
func (c *ContextLogger) Warn(msg string, args ...any) {
	logx.Warn(c.ctx, msg, append(c.fields(), args...)...)
}

// Debug logs with trace context.
func (c *ContextLogger) Debug(msg string, args ...any) {
	logx.Debug(c.ctx, msg, append(c.fields(), args...)...)
}

// FieldLogger provides structured field logging.
type FieldLogger struct {
	fields map[string]string
}

// binds 返回字段参数（预分配，避免 append 反复扩容）。
func (f *FieldLogger) binds(extra int) []any {
	args := make([]any, 0, len(f.fields)*2+extra)
	for k, v := range f.fields {
		args = append(args, k, v)
	}
	return args
}

// Info logs with structured fields.
func (f *FieldLogger) Info(ctx context.Context, msg string) {
	logx.Info(ctx, msg, f.binds(0)...)
}

// Error logs with structured fields.
func (f *FieldLogger) Error(ctx context.Context, msg string, err error) {
	logx.Error(ctx, msg, err, f.binds(0)...)
}

// Warn logs with structured fields.
func (f *FieldLogger) Warn(ctx context.Context, msg string) {
	logx.Warn(ctx, msg, f.binds(0)...)
}

// Debug logs with structured fields（级别语义同 Logger.Debug）。
func (f *FieldLogger) Debug(ctx context.Context, msg string) {
	logx.Debug(ctx, msg, f.binds(0)...)
}

// spanIDFromContext extracts span ID from context for logging.
func spanIDFromContext(ctx context.Context) string {
	return otelx.SpanIDFromContext(ctx)
}
