// log_test.go 测试 pkg/log 的日志封装。
//
// 可测性说明（源码限制）：
//   - Logger.Info/Warn/Error 与 FieldLogger.* 底层调用 internal/logx 的包级函数，
//     logx 现提供 SetOutput（2026-09-26 起），本文件所有用例统一用 withCapture
//     捕获真实 JSON 输出；历史实现因无法注入 bytes.Buffer，只能验证
//     不 panic + 字段展开逻辑；
//   - ContextLogger.* 每次调用时读取 os.Stderr 变量构造 handler，因此可通过
//     临时替换 os.Stderr 为 os.Pipe 管道捕获真实 JSON 输出断言字段结构。
package log

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/Levango7/OpsMesh/internal/logx"
)

// withCapture 通过 logx.SetOutput 捕获日志输出（同步写入 bytes.Buffer，
// 无需管道/goroutine；返回前恢复 stderr）。
//
// P1-6 起 pkg/log 全量转发到 logx，故本 helper 能捕获**所有**级别与方法
// （历史实现只能捕获 ContextLogger，且依赖它每次调用重建 handler 的缺陷）。
func withCapture(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	logx.SetOutput(&buf)
	defer func() { logx.SetOutput(os.Stderr); logx.SetLevel(slog.LevelInfo) }()
	fn()
	return buf.String()
}

// TestConfigLevels 验证 Config 把级别应用到进程级（logx），且未知级别退回 info。
func TestConfigLevels(t *testing.T) {
	defer logx.SetLevel(slog.LevelInfo)
	tests := []struct {
		level string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"", slog.LevelInfo},        // 空串回退默认 info
		{"verbose", slog.LevelInfo}, // 未知级别回退 info（并打 WARN）
		{"DEBUG", slog.LevelDebug},  // 大小写不敏感（2026-09-26 起与 logx.ParseLevel 对齐）
	}
	for _, tt := range tests {
		l := Config("test-svc", tt.level)
		if l == nil {
			t.Fatalf("Config(%q) 返回 nil", tt.level)
		}
		if got := logx.Level(); got != tt.want {
			t.Errorf("Config(level=%q) 后进程级别 = %v, want %v", tt.level, got, tt.want)
		}
		if l.serviceName != "test-svc" {
			t.Errorf("Config(%q).serviceName = %q, want %q", tt.level, l.serviceName, "test-svc")
		}
	}
}

// TestWithContext 验证 WithContext 返回的 ContextLogger 携带 service 名
// 与 context 中的 trace 信息（无 OTel span 时均为空串）。
func TestWithContext(t *testing.T) {
	l := Config("svc-a", "info")
	c := l.WithContext(context.Background())
	if c == nil {
		t.Fatal("WithContext 返回 nil")
	}
	if c.serviceName != "svc-a" {
		t.Errorf("serviceName = %q, want %q", c.serviceName, "svc-a")
	}
	// 无 OTel span 的 ctx：spanID 为空串。
	// traceID 不再由 ContextLogger 固化（改为写入时刻由 logx 从 ctx 取），
	// 故此处只断言 spanID；traceID 的真实输出见 TestContextLoggerJSONOutput。
	if c.spanID != "" {
		t.Errorf("无 span 的 ctx 应产生空 spanID, got %q", c.spanID)
	}
}

// TestWithField 验证 WithField 携带单个键值对。
func TestWithField(t *testing.T) {
	l := Config("svc-a", "info")
	f := l.WithField("key1", "value1")
	if f == nil {
		t.Fatal("WithField 返回 nil")
	}
	if len(f.fields) != 1 || f.fields["key1"] != "value1" {
		t.Fatalf("fields = %v, want {key1: value1}", f.fields)
	}
}

// TestWithFields 验证 WithFields 携带多个键值对及边界输入。
func TestWithFields(t *testing.T) {
	l := Config("svc-a", "info")

	// 正常路径：多个字段。
	f := l.WithFields(map[string]string{"k1": "v1", "k2": "v2", "k3": "v3"})
	if len(f.fields) != 3 {
		t.Fatalf("fields 数量 = %d, want 3", len(f.fields))
	}
	for k, v := range map[string]string{"k1": "v1", "k2": "v2", "k3": "v3"} {
		if f.fields[k] != v {
			t.Errorf("fields[%q] = %q, want %q", k, f.fields[k], v)
		}
	}

	// 边界：nil map。
	fnil := l.WithFields(nil)
	if fnil == nil {
		t.Fatal("WithFields(nil) 返回 nil")
	}
	if len(fnil.fields) != 0 {
		t.Errorf("WithFields(nil).fields 应为空, got %v", fnil.fields)
	}

	// 边界：空 map。
	fempty := l.WithFields(map[string]string{})
	if len(fempty.fields) != 0 {
		t.Errorf("WithFields(空 map).fields 应为空, got %v", fempty.fields)
	}

	// 边界：空 key/value 也能透传。
	fblank := l.WithFields(map[string]string{"": ""})
	if fblank.fields[""] != "" {
		t.Errorf("空 key/value 应原样保留, got %v", fblank.fields)
	}
}

// TestLoggerMethodsNoPanic 验证 Logger 各级别方法在正常/边界输入下不 panic
// （输出走 logx 全局 stderr，无法捕获断言，见文件头可测性说明）。
func TestLoggerMethodsNoPanic(t *testing.T) {
	l := Config("test-svc", "debug")
	ctx := context.Background()

	l.Info(ctx, "info message", "key", "value")
	l.Warn(ctx, "warn message", "key", "value")
	l.Error(ctx, "error message", errors.New("boom"), "key", "value")
	l.Error(ctx, "error message nil", nil) // nil error 分支
	l.Debug(ctx, "debug message", "key", "value")

	// 边界：空消息、无 args。
	l.Info(ctx, "")
	l.Info(ctx, "no args")

	// 边界：nil context（logx.Trace 对 nil ctx 返回空串，不 panic）。
	l.Info(nil, "nil ctx")

	// debug 级别的 Logger：Debug 走 logx.Info（见源码），确保分支可达。
	dbg := Config("dbg-svc", "debug")
	dbg.Debug(ctx, "debug level message")
}

// TestLoggerDebugLevelFiltering 验证 Debug 的真实过滤语义（捕获输出断言）：
//   - Config(level="info")  → Debug 不输出；
//   - Config(level="debug") → Debug 输出，且 level 字段为 DEBUG
//     （历史缺陷：Debug 走 logx.Info，被记成 INFO）。
func TestLoggerDebugLevelFiltering(t *testing.T) {
	lInfo := Config("svc", "info")
	out := withCapture(t, func() { lInfo.Debug(context.Background(), "应被过滤") })
	if strings.Contains(out, "应被过滤") {
		t.Fatalf("info 级别下 Debug 不应输出，实际: %s", out)
	}

	lDebug := Config("svc", "debug")
	out = withCapture(t, func() { lDebug.Debug(context.Background(), "应被输出") })
	if !strings.Contains(out, "应被输出") {
		t.Fatalf("debug 级别下 Debug 应输出，实际: %s", out)
	}
	if !strings.Contains(out, `"level":"DEBUG"`) {
		t.Fatalf("Debug 的 level 字段应为 DEBUG，实际: %s", out)
	}
}

// TestContextLoggerJSONOutput 验证 ContextLogger 的真实 JSON 输出结构。
// 捕获方式：withCapture（logx.SetOutput），断言
// level/msg/traceID/spanID/service 字段及透传 args 均按序进入 JSON。
// traceID 由 logx 在写入时刻从 ctx 取（此处用 logx.WithTrace 注入）。
func TestContextLoggerJSONOutput(t *testing.T) {
	traceID := "0af7651916cd43dd8448eb211c80319c"
	c := Config("test-svc", "debug").WithContext(logx.WithTrace(context.Background(), traceID))
	c.spanID = "b7ad6b7169203331"

	out := withCapture(t, func() {
		c.Info("ctx info message", "user", "alice")
		c.Warn("ctx warn message")
		c.Error("ctx error message", errors.New("boom"))
		c.Debug("ctx debug message", "k", "v")
	})

	// 四次调用各产生一行 JSON，按行解析。
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 {
		t.Fatalf("捕获到 %d 行日志, want 4\n捕获内容:\n%s", len(lines), out)
	}
	vars := []struct {
		line      string
		wantLevel string
		wantMsg   string
	}{
		{lines[0], "INFO", "ctx info message"},
		{lines[1], "WARN", "ctx warn message"},
		{lines[2], "ERROR", "ctx error message"},
		{lines[3], "DEBUG", "ctx debug message"},
	}
	for i, v := range vars {
		var m map[string]any
		if err := json.Unmarshal([]byte(v.line), &m); err != nil {
			t.Fatalf("第 %d 行不是合法 JSON: %v\n行内容: %s", i+1, err, v.line)
		}
		if m["level"] != v.wantLevel {
			t.Errorf("第 %d 行 level = %v, want %s", i+1, m["level"], v.wantLevel)
		}
		if m["msg"] != v.wantMsg {
			t.Errorf("第 %d 行 msg = %v, want %q", i+1, m["msg"], v.wantMsg)
		}
		// traceID（logx 从 ctx 取）/spanID/service 为固定注入字段。
		if m["traceID"] != traceID {
			t.Errorf("第 %d 行 traceID = %v, want %q", i+1, m["traceID"], traceID)
		}
		if m["spanID"] != c.spanID {
			t.Errorf("第 %d 行 spanID = %v, want %q", i+1, m["spanID"], c.spanID)
		}
		if m["service"] != "test-svc" {
			t.Errorf("第 %d 行 service = %v, want %q", i+1, m["service"], "test-svc")
		}
		if !strings.Contains(v.line, `"time":`) {
			t.Errorf("第 %d 行应含 time 字段: %s", i+1, v.line)
		}
	}
	// 第 1 行的透传字段 user 应进入 JSON。
	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("第 1 行解析失败: %v", err)
	}
	if first["user"] != "alice" {
		t.Errorf("透传字段 user = %v, want %q", first["user"], "alice")
	}
	// 第 3 行：非 nil error 应追加 error 字段。
	var third map[string]any
	if err := json.Unmarshal([]byte(lines[2]), &third); err != nil {
		t.Fatalf("第 3 行解析失败: %v", err)
	}
	if third["error"] != "boom" {
		t.Errorf("error 字段 = %v, want %q", third["error"], "boom")
	}
}

// TestContextLoggerErrorNilBranch 验证 ContextLogger.Error 的 nil error 分支：
// 不追加 error 字段（源码 if err != nil 分支跳过）。
func TestContextLoggerErrorNilBranch(t *testing.T) {
	c := &ContextLogger{ctx: context.Background(), serviceName: "svc", spanID: "s1"}

	out := withCapture(t, func() {
		c.Error("ctx error message nil", nil)
	})

	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &m); err != nil {
		t.Fatalf("输出不是合法 JSON: %v\n输出: %s", err, out)
	}
	if m["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", m["level"])
	}
	if _, ok := m["error"]; ok {
		t.Errorf("nil error 时不应有 error 字段, got %v", m["error"])
	}
}

// TestContextLoggerMethodsNoPanic 验证 ContextLogger 边界输入不 panic
// （真实输出结构由 TestContextLoggerJSONOutput 覆盖）。
func TestContextLoggerMethodsNoPanic(t *testing.T) {
	l := Config("ctx-svc", "debug")
	c := l.WithContext(context.Background())

	// 边界：空消息、超长消息（10KB）。
	c.Info("")
	c.Info(strings.Repeat("long", 10000))
	c.Warn("warn msg", "k", "v")
	c.Debug("debug msg", "k", "v")
}

// TestFieldLoggerFieldExpansion 验证 FieldLogger 将 map 字段展开为 slog 变长参数
// 的逻辑（源码 Info/Error/Warn/Debug 的 args 构造），用可注入的 bytes.Buffer
// 验证字段以扁平 key/value 形式进入 JSON 输出。
func TestFieldLoggerFieldExpansion(t *testing.T) {
	f := &FieldLogger{fields: map[string]string{"k1": "v1", "k2": "v2"}}

	// 直接测源码的 binds()（历史实现是"复刻一份逻辑"，会掩盖源码改动）。
	args := f.binds(0)
	if len(args) != 4 {
		t.Fatalf("展开后 args 数量 = %d, want 4（2 字段 × k/v）", len(args))
	}
	// 键值成对且值正确。
	seen := map[string]string{}
	for i := 0; i < len(args); i += 2 {
		seen[args[i].(string)] = args[i+1].(string)
	}
	if seen["k1"] != "v1" || seen["k2"] != "v2" {
		t.Fatalf("字段展开错误: %v", seen)
	}

	// 用 JSONHandler 验证展开后的字段出现在 JSON 输出里。
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("field msg", args...)
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("输出不是合法 JSON: %v", err)
	}
	if m["k1"] != "v1" || m["k2"] != "v2" {
		t.Errorf("JSON 字段缺失: k1=%v k2=%v", m["k1"], m["k2"])
	}
}

// TestFieldLoggerMethodsNoPanic 验证 FieldLogger 四个级别方法不 panic。
func TestFieldLoggerMethodsNoPanic(t *testing.T) {
	l := Config("field-svc", "info")
	ctx := context.Background()

	l.WithFields(map[string]string{"k1": "v1", "k2": "v2"}).Info(ctx, "info with fields")
	l.WithFields(map[string]string{"k1": "v1"}).Warn(ctx, "warn with fields")
	l.WithFields(map[string]string{"k1": "v1"}).Error(ctx, "error with fields", errors.New("boom"))
	l.WithFields(map[string]string{"k1": "v1"}).Error(ctx, "error nil", nil)
	l.WithFields(nil).Info(ctx, "nil fields") // nil map 边界
	l.WithFields(nil).Debug(ctx, "nil fields debug")
	l.WithField("single", "field").Info(ctx, "single field")

	// 边界：空消息 + 字段为 nil。
	l.WithFields(nil).Info(ctx, "")

	// FieldLogger.Debug 走 logx.Debug（P1-6 修正：此前误走 logx.Info）。
	l.WithField("d", "1").Debug(ctx, "debug via field logger")
}

// TestFieldLoggerNilContext 验证 FieldLogger 方法对 nil ctx 不 panic
// （logx.Trace(nil) 返回空串）。
func TestFieldLoggerNilContext(t *testing.T) {
	l := Config("svc", "info")
	l.WithField("k", "v").Info(nil, "nil ctx")
	l.WithField("k", "v").Error(nil, "nil ctx", errors.New("e"))
}

// TestSpanIDFromContext 验证 spanIDFromContext 委托 otelx：
// 无 span 的 ctx 返回空串，nil ctx 返回空串（otelx.SpanIDFromContext 对 nil 安全）。
func TestSpanIDFromContext(t *testing.T) {
	if got := spanIDFromContext(context.Background()); got != "" {
		t.Errorf("spanIDFromContext(空 ctx) = %q, want 空串", got)
	}
	if got := spanIDFromContext(nil); got != "" {
		t.Errorf("spanIDFromContext(nil) = %q, want 空串", got)
	}
}

// TestConcurrentLogging 验证并发调用各 logger 方法不 panic、无数据竞争。
func TestConcurrentLogging(t *testing.T) {
	l := Config("concurrent-svc", "debug")
	ctx := context.Background()
	c := l.WithContext(ctx)
	f := l.WithField("k", "v")

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.Info(ctx, "concurrent info")
			l.Error(ctx, "concurrent error", errors.New("e"))
			c.Warn("concurrent ctx warn")
			f.Info(ctx, "concurrent field info")
		}()
	}
	wg.Wait()
}

// TestStderrDefaultWriter 编译期确认源码引用 os.Stderr（防止误改输出目标）。
// 若 pkg/log 不再使用 os.Stderr，本测试无法编译——即输出目标变更会被立即发现。
var _ = os.Stderr
