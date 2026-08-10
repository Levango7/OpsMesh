// Package events 提供可插拔事件总线（审计/告警），内核产出的事件统一经 Bus 发布（P1-5）。
// 默认 noop / log 实现零依赖；Kafka 生产者置于 //go:build kafka 编译标签（见 kafka.go），
// 默认构建不引入重依赖，避免 go.sum 负担。
package events

import (
	"context"

	"opsmesh/internal/logx"
)

// Level 事件级别。
type Level string

const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelAlert Level = "alert"
)

// SchemaVersion 是当前事件契约（信封 JSON 结构）的语义版本。
// 任何字段增删或语义变更都必须 bump 此常量，并同步通知下游消费者
// （Kafka→审计/告警管道），否则跨版本消费者无法识别结构（工程3）。
const SchemaVersion = "1.0.0"

// Event 是内核产出的统一事件（审计/告警共用信封）。
type Event struct {
	TenantID string `json:"tenantID"`
	UserID   string `json:"userID"`
	Action   string `json:"action"` // register / create_task / report_result / alert ...
	Target   string `json:"target"`
	Detail   string `json:"detail"`
	Level    Level  `json:"level"`
	Version  string `json:"version"` // 契约版本，发布时自动加盖（工程3）
}

// stamp 在发布前为事件加盖当前契约版本；若调用方已显式指定则保留。
// 保证所有消费者收到带版本的信封，是跨版本演进的锚点。
func stamp(e Event) Event {
	if e.Version == "" {
		e.Version = SchemaVersion
	}
	return e
}

// Bus 事件总线接口。
type Bus interface {
	Publish(ctx context.Context, e Event) error
}

// NoopBus 丢弃所有事件（默认/测试用）。
type NoopBus struct{}

func (NoopBus) Publish(context.Context, Event) error { return nil }

// LogBus 把事件结构化输出到日志（开发/单机可见，替代 Kafka）。
type LogBus struct{}

func (LogBus) Publish(ctx context.Context, e Event) error {
	e = stamp(e)
	logx.Info(ctx, "event", "action", e.Action, "target", e.Target,
		"tenantID", e.TenantID, "userID", e.UserID, "level", string(e.Level),
		"detail", e.Detail, "version", e.Version)
	return nil
}

// stampingBus 包装任意 Bus，在发布前强制加盖契约版本，保证下游消费者
// （日志/Kafka/审计）都能拿到带版本信封（工程3）。
type stampingBus struct{ inner Bus }

func (s stampingBus) Publish(ctx context.Context, e Event) error {
	return s.inner.Publish(ctx, stamp(e))
}

// New 按名称构造 Bus：noop | log | kafka。brokers/topic 仅 kafka 分支使用，
// 经参数传递（替代 os.Setenv，避免并发不安全，P1-5）。返回的 Bus 统一经
// stampingBus 包装，确保发布时自动加盖契约版本（工程3）。
func New(kind, brokers, topic string) Bus {
	var inner Bus
	switch kind {
	case "log":
		inner = LogBus{}
	case "kafka":
		if b := newKafkaBus(brokers, topic); b != nil {
			inner = b
		} else {
			inner = NoopBus{} // 未配置 brokers/topic 时降级
		}
	default: // "" / "noop" / 未知
		inner = NoopBus{}
	}
	return stampingBus{inner: inner}
}

// newKafkaBus 声明移至 kafka_stub.go（//go:build !kafka）与 kafka.go（//go:build kafka），
// 两者互斥避免 -tags kafka 下重复声明（原于此处的事件.go 默认声明与 kafka.go 冲突）。
