
// Package notify 通知重试（M2-2）。
//
// 本文件实现通知重试策略：发送失败时按指数退避重试，达到最大次数后放弃并返回最后一次错误。
// 重试间隔 = Interval * Backoff^(attempt-1)（attempt 从 1 开始）。
package notify

import (
	"context"
	"fmt"
	"time"
)

// ============================================================================
// 重试策略
// ============================================================================

// RetryPolicy 重试策略。
//
// 字段语义：
//   - MaxAttempts：最大尝试次数（含首次发送；默认 3，即首次 + 2 次重试）。
//   - Interval：基础重试间隔（默认 5s）；实际间隔 = Interval * Backoff^(attempt-1)。
//   - Backoff：退避系数（默认 2.0）；每次重试间隔乘以此系数。1.0=固定间隔，2.0=指数退避。
//
// 特殊值：
//   - MaxAttempts <= 0：使用默认 3。
//   - Interval <= 0：使用默认 5s。
//   - Backoff <= 0：使用默认 2.0。
type RetryPolicy struct {
	MaxAttempts int           // 最大尝试次数（默认 3）
	Interval    time.Duration // 基础重试间隔（默认 5s）
	Backoff     float64       // 退避系数（默认 2.0）
}

// DefaultRetryPolicy 默认重试策略：3 次尝试，5s 基础间隔，2.0 指数退避。
var DefaultRetryPolicy = RetryPolicy{
	MaxAttempts: 3,
	Interval:    5 * time.Second,
	Backoff:     2.0,
}

// normalize 填充零值字段为默认值，返回规范化后的副本。
func (p RetryPolicy) normalize() RetryPolicy {
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = 3
	}
	if p.Interval <= 0 {
		p.Interval = 5 * time.Second
	}
	if p.Backoff <= 0 {
		p.Backoff = 2.0
	}
	return p
}

// backoffDuration 计算第 attempt 次重试（attempt 从 1 开始）前的等待时长。
// 第 1 次重试前等待 Interval；第 2 次等待 Interval*Backoff；第 3 次等待 Interval*Backoff^2。
func (p RetryPolicy) backoffDuration(attempt int) time.Duration {
	if attempt <= 1 {
		return p.Interval
	}
	// Interval * Backoff^(attempt-1)
	multiplier := 1.0
	for i := 1; i < attempt; i++ {
		multiplier *= p.Backoff
	}
	return time.Duration(float64(p.Interval) * multiplier)
}

// SendWithRetry 按重试策略发送消息。
//
// 行为：
//   - 首次发送失败 → 等待 Interval 后重试
//   - 第 n 次重试失败 → 等待 Interval*Backoff^(n-1) 后再重试
//   - 达到 MaxAttempts 次后仍失败 → 返回最后一次 error（包裹重试次数信息）
//   - 任一次发送成功 → 立即返回 nil
//   - policy 为 nil → 使用 DefaultRetryPolicy
//   - channel 为 nil 或 msg 为 nil → 静默返回 nil
//
// ctx 用于取消重试循环（如优雅退出）；ctx 取消时返回 ctx.Err()。
func SendWithRetry(channel Channel, msg *Message, policy *RetryPolicy) error {
	if channel == nil || msg == nil {
		return nil
	}
	return SendWithRetryContext(context.Background(), channel, msg, policy)
}

// SendWithRetryContext 带 context 的重试发送。ctx 取消时立即返回 ctx.Err()。
func SendWithRetryContext(ctx context.Context, channel Channel, msg *Message, policy *RetryPolicy) error {
	if channel == nil || msg == nil {
		return nil
	}
	p := DefaultRetryPolicy
	if policy != nil {
		p = *policy
	}
	p = p.normalize()

	var lastErr error
	for attempt := 1; attempt <= p.MaxAttempts; attempt++ {
		// 检查 ctx 是否已取消（重试前）。
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("notify: retry cancelled before attempt %d: %w", attempt, err)
		}
		// 首次发送前不等待；后续重试前按退避策略等待。
		if attempt > 1 {
			wait := p.backoffDuration(attempt - 1)
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return fmt.Errorf("notify: retry cancelled during backoff before attempt %d: %w", attempt, ctx.Err())
			}
		}
		// 尝试发送。
		lastErr = channel.Send(msg)
		if lastErr == nil {
			return nil // 成功
		}
	}
	// 所有尝试均失败。
	return fmt.Errorf("notify: send failed after %d attempts: %w", p.MaxAttempts, lastErr)
}