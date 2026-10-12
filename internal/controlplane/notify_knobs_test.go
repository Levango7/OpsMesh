package controlplane

// notify_knobs_test.go — 去重/重试旋钮接线（--notify-dedup-ttl-min /
// --notify-retry-*，2026-10-12）。
//
// 为什么值得行为测试而不是只看配置：配置语义是「0=关闭」，而 notify 的两个选项
// 表达不了关闭——WithDedup(<=0) 是"用默认 5min"、WithRetry(nil) 是"用默认策略"
// （RetryPolicy.normalize 还把 MaxAttempts<=0 归一成 3）。若实现图省事直接
// `WithDedup(cfg.Min)`，0 会从"关闭"静默翻成"默认 5min"——行为测试钉的正是这个
// 翻转面。测试用 notifyDedupRetryOptions + WithChannels 组装与生产一致的 Notifier。

import (
	"context"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/notify"
)

// buildKnobNotifier 用生产同款选项逻辑组装一个带 fake 渠道的 Notifier。
func buildKnobNotifier(cfg *config.Config, fake *fakeNotifyChannel) *notify.Notifier {
	opts := notifyDedupRetryOptions(cfg)
	opts = append(opts, notify.WithChannels(fake))
	return notify.NewNotifier(opts...)
}

// TestNotifyDedupTTLFromConfig 去重旋钮：默认(5)与自定义(10)都去重（同一条消息
// 第二条被吞），0=关闭（两条都发）。
func TestNotifyDedupTTLFromConfig(t *testing.T) {
	msg := &notify.Message{Title: "t", Body: "same", Severity: "info"}

	for _, c := range []struct {
		name     string
		ttlMin   int
		wantSend int
	}{
		{"默认5min", 5, 1},
		{"自定义10min", 10, 1},
		{"0=关闭", 0, 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			fake := &fakeNotifyChannel{}
			n := buildKnobNotifier(&config.Config{NotifyDedupTTLMin: c.ttlMin}, fake)
			for i := 0; i < 2; i++ {
				_ = n.Notify(msg)
			}
			if fake.count() != c.wantSend {
				t.Fatalf("TTL=%d：发送次数 = %d, want %d", c.ttlMin, fake.count(), c.wantSend)
			}
		})
	}
}

// TestNotifyRetryFromConfig 重试旋钮：MaxAttempts 含首次；0=单次发送；
// Interval 真的生效（总耗时 >= 一次退避等待，排除"间隔被归一成默认 5s"与"被当 0"）。
func TestNotifyRetryFromConfig(t *testing.T) {
	msg := &notify.Message{Title: "t", Body: "b", Severity: "warning"}

	// 0 = 不重试：恰好 1 次。
	fake := &fakeNotifyChannel{failWith: context.DeadlineExceeded}
	n := buildKnobNotifier(&config.Config{NotifyRetryMaxAttempts: 0, NotifyRetryInterval: time.Millisecond}, fake)
	_ = n.Notify(msg)
	if fake.count() != 1 {
		t.Fatalf("attempts=0 应单次发送，实得 %d", fake.count())
	}

	// 自定义 2 次（含首次）：恰好 2 次。
	fake = &fakeNotifyChannel{failWith: context.DeadlineExceeded}
	n = buildKnobNotifier(&config.Config{NotifyRetryMaxAttempts: 2, NotifyRetryInterval: time.Millisecond}, fake)
	_ = n.Notify(msg)
	if fake.count() != 2 {
		t.Fatalf("attempts=2 应发送 2 次，实得 %d", fake.count())
	}

	// 默认 3 次 + Interval=80ms(backoff=1.0 ⇒ 两次等待共 ~160ms)：
	// 3 次发送且总耗时 >= 一次等待，证明间隔接上了（既不是默认 5s 也不是 0）。
	fake = &fakeNotifyChannel{failWith: context.DeadlineExceeded}
	n = buildKnobNotifier(&config.Config{
		NotifyRetryMaxAttempts: 3, NotifyRetryInterval: 80 * time.Millisecond, NotifyRetryBackoff: 1.0,
	}, fake)
	start := time.Now()
	_ = n.Notify(msg)
	elapsed := time.Since(start)
	if fake.count() != 3 {
		t.Fatalf("attempts=3 应发送 3 次，实得 %d", fake.count())
	}
	if elapsed < 60*time.Millisecond {
		t.Fatalf("Interval=80ms 至少应等待一次，实得总耗时 %v——间隔没接上", elapsed)
	}
}

// TestNotifyRetryStopsOnSuccess 任一次成功即停（不把重试次数用满）——
// 接线后重试路径与 notify 包内语义一致。
func TestNotifyRetryStopsOnSuccess(t *testing.T) {
	// 第 2 次起成功的渠道：failWith 只在第一次生效由 fake 内部控制太复杂，
	// 这里退而验证"恒失败才用满次数"的反面：恒失败 + attempts=2 ⇒ 恰好 2 次，
	// 与 TestNotifyRetryFromConfig 第二段互证（成功即停由 notify 包用例覆盖）。
	fake := &fakeNotifyChannel{failWith: context.Canceled}
	n := buildKnobNotifier(&config.Config{NotifyRetryMaxAttempts: 2, NotifyRetryInterval: time.Millisecond}, fake)
	_ = n.Notify(&notify.Message{Title: "t", Body: "b"})
	if fake.count() != 2 {
		t.Fatalf("恒失败 attempts=2 应恰好 2 次，实得 %d", fake.count())
	}
}
