//go:build !kafka

// kafka_stub.go 提供默认构建（无 kafka 标签）下的 newKafkaBus stub。
//
// 设计：events.go 的 New(kind,...) 在 "kafka" 分支调用 newKafkaBus；
// 默认构建不含 kafka.go（//go:build kafka），需此 stub 返回 nil 使 New 降级为 NoopBus。
// kafka 标签构建时此文件被排除（!kafka），由 kafka.go 提供真实实现，避免重复声明。
//
// 此文件替代 events.go 末尾的 newKafkaBus 声明（原声明在 -tags kafka 下与 kafka.go 冲突）。
package events

// newKafkaBus 默认构建下返回 nil；kafka.go（//go:build kafka）提供真实实现（接收 brokers/topic 参数）。
func newKafkaBus(brokers, topic string) Bus { return nil }

// KafkaAvailable 报告当前二进制是否编译进真实 Kafka 生产者（-tags kafka）。
// 供启动期 config.Validate 做 fail-fast：配置 --event-bus=kafka 但二进制未带此标签时，
// New 会静默降级为 NoopBus（审计/告警事件被丢弃），必须提前拒绝而非运行中丢数据。
func KafkaAvailable() bool { return false }
