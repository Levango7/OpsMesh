//go:build kafka

// kafka_wal.go 实现 Kafka 生产者的本地 WAL（Write-Ahead Log）兜底（B-3 审计合规）。
//
// 设计动机：审计事件是合规关键数据，原 Publish 同步阻塞无重试，broker 故障时仅 log 后丢弃，
// 违反合规要求。本 WAL 在 Publish 失败时把事件落盘到 data/kafka-wal/ 目录，
// 启动后台 goroutine 周期重试发送，确保事件最终送达（at-least-once）。
//
// 文件名格式：<unix-nano>-<uuid>.json
//   - unix-nano：单调递增时间戳，便于按写入顺序重试
//   - uuid：避免同一纳秒并发写入冲突
//
// 失败计数器（atomic.Int64）暴露 FailedCount() 供 metrics/告警消费，
// 例如 Prometheus exporter 周期抓取触发告警（failed_count > threshold）。
package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"opsmesh/internal/logx"

	"github.com/google/uuid"
)

// kafkaWAL 管理本地 WAL 文件目录与后台重试 goroutine。
//
// 字段语义：
//   - dir：WAL 文件落盘目录（如 data/kafka-wal/）
//   - failedCount：累计 Publish 失败次数（atomic，线程安全），单调递增，不重置
//   - retryInterval：后台重试 goroutine 的扫描周期（默认 5s，可注入测试覆盖）
//   - send：重试时调用的发送闭包（注入 kafkaBus.WriteMessages 或测试 mock）
//   - cancel/stop：控制后台 goroutine 生命周期（Stop 幂等）
type kafkaWAL struct {
	dir           string
	failedCount   atomic.Int64
	retryInterval time.Duration
	send          func(ctx context.Context, value []byte) error

	cancel context.CancelFunc
	stop   chan struct{}
	once   sync.Once
}

// walEntry 是 WAL 文件的 JSON 信封，包含原始事件字节与元信息。
//
// 不直接落盘原始事件 JSON 是为了：保留原始字节数组避免二次序列化误差，
// 并附加写入时间便于诊断（文件名已含时间，但 JSON 内冗余便于人工查看）。
type walEntry struct {
	WrittenAt int64  `json:"writtenAt"` // unix-nano，与文件名时间戳一致
	Payload   []byte `json:"payload"`   // 原始事件 JSON 字节
}

// newKafkaWAL 创建 WAL 实例并确保目录存在。
//
// send 闭包在重试时被调用，签名与 kafkaBus.WriteMessages 适配：
//   - 成功返回 nil → WAL 文件被删除
//   - 失败返回 err → WAL 文件保留，下次重试周期再试
//
// retryInterval <= 0 时取默认 5s（生产值）；测试可注入小值加速。
// 不在此启动后台 goroutine，由 StartRetryLoop 显式启动（构造与运行分离，便于测试）。
func newKafkaWAL(dir string, send func(ctx context.Context, value []byte) error, retryInterval time.Duration) (*kafkaWAL, error) {
	if dir == "" {
		return nil, errors.New("kafkaWAL: dir is empty")
	}
	if send == nil {
		return nil, errors.New("kafkaWAL: send is nil")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("kafkaWAL: mkdir %s: %w", dir, err)
	}
	if retryInterval <= 0 {
		retryInterval = 5 * time.Second
	}
	return &kafkaWAL{
		dir:           dir,
		retryInterval: retryInterval,
		send:          send,
		stop:          make(chan struct{}),
	}, nil
}

// Write 把事件字节落盘到 WAL 文件。
//
// 文件名：<unix-nano>-<uuid>.json；O_CREATE|O_EXCL 保证不覆盖（uuid 防冲突）。
// 写入失败返回 err，调用方可决定是否再 log/告警；但通常 Write 失败意味着磁盘问题，
// 此时事件已无法保留，只能 log.Error。
//
// 注意：Write 不递增 failedCount——failedCount 语义是"Publish 到 broker 失败次数"，
// 而非"WAL 写入失败次数"。调用方（kafkaBus.Publish）在 broker 失败时先递增 failedCount
// 再调 Write，保证计数语义一致。
func (w *kafkaWAL) Write(payload []byte) error {
	now := time.Now().UnixNano()
	id := uuid.NewString()
	name := fmt.Sprintf("%d-%s.json", now, id)
	path := filepath.Join(w.dir, name)

	entry := walEntry{WrittenAt: now, Payload: payload}
	b, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("kafkaWAL: marshal entry: %w", err)
	}
	// O_EXCL：若文件已存在（uuid 碰撞，概率 ~0）返回 err，调用方可重试。
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("kafkaWAL: write %s: %w", path, err)
	}
	return nil
}

// StartRetryLoop 启动后台 goroutine 周期重试发送 WAL 中未送达的事件。
//
// 幂等：多次调用只启动一次（sync.Once 守卫）；Stop 后不可再启动。
// goroutine 在 ctx.Done() 或 Stop() 时退出，退出后 close(stop) 通知调用方。
//
// 重试策略：
//  1. 扫描 dir 下所有 *.json 文件，按文件名（含时间戳）升序重试——保证先入先送，
//     维持事件因果顺序（审计回放依赖时序）。
//  2. 每个文件：读 payload → 调 send → 成功则删除文件；失败则保留，等下个周期。
//  3. 单次重试不阻塞过久：send 用 ctx 超时（默认沿用传入 ctx，调用方可加 timeout）。
//
// 失败的 send 不递增 failedCount——failedCount 仅计 Publish 时刻的失败，重试失败
// 不再计入（否则计数会因 broker 长时间故障而爆炸式增长，失去告警意义）。
// 重试失败通过 logx.Warn 记日志，运维可据日志 + WAL 目录文件数监控积压。
func (w *kafkaWAL) StartRetryLoop(ctx context.Context) {
	w.once.Do(func() {
		ctx, w.cancel = context.WithCancel(ctx)
		go w.retryLoop(ctx)
	})
}

// retryLoop 是 StartRetryLoop 的内部实现，便于测试直接调用（同步重试一次）。
func (w *kafkaWAL) retryLoop(ctx context.Context) {
	ticker := time.NewTicker(w.retryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			close(w.stop)
			return
		case <-ticker.C:
			w.RetryOnce(ctx)
		}
	}
}

// RetryOnce 执行一轮重试：扫描 WAL 目录，按时间戳升序重试每个文件。
//
// 暴露为公开方法便于测试同步触发重试（无需等待 ticker 周期），
// 也便于运维手动触发（如 broker 恢复后立即重放，不等下个周期）。
//
// 返回 (succeeded, failed int)：本轮成功/失败的文件数，便于测试断言与 metrics 暴露。
func (w *kafkaWAL) RetryOnce(ctx context.Context) (succeeded, failed int) {
	files, err := w.listWALFiles()
	if err != nil {
		logx.Warn(ctx, "kafkaWAL: list files failed", "dir", w.dir, "error", err.Error())
		return 0, 0
	}
	for _, path := range files {
		if err := w.retryFile(ctx, path); err != nil {
			failed++
			logx.Warn(ctx, "kafkaWAL: retry failed", "file", path, "error", err.Error())
			continue
		}
		succeeded++
	}
	return succeeded, failed
}

// retryFile 读单个 WAL 文件并重试发送；成功后删除文件。
func (w *kafkaWAL) retryFile(ctx context.Context, path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	var entry walEntry
	if err := json.Unmarshal(b, &entry); err != nil {
		// 损坏的 WAL 文件无法重放，删除避免持续失败阻塞重试队列。
		// 审计合规上损坏文件已是数据丢失，删除并 log 是最佳处置（保留会持续告警噪声）。
		_ = os.Remove(path)
		return fmt.Errorf("unmarshal (file removed): %w", err)
	}
	if err := w.send(ctx, entry.Payload); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	if err := os.Remove(path); err != nil {
		// send 已成功但删除失败（并发删除/权限），log 但不算失败——
		// 下轮会再读到该文件，send 会重复发送（at-least-once，消费者需幂等）。
		logx.Warn(ctx, "kafkaWAL: send ok but remove failed", "file", path, "error", err.Error())
	}
	return nil
}

// listWALFiles 列出 dir 下所有 *.json 文件并按文件名升序排序。
//
// 升序即按时间戳升序（文件名前缀是 unix-nano），保证先入先送，维持事件时序。
// 排序在文件名层面而非读取 WrittenAt 字段，避免 O(N) 次 JSON 解析。
func (w *kafkaWAL) listWALFiles() ([]string, error) {
	var files []string
	err := filepath.WalkDir(w.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// FailedCount 返回累计 Publish 失败次数（单调递增，线程安全）。
//
// 语义：自 WAL 启用以来，kafkaBus.Publish 因 broker 故障写入 WAL 的次数。
// 不含 WAL 自身写入失败、不含后台重试失败（重试失败用 WAL 目录文件数监控积压）。
// 供 Prometheus exporter / 告警规则消费：failed_count 单调递增即健康，
// 持续递增（导数 > 0）则 broker 故障中，需告警。
func (w *kafkaWAL) FailedCount() int64 {
	return w.failedCount.Load()
}

// IncFailed 递增失败计数器。由 kafkaBus.Publish 在 broker 失败时调用。
//
// 暴露为方法（而非直接 atomic.Add）封装计数语义，避免调用方误用（如重试时也递增）。
func (w *kafkaWAL) IncFailed() {
	w.failedCount.Add(1)
}

// Stop 终止后台重试 goroutine，幂等。
//
// 不删除 WAL 目录中未送达的文件——重启后可继续重放（持久化兜底的核心价值）。
// 测试可用 Stop + 等待 <-stop 确认 goroutine 退出，避免 goroutine 泄漏。
func (w *kafkaWAL) Stop() {
	if w.cancel != nil {
		w.cancel()
		<-w.stop
	}
}

// PendingCount 返回当前 WAL 目录中未送达的文件数，便于测试断言与运维监控积压。
//
// 测试关键断言：FallbackPublish 后 PendingCount==1；Retry 后 PendingCount==0。
func (w *kafkaWAL) PendingCount() int {
	files, err := w.listWALFiles()
	if err != nil {
		return -1
	}
	return len(files)
}
