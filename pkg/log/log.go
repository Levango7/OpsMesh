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
package log

import (
	"context"
	"log/slog"
	"os"

	"opsmesh/internal/logx"
	"opsmesh/internal/otelx"
)

// Logger provides structured logging with trace context.
type Logger struct {
	serviceName string
	level       slog.Level
}

// Config creates a new Logger configuration with the given service name and level.
// level can be "debug", "info", "warn", or "error". Defaults to "info".
func Config(serviceName, level string) *Logger {
	lvl := slog.LevelInfo
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	}
	return &Logger{
		serviceName: serviceName,
		level:       lvl,
	}
}

// WithContext returns a context-aware logger wrapper that automatically
// includes trace_id and span_id from the OTel span context.
func (l *Logger) WithContext(ctx context.Context) *ContextLogger {
	return &ContextLogger{
		ctx:         ctx,
		serviceName: l.serviceName,
		traceID:     logx.Trace(ctx),
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
	logx.Info(ctx, msg, args...)
}

// Error logs an error-level message.
func (l *Logger) Error(ctx context.Context, msg string, err error, args ...any) {
	logx.Error(ctx, msg, err, args...)
}

// Warn logs a warn-level message.
func (l *Logger) Warn(ctx context.Context, msg string, args ...any) {
	logx.Warn(ctx, msg, args...)
}

// Debug logs a debug-level message.
func (l *Logger) Debug(ctx context.Context, msg string, args ...any) {
	if l.level <= slog.LevelDebug {
		logx.Info(ctx, msg, args...)
	}
}

// ContextLogger wraps a context to provide trace-aware logging.
type ContextLogger struct {
	ctx         context.Context
	serviceName string
	traceID     string
	spanID      string
}

// Info logs with trace context.
func (c *ContextLogger) Info(msg string, args ...any) {
	slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})).
		Info(msg, append([]any{"traceID", c.traceID, "spanID", c.spanID, "service", c.serviceName}, args...)...)
}

// Error logs with trace context.
func (c *ContextLogger) Error(msg string, err error, args ...any) {
	if err != nil {
		args = append(args, "error", err.Error())
	}
	slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})).
		Error(msg, append([]any{"traceID", c.traceID, "spanID", c.spanID, "service", c.serviceName}, args...)...)
}

// Warn logs with trace context.
func (c *ContextLogger) Warn(msg string, args ...any) {
	slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})).
		Warn(msg, append([]any{"traceID", c.traceID, "spanID", c.spanID, "service", c.serviceName}, args...)...)
}

// Debug logs with trace context.
func (c *ContextLogger) Debug(msg string, args ...any) {
	slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})).
		Debug(msg, append([]any{"traceID", c.traceID, "spanID", c.spanID, "service", c.serviceName}, args...)...)
}

// FieldLogger provides structured field logging.
type FieldLogger struct {
	fields map[string]string
}

// Info logs with structured fields.
func (f *FieldLogger) Info(ctx context.Context, msg string) {
	args := make([]any, 0, len(f.fields)*2)
	for k, v := range f.fields {
		args = append(args, k, v)
	}
	logx.Info(ctx, msg, args...)
}

// Error logs with structured fields.
func (f *FieldLogger) Error(ctx context.Context, msg string, err error) {
	args := make([]any, 0, len(f.fields)*2+2)
	for k, v := range f.fields {
		args = append(args, k, v)
	}
	logx.Error(ctx, msg, err, args...)
}

// Warn logs with structured fields.
func (f *FieldLogger) Warn(ctx context.Context, msg string) {
	args := make([]any, 0, len(f.fields)*2)
	for k, v := range f.fields {
		args = append(args, k, v)
	}
	logx.Warn(ctx, msg, args...)
}

// Debug logs with structured fields.
func (f *FieldLogger) Debug(ctx context.Context, msg string) {
	args := make([]any, 0, len(f.fields)*2)
	for k, v := range f.fields {
		args = append(args, k, v)
	}
	logx.Info(ctx, msg, args...)
}

// spanIDFromContext extracts span ID from context for logging.
func spanIDFromContext(ctx context.Context) string {
	return otelx.SpanIDFromContext(ctx)
}
