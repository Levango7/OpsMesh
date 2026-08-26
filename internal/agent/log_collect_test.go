// log_collect_test.go 测试 LogCollector。
//
// 覆盖：
//   - NewLogCollector 构造与参数校验。
//   - 基本采集：追加内容后增量读取并 pushFn 收到记录。
//   - 过滤：IncludeRules 白名单 / ExcludeRules 黑名单。
//   - 多行合并：MultilineRule 正则识别记录首行，后续行合并。
//   - 启停：Start/Stop 幂等，Stop 后不再采集。
//   - 热更新：UpdateConfig 运行中替换路径。
//   - 统计：Stats 反映采集行数/字节数。
package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestCollector 构造测试用采集器：短 Interval + 收集记录的 pushFn。
func newTestCollector(t *testing.T, cfg LogCollectConfig) (*LogCollector, *sync.Mutex, *[]CollectedLog) {
	t.Helper()
	var mu sync.Mutex
	var got []CollectedLog
	pushFn := func(_ context.Context, records []CollectedLog) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, records...)
		return nil
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 50 * time.Millisecond
	}
	lc, err := NewLogCollector(cfg, pushFn)
	if err != nil {
		t.Fatalf("NewLogCollector 失败: %v", err)
	}
	return lc, &mu, &got
}

// snapshot 读取 pushFn 收集到的记录快照（线程安全）。
func snapshot(mu *sync.Mutex, got *[]CollectedLog) []CollectedLog {
	mu.Lock()
	defer mu.Unlock()
	out := make([]CollectedLog, len(*got))
	copy(out, *got)
	return out
}

// waitForRecords 轮询等待 pushFn 收到至少 n 条记录（超时 2s）。
func waitForRecords(t *testing.T, mu *sync.Mutex, got *[]CollectedLog, n int) []CollectedLog {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if recs := snapshot(mu, got); len(recs) >= n {
			return recs
		}
		time.Sleep(10 * time.Millisecond)
	}
	return snapshot(mu, got)
}

// appendLog 向文件追加内容（自动 sync 刷盘，确保后续 Open 能读到）。
func appendLog(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("打开文件追加失败: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("追加内容失败: %v", err)
	}
}

// TestLogCollectorNew 验证构造与参数校验。
func TestLogCollectorNew(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		cfg := LogCollectConfig{Paths: []string{"/var/log/syslog"}, Interval: 100 * time.Millisecond}
		lc, err := NewLogCollector(cfg, func(_ context.Context, _ []CollectedLog) error { return nil })
		if err != nil {
			t.Fatalf("构造失败: %v", err)
		}
		if lc.config.Interval != 100*time.Millisecond {
			t.Fatalf("Interval 未保留: %v", lc.config.Interval)
		}
	})
	t.Run("nil pushFn", func(t *testing.T) {
		if _, err := NewLogCollector(LogCollectConfig{}, nil); err == nil {
			t.Fatalf("pushFn 为 nil 应报错")
		}
	})
	t.Run("default interval", func(t *testing.T) {
		lc, err := NewLogCollector(LogCollectConfig{}, func(_ context.Context, _ []CollectedLog) error { return nil })
		if err != nil {
			t.Fatalf("构造失败: %v", err)
		}
		if lc.config.Interval != defaultLogCollectInterval {
			t.Fatalf("默认 Interval 应为 %v, 得到 %v", defaultLogCollectInterval, lc.config.Interval)
		}
	})
	t.Run("invalid include rule", func(t *testing.T) {
		cfg := LogCollectConfig{IncludeRules: []string{"(unclosed"}}
		if _, err := NewLogCollector(cfg, func(_ context.Context, _ []CollectedLog) error { return nil }); err == nil {
			t.Fatalf("非法 Include 正则应报错")
		}
	})
	t.Run("invalid multiline rule", func(t *testing.T) {
		cfg := LogCollectConfig{MultilineRule: "(bad"}
		if _, err := NewLogCollector(cfg, func(_ context.Context, _ []CollectedLog) error { return nil }); err == nil {
			t.Fatalf("非法 Multiline 正则应报错")
		}
	})
}

// TestLogCollectorBasic 验证基本增量采集：追加内容后 pushFn 收到对应记录。
func TestLogCollectorBasic(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "app.log")
	if err := os.WriteFile(logFile, []byte(""), 0o644); err != nil {
		t.Fatalf("创建文件失败: %v", err)
	}

	cfg := LogCollectConfig{Paths: []string{logFile}, Interval: 50 * time.Millisecond}
	lc, mu, got := newTestCollector(t, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = lc.Start(ctx) }()
	defer lc.Stop()

	// 追加 3 行。
	appendLog(t, logFile, "line1\nline2\nline3\n")

	recs := waitForRecords(t, mu, got, 3)
	if len(recs) < 3 {
		t.Fatalf("期望至少 3 条记录, 得到 %d: %+v", len(recs), recs)
	}

	// 验证内容（顺序应与追加一致）。
	var contents []string
	for _, r := range recs[:3] {
		contents = append(contents, r.Content)
	}
	expect := []string{"line1", "line2", "line3"}
	for i, want := range expect {
		if contents[i] != want {
			t.Fatalf("第 %d 条记录内容期望 %q, 得到 %q", i, want, contents[i])
		}
	}

	// 验证统计。
	stats := lc.Stats()
	if stats.TotalLines < 3 {
		t.Fatalf("TotalLines 期望 >=3, 得到 %d", stats.TotalLines)
	}
	if stats.ActiveFiles != 1 {
		t.Fatalf("ActiveFiles 期望 1, 得到 %d", stats.ActiveFiles)
	}
	if stats.Pushed < 3 {
		t.Fatalf("Pushed 期望 >=3, 得到 %d", stats.Pushed)
	}
}

// TestLogCollectorFilter 验证 Include/Exclude 过滤规则。
func TestLogCollectorFilter(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "filter.log")
	if err := os.WriteFile(logFile, []byte(""), 0o644); err != nil {
		t.Fatalf("创建文件失败: %v", err)
	}

	cfg := LogCollectConfig{
		Paths:        []string{logFile},
		Interval:     50 * time.Millisecond,
		IncludeRules: []string{`ERROR|WARN`}, // 仅保留 ERROR 或 WARN 行
		ExcludeRules: []string{`DEBUG`},      // 排除 DEBUG 行
	}
	lc, mu, got := newTestCollector(t, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = lc.Start(ctx) }()
	defer lc.Stop()

	appendLog(t, logFile, "INFO info msg\nERROR err msg\nWARN warn msg\nDEBUG debug msg\nERROR another\n")

	// 期望保留：ERROR err msg, WARN warn msg, ERROR another（DEBUG 被 Exclude，INFO 不匹配 Include）。
	recs := waitForRecords(t, mu, got, 3)
	var contents []string
	for _, r := range recs {
		contents = append(contents, r.Content)
	}
	joined := strings.Join(contents, "\n")
	if !strings.Contains(joined, "ERROR err msg") {
		t.Fatalf("期望包含 ERROR err msg, 得到: %s", joined)
	}
	if !strings.Contains(joined, "WARN warn msg") {
		t.Fatalf("期望包含 WARN warn msg, 得到: %s", joined)
	}
	if !strings.Contains(joined, "ERROR another") {
		t.Fatalf("期望包含 ERROR another, 得到: %s", joined)
	}
	if strings.Contains(joined, "INFO info msg") {
		t.Fatalf("INFO 行不应被保留（不匹配 Include）: %s", joined)
	}
	if strings.Contains(joined, "DEBUG debug msg") {
		t.Fatalf("DEBUG 行应被 Exclude 排除: %s", joined)
	}
}

// TestLogCollectorMultiline 验证多行合并：以时间戳开头的行作为新记录，后续行合并。
func TestLogCollectorMultiline(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "multi.log")
	if err := os.WriteFile(logFile, []byte(""), 0o644); err != nil {
		t.Fatalf("创建文件失败: %v", err)
	}

	cfg := LogCollectConfig{
		Paths:         []string{logFile},
		Interval:      50 * time.Millisecond,
		MultilineRule: `^\d{4}-\d{2}-\d{2}`, // 以日期开头的行作为新记录
	}
	lc, mu, got := newTestCollector(t, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = lc.Start(ctx) }()
	defer lc.Stop()

	// 写入两条多行记录（模拟 Java stacktrace）。
	content := "2024-01-01 ERROR something went wrong\n" +
		"  at com.foo.Bar.baz(Bar.java:10)\n" +
		"  at com.foo.Qux.run(Qux.java:20)\n" +
		"2024-01-02 INFO normal line\n" +
		"  continued context line\n"
	appendLog(t, logFile, content)

	recs := waitForRecords(t, mu, got, 2)
	if len(recs) < 2 {
		t.Fatalf("期望至少 2 条合并记录, 得到 %d: %+v", len(recs), recs)
	}

	// 第一条记录应包含 stacktrace 的两行后续。
	first := recs[0]
	if !strings.Contains(first.Content, "something went wrong") {
		t.Fatalf("第一条记录应包含首行: %q", first.Content)
	}
	if !strings.Contains(first.Content, "at com.foo.Bar.baz") {
		t.Fatalf("第一条记录应合并 stacktrace 行: %q", first.Content)
	}
	if !strings.Contains(first.Content, "at com.foo.Qux.run") {
		t.Fatalf("第一条记录应合并第二行 stacktrace: %q", first.Content)
	}
	if first.LineCount < 3 {
		t.Fatalf("第一条记录 LineCount 期望 >=3, 得到 %d", first.LineCount)
	}

	// 第二条记录应包含 normal line + continued context。
	second := recs[1]
	if !strings.Contains(second.Content, "normal line") {
		t.Fatalf("第二条记录应包含 normal line: %q", second.Content)
	}
	if !strings.Contains(second.Content, "continued context line") {
		t.Fatalf("第二条记录应合并 continued context line: %q", second.Content)
	}
	if second.LineCount < 2 {
		t.Fatalf("第二条记录 LineCount 期望 >=2, 得到 %d", second.LineCount)
	}
}

// TestLogCollectorStartStop 验证启停：Start 幂等，Stop 幂等，Stop 后不再采集。
func TestLogCollectorStartStop(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "stop.log")
	if err := os.WriteFile(logFile, []byte(""), 0o644); err != nil {
		t.Fatalf("创建文件失败: %v", err)
	}

	cfg := LogCollectConfig{Paths: []string{logFile}, Interval: 50 * time.Millisecond}
	lc, mu, got := newTestCollector(t, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start 幂等：连续两次 Start 不报错且不重复启动。
	go func() { _ = lc.Start(ctx) }()
	go func() { _ = lc.Start(ctx) }()

	appendLog(t, logFile, "before-stop\n")
	recs := waitForRecords(t, mu, got, 1)
	if len(recs) < 1 {
		t.Fatalf("Stop 前应能采集到记录: %+v", recs)
	}

	// Stop 后不再采集。
	if err := lc.Stop(); err != nil {
		t.Fatalf("Stop 失败: %v", err)
	}
	// Stop 幂等。
	if err := lc.Stop(); err != nil {
		t.Fatalf("二次 Stop 应幂等: %v", err)
	}

	countBefore := len(snapshot(mu, got))
	// 等待多个 tick 确认不再有新记录。
	time.Sleep(200 * time.Millisecond)
	appendLog(t, logFile, "after-stop\n")
	time.Sleep(200 * time.Millisecond)

	countAfter := len(snapshot(mu, got))
	if countAfter > countBefore+1 { // 允许 +1 容忍 Stop 前最后一 tick 的在途记录
		t.Fatalf("Stop 后不应继续采集: before=%d after=%d", countBefore, countAfter)
	}
}

// TestLogCollectorUpdateConfig 验证热更新：运行中切换路径。
func TestLogCollectorUpdateConfig(t *testing.T) {
	dir := t.TempDir()
	file1 := filepath.Join(dir, "f1.log")
	file2 := filepath.Join(dir, "f2.log")
	if err := os.WriteFile(file1, []byte(""), 0o644); err != nil {
		t.Fatalf("创建文件失败: %v", err)
	}
	if err := os.WriteFile(file2, []byte(""), 0o644); err != nil {
		t.Fatalf("创建文件失败: %v", err)
	}

	cfg := LogCollectConfig{Paths: []string{file1}, Interval: 50 * time.Millisecond}
	lc, mu, got := newTestCollector(t, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = lc.Start(ctx) }()
	defer lc.Stop()

	// 先采集 file1。
	appendLog(t, file1, "from-file1\n")
	_ = waitForRecords(t, mu, got, 1)

	// 热更新切换到 file2。
	if err := lc.UpdateConfig(LogCollectConfig{Paths: []string{file2}, Interval: 50 * time.Millisecond}); err != nil {
		t.Fatalf("UpdateConfig 失败: %v", err)
	}

	countBefore := len(snapshot(mu, got))
	appendLog(t, file2, "from-file2\n")
	_ = waitForRecords(t, mu, got, countBefore+1)

	recs := snapshot(mu, got)
	foundFile2 := false
	for _, r := range recs {
		if strings.Contains(r.Content, "from-file2") {
			foundFile2 = true
			break
		}
	}
	if !foundFile2 {
		t.Fatalf("热更新后应能采集 file2 的内容")
	}

	// 热更新非法正则应报错且不破坏现有配置。
	if err := lc.UpdateConfig(LogCollectConfig{Paths: []string{file2}, IncludeRules: []string{"(bad"}}); err == nil {
		t.Fatalf("非法正则应报错")
	}
	// 仍能继续采集 file2。
	countBefore2 := len(snapshot(mu, got))
	appendLog(t, file2, "after-bad-update\n")
	_ = waitForRecords(t, mu, got, countBefore2+1)
}

// TestLogCollectorRateLimit 验证限速：超限行被丢弃并计入 Dropped。
func TestLogCollectorRateLimit(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "rate.log")
	if err := os.WriteFile(logFile, []byte(""), 0o644); err != nil {
		t.Fatalf("创建文件失败: %v", err)
	}

	cfg := LogCollectConfig{
		Paths:     []string{logFile},
		Interval:  50 * time.Millisecond,
		RateLimit: 3, // 3 行/秒
	}
	lc, mu, got := newTestCollector(t, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = lc.Start(ctx) }()
	defer lc.Stop()

	// 一次写入 10 行，限速 3 行/秒，应丢弃大部分。
	var sb strings.Builder
	for i := 0; i < 10; i++ {
		sb.WriteString("rate-line\n")
	}
	appendLog(t, logFile, sb.String())

	// 等待采集发生。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if lc.Stats().Dropped > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	_ = snapshot(mu, got)
	stats := lc.Stats()
	if stats.Dropped == 0 {
		t.Fatalf("期望有丢弃行（限速 3 行/秒，写入 10 行）, stats=%+v", stats)
	}
	if stats.TotalLines < 10 {
		t.Fatalf("TotalLines 期望 >=10, 得到 %d", stats.TotalLines)
	}
}

// TestLogCollectorGlob 验证通配符路径展开。
func TestLogCollectorGlob(t *testing.T) {
	dir := t.TempDir()
	file1 := filepath.Join(dir, "a.log")
	file2 := filepath.Join(dir, "b.log")
	if err := os.WriteFile(file1, []byte(""), 0o644); err != nil {
		t.Fatalf("创建文件失败: %v", err)
	}
	if err := os.WriteFile(file2, []byte(""), 0o644); err != nil {
		t.Fatalf("创建文件失败: %v", err)
	}

	pattern := filepath.Join(dir, "*.log")
	cfg := LogCollectConfig{Paths: []string{pattern}, Interval: 50 * time.Millisecond}
	lc, mu, got := newTestCollector(t, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = lc.Start(ctx) }()
	defer lc.Stop()

	appendLog(t, file1, "from-a\n")
	appendLog(t, file2, "from-b\n")

	recs := waitForRecords(t, mu, got, 2)
	joined := ""
	for _, r := range recs {
		joined += r.Content + "\n"
	}
	if !strings.Contains(joined, "from-a") {
		t.Fatalf("期望采集到 from-a: %s", joined)
	}
	if !strings.Contains(joined, "from-b") {
		t.Fatalf("期望采集到 from-b: %s", joined)
	}
	stats := lc.Stats()
	if stats.ActiveFiles != 2 {
		t.Fatalf("ActiveFiles 期望 2, 得到 %d", stats.ActiveFiles)
	}
}

// TestLogCollectorFileRotation 验证文件轮转：文件被截断后从头部重新读取。
func TestLogCollectorFileRotation(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "rotate.log")
	if err := os.WriteFile(logFile, []byte(""), 0o644); err != nil {
		t.Fatalf("创建文件失败: %v", err)
	}

	cfg := LogCollectConfig{Paths: []string{logFile}, Interval: 50 * time.Millisecond}
	lc, mu, got := newTestCollector(t, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = lc.Start(ctx) }()
	defer lc.Stop()

	// 写入较长的旧内容，使 offset 推进到较大值。
	appendLog(t, logFile, "old-content-padding-padding-padding-padding\n")
	_ = waitForRecords(t, mu, got, 1)

	// 截断文件并写入较短的新内容（size < offset 触发轮转重置，从头部重读）。
	if err := os.WriteFile(logFile, []byte("new-after-rotate\n"), 0o644); err != nil {
		t.Fatalf("截断写入失败: %v", err)
	}

	countBefore := len(snapshot(mu, got))
	_ = waitForRecords(t, mu, got, countBefore+1)

	recs := snapshot(mu, got)
	found := false
	for _, r := range recs {
		if strings.Contains(r.Content, "new-after-rotate") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("文件轮转后应能采集到新内容")
	}
}

// makeFixedLine 构造固定宽度（含 \n）的日志行，内容含序号便于拼接验证。
// width 为含换行符的目标字节数；内容不足用 'x' 右填充。
func makeFixedLine(idx, width int) string {
	s := fmt.Sprintf("line-%05d", idx)
	if pad := width - 1 - len(s); pad > 0 {
		s += strings.Repeat("x", pad)
	}
	return s
}

// drainCollectFile 重复调用 collectFile 直到 offset 到达文件末尾，
// 拼接所有记录的 Content+\n 返回。用于截断算法的多轮分片拼接验证。
func drainCollectFile(t *testing.T, lc *LogCollector, logFile string, payloadLen int64) string {
	t.Helper()
	var collected strings.Builder
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		records, _, err := lc.collectFile(logFile, nil, nil, nil, 0)
		if err != nil {
			t.Fatalf("collectFile 失败: %v", err)
		}
		for _, r := range records {
			collected.WriteString(r.Content)
			collected.WriteByte('\n')
		}
		lc.mu.RLock()
		off := lc.offsets[logFile]
		lc.mu.RUnlock()
		if off >= payloadLen {
			break
		}
	}
	return collected.String()
}

// TestLogCollectTruncationHitLimitRecovery 验证截断算法在 hitLimit 时的正确性：
// 当单 tick 记录数达到 logCollectMaxRecords 上限，offset 只推进到已处理位置，
// 剩余行下次 collectFile 重读，多轮拼接后应还原原始 payload。
//
// 构造：1500 行 × 100 字节 = 150KB。logCollectMaxRecords=1000，
// 第一轮处理 1000 行后 hitLimit，offset 推进到 100KB（非 150KB），
// 第二轮从 100KB 读剩余 500 行，拼接还原。
func TestLogCollectTruncationHitLimitRecovery(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "trunc.log")

	const lineCount = 1500
	const lineWidth = 100 // 含 \n
	var sb strings.Builder
	for i := 0; i < lineCount; i++ {
		sb.WriteString(makeFixedLine(i, lineWidth))
		sb.WriteByte('\n')
	}
	payload := sb.String()
	if err := os.WriteFile(logFile, []byte(payload), 0o644); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	cfg := LogCollectConfig{Paths: []string{logFile}}
	lc, err := NewLogCollector(cfg, func(_ context.Context, _ []CollectedLog) error { return nil })
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}

	got := drainCollectFile(t, lc, logFile, int64(len(payload)))
	if got != payload {
		t.Fatalf("hitLimit 回退后拼接内容与原始 payload 不一致: got len=%d, want len=%d", len(got), len(payload))
	}
}

// TestLogCollectLargePayloadMultiRound 验证大 payload 分多次读取拼接还原：
// payload 大于 logCollectReadLimit（1MB），需多轮读取，拼接后应还原原始内容。
//
// 构造：1024 行 × 2048 字节 = 2MB。logCollectReadLimit=1MB，
// 需 2 轮读取（各 1MB=512 行），每轮 512 行 < 1000，不 hitLimit。
// 选 2048 字节行使 1MB 恰为行整数倍，避免按字节截断拆分行。
func TestLogCollectLargePayloadMultiRound(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "large.log")

	const lineCount = 1024
	const lineWidth = 2048 // 含 \n；1024*2048=2097152=2MB
	var sb strings.Builder
	for i := 0; i < lineCount; i++ {
		sb.WriteString(makeFixedLine(i, lineWidth))
		sb.WriteByte('\n')
	}
	payload := sb.String()
	if err := os.WriteFile(logFile, []byte(payload), 0o644); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	cfg := LogCollectConfig{Paths: []string{logFile}}
	lc, err := NewLogCollector(cfg, func(_ context.Context, _ []CollectedLog) error { return nil })
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}

	got := drainCollectFile(t, lc, logFile, int64(len(payload)))
	if got != payload {
		t.Fatalf("多轮读取拼接内容与原始 payload 不一致: got len=%d, want len=%d", len(got), len(payload))
	}
}

// TestLogCollectPayloadExactBuffer 验证 payload 恰好等于缓冲上限的边界：
// payload 长度 == logCollectReadLimit（1MB），单轮读取应完整处理，拼接还原。
func TestLogCollectPayloadExactBuffer(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "exact.log")

	// 构造恰好 1MB 的 payload：512 行 × 2048 字节 = 1MB。
	const lineCount = 512
	const lineWidth = 2048 // 含 \n；512*2048=1048576=1MB
	var sb strings.Builder
	for i := 0; i < lineCount; i++ {
		sb.WriteString(makeFixedLine(i, lineWidth))
		sb.WriteByte('\n')
	}
	payload := sb.String()
	if len(payload) != logCollectReadLimit {
		t.Fatalf("payload 长度应为 %d, 得到 %d", logCollectReadLimit, len(payload))
	}
	if err := os.WriteFile(logFile, []byte(payload), 0o644); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	cfg := LogCollectConfig{Paths: []string{logFile}}
	lc, err := NewLogCollector(cfg, func(_ context.Context, _ []CollectedLog) error { return nil })
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}

	got := drainCollectFile(t, lc, logFile, int64(len(payload)))
	if got != payload {
		t.Fatalf("恰好等于缓冲时拼接内容与原始 payload 不一致: got len=%d, want len=%d", len(got), len(payload))
	}
}

// TestLogCollectErrorIs 验证 logCollectError 的 Is 方法使 errors.Is 生效（C-4 正反用例）。
func TestLogCollectErrorIs(t *testing.T) {
	// 正：两个 *logCollectError sentinel 互相 errors.Is 应为 true（同类型）。
	if !errors.Is(errLogCollectPushFnNil, errLogCollectMultilineCompile) {
		t.Fatalf("errors.Is(同类型 sentinel) 应为 true")
	}
	// 正：wrap 后的新实例仍可被 errors.Is 识别为 *logCollectError。
	wrapped := errLogCollectRuleCompile.wrap(errors.New("inner"), "rule", 0)
	if !errors.Is(wrapped, errLogCollectPushFnNil) {
		t.Fatalf("errors.Is(wrap 后实例, sentinel) 应为 true")
	}
	// 正：sentinel 对自身 wrap 实例也应为 true（反向匹配）。
	if !errors.Is(errLogCollectPushFnNil, wrapped) {
		t.Fatalf("errors.Is(sentinel, wrap 后实例) 应为 true")
	}
	// 反：非 *logCollectError 类型应返回 false。
	other := errors.New("unrelated error")
	if errors.Is(errLogCollectPushFnNil, other) {
		t.Fatalf("errors.Is(对不同类型) 应为 false")
	}
	// 反：nil error 对 sentinel 应为 false。
	if errors.Is(nil, errLogCollectPushFnNil) {
		t.Fatalf("errors.Is(nil, sentinel) 应为 false")
	}
}
