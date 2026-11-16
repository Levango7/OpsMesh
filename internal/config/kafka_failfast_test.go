//go:build !kafka

// kafka_failfast_test.go 钉住「配置 --event-bus=kafka 但二进制未编译 kafka 生产者」的
// 启动期 fail-fast：默认构建下 events.KafkaAvailable()==false，
// config.Validate() 必须拒绝该配置，而不是让它静默降级为 NoopBus 丢审计/告警事件。
// kafka 标签构建时本文件被排除（kafka.go 提供 KafkaAvailable()==true，配置校验放行）。
package config

import (
	"strings"
	"testing"
)

func TestValidate_KafkaNotCompiledFailFast(t *testing.T) {
	c := base()
	c.EventBus = "kafka"
	c.KafkaBrokers = "broker1:9092"
	c.KafkaTopic = "opsmesh-audit"
	err := c.Validate()
	if err == nil {
		t.Fatal("默认构建下 --event-bus=kafka 必须被 Validate 拒绝（否则静默 NoopBus 丢事件）")
	}
	if !strings.Contains(err.Error(), "-tags kafka") {
		t.Fatalf("错误信息应提示 -tags kafka，实际: %v", err)
	}
}

func TestValidate_KafkaNotConfiguredOK(t *testing.T) {
	// 未配置 kafka（noop/log）时不应被新门禁误伤。
	for _, kind := range []string{"", "noop", "log"} {
		c := base()
		c.EventBus = kind
		if err := c.Validate(); err != nil {
			t.Fatalf("EventBus=%q 不应被拒绝: %v", kind, err)
		}
	}
}
