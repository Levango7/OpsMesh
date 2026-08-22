//go:build kafka

// Package events 的 Kafka 生产者实现（仅 -tags kafka 构建包含）。
// 依赖 github.com/segmentio/kafka-go；默认构建不含此文件，故 go.mod 也无需声明该依赖。
// 启用方式：
//
//	go get github.com/segmentio/kafka-go
//	go build -tags kafka ./...
//	./opsmesh --event-bus=kafka --kafka-brokers=broker1:9092,broker2:9092 --kafka-topic=opsmesh-audit
package events

import (
	"context"
	"encoding/json"
	"strings"

	"opsmesh/internal/logx"

	"github.com/segmentio/kafka-go"
)

// newKafkaBus 由调用方传入 brokers/topic（参数传递，避免 os.Setenv 竞态）；
// 未配置时返回 nil（调用方回退 noop）。
//
// 不启用 WAL（wal 字段为 nil）：保持向后兼容，原 New(kind,...) 路径无 WAL 兜底。
// 需 WAL 兜底时用 NewKafkaBusWithWAL 显式构造（审计合规）。
func newKafkaBus(brokers, topic string) Bus {
	clean := strings.Split(brokers, ",")
	filtered := clean[:0]
	for _, b := range clean {
		if b = strings.TrimSpace(b); b != "" {
			filtered = append(filtered, b)
		}
	}
	if len(filtered) == 0 || topic == "" {
		return nil
	}
	return &kafkaBus{w: kafka.NewWriter(kafka.WriterConfig{Brokers: filtered, Topic: topic})}
}

// kafkaBus 实现 Bus 接口，封装 kafka-go Writer。
//
// 字段语义：
//   - w：底层 kafka-go Writer（同步阻塞发送）
//   - wal：本地 WAL 兜底（nil 表示不启用，向后兼容原 newKafkaBus 路径）
//
// Publish 行为（审计合规）：
//  1. stamp 加盖契约版本 → JSON 序列化
//  2. 调 w.WriteMessages 同步发送
//  3. 成功 → 返回 nil
//  4. 失败 → 若 wal 启用：递增 failedCount + 写入 WAL（at-least-once 兜底）+ log.Warn + 返回 err
//     若 wal 未启用：log.Warn + 返回 err（原行为，向后兼容）
//
// 注意：返回 err 不变（调用方仍知发送失败），但事件已落盘 WAL，后台 goroutine 会重试。
// 调用方据 err 可决定是否告警，但事件不会丢失（合规要求满足）。
type kafkaBus struct {
	w   *kafka.Writer
	wal *kafkaWAL
}

// Publish 发布事件到 Kafka，失败时落盘 WAL 兜底（审计合规）。
//
// 关键设计：即使 WAL 启用，Publish 仍返回 err（而非吞掉错误返回 nil）。
// 原因：调用方（如 stampingBus）据 err 决定是否告警/降级，吞掉会破坏上层错误处理语义。
// WAL 兜底是"事件不丢"的保证，不是"调用方无感知"的保证——两者正交。
func (k *kafkaBus) Publish(ctx context.Context, e Event) error {
	e = stamp(e)
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := k.w.WriteMessages(ctx, kafka.Message{Value: b}); err != nil {
		// broker 故障：log.Warn（保留原可观测性）+ WAL 兜底（新增合规保证）。
		logx.Warn(ctx, "kafka publish failed, falling back to WAL", "action", e.Action, "error", err.Error())
		if k.wal != nil {
			k.wal.IncFailed()
			if werr := k.wal.Write(b); werr != nil {
				// WAL 写入失败意味着磁盘问题，事件已无法保留——log.Error 是最后手段。
				// 此处不返回 werr 而返回原 err：调用方关心的是 broker 失败（业务语义），
				// WAL 失败是基础设施问题，由 log.Error 触发运维介入。
				logx.Error(ctx, "kafka WAL write failed (event lost)", werr, "action", e.Action)
			}
		}
		return err
	}
	return nil
}

// NewKafkaBusWithWAL 构造启用 WAL 兜底的 Kafka Bus（审计合规）。
//
// 参数：
//   - brokers：逗号分隔的 broker 地址列表（同 newKafkaBus）
//   - topic：Kafka topic
//   - walDir：WAL 文件落盘目录（如 data/kafka-wal/），空串或不合法则不启用 WAL
//
// 返回的 Bus 已启动后台重试 goroutine（基于 context.Background()），
// goroutine 在进程退出时自然终止；如需显式停止，调用方应保留返回的 *kafkaBus 引用并调 Stop()。
//
// 设计权衡：返回 Bus 接口而非 *kafkaBus，与 New(kind,...) 签名一致便于替换。
// 测试需访问 wal 字段时，可通过类型断言拿到 *kafkaBus。
//
// brokers/topic 不合法（空）时返回 nil，调用方回退 noop——与 newKafkaBus 一致。
func NewKafkaBusWithWAL(brokers, topic, walDir string) Bus {
	clean := strings.Split(brokers, ",")
	filtered := clean[:0]
	for _, b := range clean {
		if b = strings.TrimSpace(b); b != "" {
			filtered = append(filtered, b)
		}
	}
	if len(filtered) == 0 || topic == "" {
		return nil
	}
	w := kafka.NewWriter(kafka.WriterConfig{Brokers: filtered, Topic: topic})
	bus := &kafkaBus{w: w}
	if walDir != "" {
		wal, err := newKafkaWAL(walDir, func(ctx context.Context, value []byte) error {
			return w.WriteMessages(ctx, kafka.Message{Value: value})
		}, 0) // 0 → 默认 5s 重试周期
		if err != nil {
			// WAL 初始化失败（如目录创建权限）不阻断启动——降级为无 WAL 的 kafkaBus。
			// log.Warn 触发运维介入修复目录权限，重启后恢复 WAL 兜底。
			logx.Warn(context.Background(), "kafka WAL init failed, running without WAL fallback", "walDir", walDir, "error", err.Error())
		} else {
			bus.wal = wal
			wal.StartRetryLoop(context.Background())
		}
	}
	return bus
}

// StopWAL 终止 WAL 后台重试 goroutine（幂等），供优雅停机调用。
//
// 若 WAL 未启用（wal==nil）或 newKafkaBus 构造（无 WAL），no-op。
// 不删除 WAL 文件——重启后可继续重放（持久化兜底核心价值）。
func (k *kafkaBus) StopWAL() {
	if k.wal != nil {
		k.wal.Stop()
	}
}

// FailedCount 返回累计 Publish 失败次数（metrics/告警）。
//
// WAL 未启用时返回 0（无 WAL 即无失败计数语义）。
func (k *kafkaBus) FailedCount() int64 {
	if k.wal == nil {
		return 0
	}
	return k.wal.FailedCount()
}
