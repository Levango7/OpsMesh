//go:build kafka

// kafka_test.go 测试 Kafka 生产者的 WAL 兜底机制（B-3 审计合规）。
//
// 测试策略：不依赖真实 Kafka broker（CI 无 broker 容器），分两层：
//  1. 单元层：直接构造 kafkaWAL，注入 mock send 闭包模拟 broker 故障/恢复，
//     验证 WAL 文件操作、失败计数、重试逻辑。快速且确定性强。
//  2. 集成层：构造 kafkaBus + 不可达 broker（localhost:1 立即拒绝连接），
//     验证 Publish 失败时落盘 WAL、FailedCount 递增——验证 kafkaBus.Publish 与 WAL 的集成。
//
// 所有测试用 t.TempDir() 隔离 WAL 目录，t.Cleanup 注册 StopWAL 防 goroutine 泄漏。
package events

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestKafkaWAL_FallbackPublish 验证 broker 不可用时事件落盘 WAL 文件（B-3 审计合规核心）。
//
// 场景：mock send 总是返回 error（broker 故障），调用 Write 后：
//   - WAL 目录下应有 1 个 .json 文件
//   - 文件名格式：<unix-nano>-<uuid>.json
//   - 文件内容可反序列化为 walEntry，payload 与写入一致
//   - PendingCount()==1
func TestKafkaWAL_FallbackPublish(t *testing.T) {
	dir := t.TempDir()
	send := func(ctx context.Context, value []byte) error {
		return errors.New("broker unreachable")
	}
	wal, err := newKafkaWAL(dir, send, 0)
	if err != nil {
		t.Fatalf("newKafkaWAL: %v", err)
	}

	payload, _ := json.Marshal(Event{Action: "register", Level: LevelInfo})
	if err := wal.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if got := wal.PendingCount(); got != 1 {
		t.Fatalf("PendingCount = %d, want 1", got)
	}

	files, err := wal.listWALFiles()
	if err != nil || len(files) != 1 {
		t.Fatalf("listWALFiles: err=%v, len=%d", err, len(files))
	}

	// 验证文件名格式：<unix-nano>-<uuid>.json
	name := filepath.Base(files[0])
	if !strings.HasSuffix(name, ".json") {
		t.Fatalf("WAL file name lacks .json suffix: %s", name)
	}
	// <nano>-<uuid>.json → 去后缀后应含至少 2 段（nano 和 uuid），由 '-' 分隔
	stem := strings.TrimSuffix(name, ".json")
	parts := strings.Split(stem, "-")
	if len(parts) < 2 {
		t.Fatalf("WAL file name malformed (want <nano>-<uuid>.json): %s", name)
	}

	// 验证文件内容可反序列化为 walEntry，payload 一致
	b, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var entry walEntry
	if err := json.Unmarshal(b, &entry); err != nil {
		t.Fatalf("Unmarshal walEntry: %v", err)
	}
	if string(entry.Payload) != string(payload) {
		t.Fatalf("payload mismatch: got %q want %q", entry.Payload, payload)
	}
	if entry.WrittenAt <= 0 {
		t.Fatalf("WrittenAt should be positive unix-nano, got %d", entry.WrittenAt)
	}
}

// TestKafkaWAL_FailedCount 验证失败计数器递增（B-3 metrics/告警）。
//
// 场景：连续调用 IncFailed 5 次，FailedCount 应从 0 → 5。
// 验证计数器单调递增、线程安全（atomic.Int64 保证）。
func TestKafkaWAL_FailedCount(t *testing.T) {
	dir := t.TempDir()
	send := func(ctx context.Context, value []byte) error {
		return errors.New("broker unreachable")
	}
	wal, err := newKafkaWAL(dir, send, 0)
	if err != nil {
		t.Fatalf("newKafkaWAL: %v", err)
	}

	if got := wal.FailedCount(); got != 0 {
		t.Fatalf("initial FailedCount = %d, want 0", got)
	}

	const n = 5
	for i := 0; i < n; i++ {
		wal.IncFailed()
	}
	if got := wal.FailedCount(); got != n {
		t.Fatalf("FailedCount = %d, want %d", got, n)
	}

	// 并发递增验证线程安全（atomic.Int64）
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			wal.IncFailed()
		}()
	}
	wg.Wait()
	if got := wal.FailedCount(); got != n+100 {
		t.Fatalf("FailedCount after concurrent Inc = %d, want %d", got, n+100)
	}
}

// TestKafkaWAL_Retry 验证 broker 恢复后 WAL 事件被重试发送并删除文件（B-3 at-least-once）。
//
// 场景分两阶段：
//  1. broker 故障：Write 落盘 → RetryOnce 应失败（succeeded=0, failed=1），文件保留
//  2. broker 恢复：RetryOnce 应成功（succeeded=1, failed=0），文件删除，PendingCount=0
//
// 验证 WAL 兜底的核心价值：事件最终送达，不丢失。
func TestKafkaWAL_Retry(t *testing.T) {
	dir := t.TempDir()
	var sendMu sync.Mutex
	sendOK := false
	send := func(ctx context.Context, value []byte) error {
		sendMu.Lock()
		defer sendMu.Unlock()
		if sendOK {
			return nil // broker 恢复
		}
		return errors.New("broker unreachable")
	}
	wal, err := newKafkaWAL(dir, send, 0)
	if err != nil {
		t.Fatalf("newKafkaWAL: %v", err)
	}

	payload, _ := json.Marshal(Event{Action: "register", Level: LevelInfo})
	if err := wal.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := wal.PendingCount(); got != 1 {
		t.Fatalf("PendingCount after Write = %d, want 1", got)
	}

	// 阶段 1：broker 仍故障，重试应失败，文件保留
	succeeded, failed := wal.RetryOnce(context.Background())
	if succeeded != 0 || failed != 1 {
		t.Fatalf("RetryOnce (broker down): succeeded=%d failed=%d, want 0/1", succeeded, failed)
	}
	if got := wal.PendingCount(); got != 1 {
		t.Fatalf("PendingCount after failed retry = %d, want 1", got)
	}

	// 阶段 2：broker 恢复，重试应成功，文件删除
	sendMu.Lock()
	sendOK = true
	sendMu.Unlock()

	succeeded, failed = wal.RetryOnce(context.Background())
	if succeeded != 1 || failed != 0 {
		t.Fatalf("RetryOnce (broker up): succeeded=%d failed=%d, want 1/0", succeeded, failed)
	}
	if got := wal.PendingCount(); got != 0 {
		t.Fatalf("PendingCount after successful retry = %d, want 0", got)
	}
}

// TestKafkaWAL_RetryFIFO 验证多个 WAL 事件按写入时间戳升序重试（FIFO，审计时序）。
//
// 审计回放依赖事件时序：先写入的事件应先重试发送。
// 文件名前缀是 unix-nano，listWALFiles 按文件名升序排序，保证 FIFO。
func TestKafkaWAL_RetryFIFO(t *testing.T) {
	dir := t.TempDir()
	var mu sync.Mutex
	var sentOrder []string // 记录实际发送顺序的 action
	send := func(ctx context.Context, value []byte) error {
		mu.Lock()
		defer mu.Unlock()
		var e Event
		_ = json.Unmarshal(value, &e)
		sentOrder = append(sentOrder, e.Action)
		return nil
	}
	wal, err := newKafkaWAL(dir, send, 0)
	if err != nil {
		t.Fatalf("newKafkaWAL: %v", err)
	}

	// 按顺序写入 3 个事件（不同 action 便于追踪）
	actions := []string{"first", "second", "third"}
	for _, a := range actions {
		payload, _ := json.Marshal(Event{Action: a})
		if err := wal.Write(payload); err != nil {
			t.Fatalf("Write %s: %v", a, err)
		}
		// 短暂 sleep 保证时间戳递增（同纳秒写入时文件名 uuid 防冲突，但排序需时间戳差异）
		time.Sleep(1 * time.Millisecond)
	}

	if got := wal.PendingCount(); got != 3 {
		t.Fatalf("PendingCount = %d, want 3", got)
	}

	succeeded, failed := wal.RetryOnce(context.Background())
	if succeeded != 3 || failed != 0 {
		t.Fatalf("RetryOnce: succeeded=%d failed=%d, want 3/0", succeeded, failed)
	}

	// 验证发送顺序与写入顺序一致（FIFO）
	mu.Lock()
	defer mu.Unlock()
	if len(sentOrder) != 3 {
		t.Fatalf("sentOrder len = %d, want 3", len(sentOrder))
	}
	for i, want := range actions {
		if sentOrder[i] != want {
			t.Fatalf("sentOrder[%d] = %q, want %q (FIFO violated)", i, sentOrder[i], want)
		}
	}
}

// TestKafkaWAL_RetryLoopStop 验证 StartRetryLoop/Stop 的 goroutine 生命周期管理。
//
// 防止 goroutine 泄漏：Stop 后 goroutine 应退出，多次 Stop 幂等不 panic。
func TestKafkaWAL_RetryLoopStop(t *testing.T) {
	dir := t.TempDir()
	var sendCalls atomic.Int32
	send := func(ctx context.Context, value []byte) error {
		sendCalls.Add(1)
		return nil
	}
	wal, err := newKafkaWAL(dir, send, 10*time.Millisecond) // 短间隔加速测试
	if err != nil {
		t.Fatalf("newKafkaWAL: %v", err)
	}

	// 写入一个事件供重试
	payload, _ := json.Marshal(Event{Action: "register"})
	if err := wal.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}

	wal.StartRetryLoop(context.Background())

	// 等待后台 goroutine 重试成功（短间隔 + 短等待）
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if wal.PendingCount() == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := wal.PendingCount(); got != 0 {
		t.Fatalf("PendingCount after retry loop = %d, want 0 (goroutine should have retried)", got)
	}

	// Stop 幂等：多次调用不 panic
	wal.Stop()
	wal.Stop()
}

// TestKafkaBus_PublishWALFallback 验证 kafkaBus.Publish 在 broker 故障时写入 WAL（集成层）。
//
// 用不可达 broker（localhost:1 立即拒绝连接）模拟 broker 故障：
//   - Publish 应返回 error（调用方仍知失败，错误语义不被吞）
//   - WAL 应有 1 个文件（事件落盘兜底）
//   - FailedCount 应为 1（计数器递增）
//
// 这验证 kafkaBus.Publish 与 kafkaWAL 的集成：Publish 失败 → IncFailed + Write。
func TestKafkaBus_PublishWALFallback(t *testing.T) {
	dir := t.TempDir()
	// localhost:1 立即拒绝连接（无监听），kafka-go dial 快速失败
	bus := NewKafkaBusWithWAL("localhost:1", "test-topic", dir)
	if bus == nil {
		t.Fatal("NewKafkaBusWithWAL returned nil")
	}
	kb, ok := bus.(*kafkaBus)
	if !ok {
		t.Fatal("expected *kafkaBus")
	}
	t.Cleanup(func() { kb.StopWAL() })

	// 用短超时 context 防止 dial 卡住（理论上 localhost:1 立即拒绝，但保守加超时）
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := bus.Publish(ctx, Event{Action: "register", Level: LevelInfo})
	if err == nil {
		t.Fatal("Publish should fail with unreachable broker")
	}

	if got := kb.FailedCount(); got != 1 {
		t.Fatalf("FailedCount = %d, want 1", got)
	}
	if got := kb.wal.PendingCount(); got != 1 {
		t.Fatalf("WAL PendingCount = %d, want 1", got)
	}
}

// TestKafkaBus_NoWALBackwardCompat 验证 newKafkaBus（无 WAL）的向后兼容性。
//
// newKafkaBus 不启用 WAL（wal==nil），Publish 失败时：
//   - 不写 WAL（无 dir，无文件）
//   - FailedCount 返回 0（无 WAL 即无计数语义）
//   - 仍返回 error（原行为保留）
func TestKafkaBus_NoWALBackwardCompat(t *testing.T) {
	bus := newKafkaBus("localhost:1", "test-topic")
	if bus == nil {
		t.Fatal("newKafkaBus returned nil")
	}
	kb, ok := bus.(*kafkaBus)
	if !ok {
		t.Fatal("expected *kafkaBus")
	}
	if kb.wal != nil {
		t.Fatal("newKafkaBus should not enable WAL (backward compat)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := bus.Publish(ctx, Event{Action: "register"})
	if err == nil {
		t.Fatal("Publish should fail with unreachable broker")
	}
	if got := kb.FailedCount(); got != 0 {
		t.Fatalf("FailedCount without WAL = %d, want 0", got)
	}
}
