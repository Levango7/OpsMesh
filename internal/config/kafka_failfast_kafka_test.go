//go:build kafka

// kafka_failfast_test.go（-tags kafka 版本）钉住「Kafka 生产者已编译时不误拦」：
// kafka.go 的 KafkaAvailable()==true ⇒ config.Validate() 放行 --event-bus=kafka。
// 真实事件投递（broker 不可达时 Publish 落 WAL）由 kafka_test.go 覆盖。
package config

import "testing"

func TestValidate_KafkaCompiledOK(t *testing.T) {
	c := base()
	c.EventBus = "kafka"
	c.KafkaBrokers = "broker1:9092"
	c.KafkaTopic = "opsmesh-audit"
	if err := c.Validate(); err != nil {
		t.Fatalf("-tags kafka 构建下 --event-bus=kafka 应放行: %v", err)
	}
}
