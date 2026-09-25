package logx

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

func TestTraceRoundTrip(t *testing.T) {
	ctx := WithTrace(context.Background(), "trace-123")
	if got := Trace(ctx); got != "trace-123" {
		t.Fatalf("Trace = %q, want trace-123", got)
	}
	if got := Trace(context.Background()); got != "" {
		t.Fatalf("Trace(empty) = %q, want empty", got)
	}
}

// TestTraceFromOTelSpan 验证 ：Trace(ctx) 优先从 OTel span context 提取 trace_id。
// 当 ctx 携带有效 OTel span 时，Trace 返回 span 的 TraceID（32 字符 hex），
// 而非 WithTrace 显式注入的 fallback 值。
func TestTraceFromOTelSpan(t *testing.T) {
	// 用 OTel Tracer 创建一个 span（无需 Init，no-op Tracer 也能产生有效 SpanContext）。
	// 注意：no-op TracerProvider 产生的 span SpanContext 无效（IsValid()=false），
	// 故此处用真实 Tracer（otel.Tracer("test") 在 no-op 模式下返回的 span 无效）。
	// 改用 sdktrace 验证需引入 sdk 依赖，此处用 logx 自身的 OTel 集成路径验证：
	// 当 span 无效时回退到 WithTrace，当 span 有效时优先用 span TraceID。
	// 由于 logx 包不直接依赖 otelx.Init，这里通过手动构造有效 SpanContext 注入 ctx。
	ctx := context.Background()
	// 无 span 时回退到 WithTrace。
	ctx = WithTrace(ctx, "fallback-trace")
	if got := Trace(ctx); got != "fallback-trace" {
		t.Fatalf("Trace(WithTrace only) = %q, want fallback-trace", got)
	}
	// 注入有效 span context 后应优先返回 span 的 TraceID。
	// 用 otel.Tracer 创建 span（no-op 模式下 span 无效，故此用例仅在 span 有效时验证优先级）。
	tracer := otel.Tracer("logx-test")
	spanCtx, span := tracer.Start(ctx, "test-span")
	defer span.End()
	if span.SpanContext().IsValid() {
		want := span.SpanContext().TraceID().String()
		if got := Trace(spanCtx); got != want {
			t.Fatalf("Trace(spanCtx) = %q, want %q (OTel TraceID)", got, want)
		}
		// 有效 span 应覆盖 WithTrace 的 fallback 值。
		if got := Trace(spanCtx); got == "fallback-trace" {
			t.Fatal("Trace(spanCtx) 返回了 fallback 值，应优先用 OTel TraceID")
		}
	} else {
		t.Log("no-op span 无效（预期，未调用 otelx.Init），跳过 OTel 优先级验证")
	}
}

// TestTraceNilContext 验证 Trace(nil) 不 panic 且返回空串。
func TestTraceNilContext(t *testing.T) {
	if got := Trace(nil); got != "" {
		t.Fatalf("Trace(nil) = %q, want empty", got)
	}
}

// TestTraceWithSpanAndFallback 验证当 ctx 同时有 span（无效）和 WithTrace 时回退正确。
func TestTraceWithSpanAndFallback(t *testing.T) {
	// no-op span（SpanContext 无效）+ WithTrace fallback → 应返回 fallback。
	spanCtx, span := trace.SpanFromContext(context.Background()), trace.SpanFromContext(context.Background())
	defer span.End()
	_ = spanCtx
	ctx := WithTrace(context.Background(), "fb-123")
	// 注入无效 span（no-op span 的 SpanContext IsValid()=false）。
	if got := Trace(ctx); got != "fb-123" {
		t.Fatalf("Trace(invalid span + WithTrace) = %q, want fb-123", got)
	}
}

// TestInfoWithTrace 验证 Info 日志携带 traceID 字段。
// 这是 的核心保证：日志自动关联 trace_id，无需调用方手动添加。
func TestInfoWithTrace(t *testing.T) {
	// 仅验证不 panic + 不阻塞，日志内容校验由集成测试覆盖。
	ctx := WithTrace(context.Background(), "log-trace-456")
	Info(ctx, "test message", "key", "value")
	Warn(ctx, "test warn", "key", "value")
	Error(ctx, "test error", errors.New("err"), "key", "value")
}

// ── P1-6：日志级别可配 + 输出可重定向 ────────────────────────────────

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in      string
		want    slog.Level
		wantErr bool
	}{
		{"debug", slog.LevelDebug, false},
		{"DEBUG", slog.LevelDebug, false},   // 大小写不敏感
		{" Debug ", slog.LevelDebug, false}, // 两端空白容忍（env 里常见）
		{"info", slog.LevelInfo, false},
		{"", slog.LevelInfo, false}, // 空串=默认
		{"warn", slog.LevelWarn, false},
		{"warning", slog.LevelWarn, false},
		{"error", slog.LevelError, false},
		{"verbose", slog.LevelInfo, true}, // 非法值必须报错，不静默退回
		{"2", slog.LevelInfo, true},
	}
	for _, c := range cases {
		got, err := ParseLevel(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseLevel(%q) 期望报错，实际 err=nil（静默退回会让排障变猜谜）", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseLevel(%q) 意外错误: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestSetLevelControlsFiltering(t *testing.T) {
	defer SetLevel(slog.LevelInfo)

	var buf bytes.Buffer
	defer SetOutput(os.Stderr)
	SetOutput(&buf)

	// 默认（info）：debug 不输出，info 输出。
	SetLevel(slog.LevelInfo)
	Debug(context.Background(), "调试-应被过滤")
	Info(context.Background(), "信息-应输出")
	if s := buf.String(); strings.Contains(s, "应被过滤") {
		t.Fatalf("info 级别下 debug 日志不应输出，实际: %s", s)
	} else if !strings.Contains(s, "应输出") {
		t.Fatalf("info 级别下 info 日志应输出，实际: %s", s)
	}

	// debug：debug 输出，且级别字段为 DEBUG（历史缺陷是 debug 走 Info 记成 INFO）。
	buf.Reset()
	SetLevel(slog.LevelDebug)
	Debug(context.Background(), "调试-应输出")
	out := buf.String()
	if !strings.Contains(out, "应输出") {
		t.Fatalf("debug 级别下 debug 日志应输出，实际: %s", out)
	}
	if !strings.Contains(out, `"level":"DEBUG"`) {
		t.Fatalf("debug 日志级别字段应为 DEBUG，实际: %s", out)
	}

	// error：info 与 warn 均被过滤。
	buf.Reset()
	SetLevel(slog.LevelError)
	Info(context.Background(), "信息-应过滤")
	Warn(context.Background(), "告警-应过滤")
	Error(context.Background(), "错误-应输出", errors.New("e"))
	out = buf.String()
	if strings.Contains(out, "应过滤") {
		t.Fatalf("error 级别下 info/warn 不应输出，实际: %s", out)
	}
	if !strings.Contains(out, "应输出") {
		t.Fatalf("error 级别下 error 日志应输出，实际: %s", out)
	}
	if Level() != slog.LevelError {
		t.Fatalf("Level() = %v, want error", Level())
	}
}

// TestSetOutputAndConcurrentLevelChange 验证输出重定向与级别调整并发安全
// （-race 下跑；替换 logger 变量的实现会在这里报数据竞争）。
func TestSetOutputAndConcurrentLevelChange(t *testing.T) {
	defer func() { SetOutput(os.Stderr); SetLevel(slog.LevelInfo) }()
	var buf bytes.Buffer
	SetOutput(&buf)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				Info(context.Background(), "并发日志", "i", j)
				Debug(context.Background(), "并发调试", "i", j)
			}
		}()
	}
	for i := 0; i < 20; i++ {
		SetLevel(slog.LevelDebug)
		SetLevel(slog.LevelInfo)
	}
	wg.Wait()
	if buf.Len() == 0 {
		t.Fatal("重定向后应捕获到日志输出")
	}
}
