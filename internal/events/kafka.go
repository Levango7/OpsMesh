//go:build kafka

// Package events 的 Kafka 生产者实现（仅 -tags kafka 构建包含）。
// 依赖 github.com/segmentio/kafka-go；默认构建不含此文件，故 go.mod 也无需声明该依赖。
// 启用方式：
//   go get github.com/segmentio/kafka-go
//   go build -tags kafka ./...
//   ./opsmesh --event-bus=kafka --kafka-brokers=broker1:9092,broker2:9092 --kafka-topic=opsmesh-audit
package events

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/segmentio/kafka-go"
)

// newKafkaBus 由调用方传入 brokers/topic（参数传递，避免 os.Setenv 竞态，P1-5）；
// 未配置时返回 nil（调用方回退 noop）。
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

type kafkaBus struct{ w *kafka.Writer }

func (k *kafkaBus) Publish(ctx context.Context, e Event) error {
	e = stamp(e)
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return k.w.WriteMessages(ctx, kafka.Message{Value: b})
}
