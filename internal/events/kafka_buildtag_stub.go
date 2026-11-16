//go:build !kafka

package events

// kafkaSupported 报告当前二进制是否编译了 Kafka 支持（-tags kafka）。
func kafkaSupported() bool { return false }

// KafkaSupported 是 kafkaSupported 的导出形式，供外部包（如 config）调用。
func KafkaSupported() bool { return false }
