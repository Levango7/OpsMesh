// Package logx 提供结构化日志（slog JSON）与 request/gRPC 级别的 traceID 透传。
// 替代散落的 log.Printf，满足：可检索、可关联、可接采集器。仅依赖标准库 log/slog。
//
// 分布式可观测性：Trace(ctx) 优先从 OTel span context 提取真实 trace_id，
// 使日志与 OTel 链路追踪自动关联；ctx 无有效 span 时回退到 WithTrace 显式注入的 traceID，
// 再回退到空串（向后兼容，不破坏无 OTel 场景）。
package logx

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"

	"go.opentelemetry.io/otel/trace"
)

// swapWriter 是可原子替换目标的转发 Writer（默认 os.Stderr）。
//
// 为什么不在 SetOutput 里直接重建 slog.Logger：运行期替换 logger 变量会与
// 并发写日志构成数据竞争（-race 可复现）。转发 Writer 让「替换目标」与
// 「写日志」都只经过原子操作，handler 无需重建。
type swapWriter struct{ p atomic.Pointer[io.Writer] }

func (s *swapWriter) Write(b []byte) (int, error) {
	w := s.p.Load()
	if w == nil {
		return len(b), nil // 无目标时静默丢弃，绝不因日志阻塞业务
	}
	return (*w).Write(b)
}

var out swapWriter

func init() {
	var w io.Writer = os.Stderr
	out.p.Store(&w)
}

// SetOutput 重定向日志输出（默认 os.Stderr）。并发安全。
// 用途：测试捕获输出；进程启动期把日志改写到文件或采集管道。
func SetOutput(w io.Writer) { out.p.Store(&w) }

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
// 优先级：
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

// level 持有进程日志级别（P1-6 可支撑性）。
//
// 用 slog.LevelVar 而非普通变量：它自带并发安全（Set/Level 内部同步），
// 可在运行期调整而无需重启；若换成裸变量，改级别就与并发写日志构成数据竞争
// （-race 可复现）。零值即 LevelInfo，与历史行为一致。
var level = new(slog.LevelVar)

// logger 以 JSON 输出到 out（默认 stderr，可被采集器 tail / 转发）。
var logger = slog.New(slog.NewJSONHandler(&out, &slog.HandlerOptions{Level: level}))

// ParseLevel 解析级别名（大小写不敏感，空串视为 info）。
//
// 非法值返回错误而不是静默退回默认：静默退回会让「我明明设了 debug，为什么
// 没有 debug 日志」变成一次现场排障（配置类错误必须响亮）。由启动期调用方
// fail-fast。
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return slog.LevelInfo, fmt.Errorf("非法日志级别 %q（可选：debug|info|warn|error）", s)
}

// SetLevel 设置进程日志级别（并发安全，可在运行期调用）。
func SetLevel(l slog.Level) { level.Set(l) }

// Level 返回当前生效的日志级别。
func Level() slog.Level { return level.Level() }

// Debug 结构化调试日志（带 traceID）。默认级别为 info，故默认不输出；
// 用 --log-level=debug / OPSMESH_LOG_LEVEL=debug 开启。
func Debug(ctx context.Context, msg string, args ...any) {
	logger.Debug(msg, append([]any{"traceID", Trace(ctx)}, args...)...)
}

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
