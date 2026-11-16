//go:build kafka

package events

// KafkaSupported 报告当前二进制是否编译了 Kafka 支持（-tags kafka），供外部包（如 config）做启动期 fail-fast。
func KafkaSupported() bool { return true }
