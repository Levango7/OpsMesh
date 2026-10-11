// circuit.go 进程内熔断器（TD-66 P2）。
//
// 为什么是"被动熔断"而不是"主动健康探测"：本网关的后端是本栈内的微服务，
// 探测只需要保证**故障时快速失败**（fast-fail），不需要主动摘除（那要引入探测循环
// 与被探测后恢复的两套逻辑）。半开试探天然覆盖"后端恢复"的发现。
//
// 状态机：closed →（连续 threshold 次失败）→ open →（冷却 cooldown 后首个请求）→
// half-open →（成功则 closed / 失败则回到 open 并重新计时）。
//
// 失败的定义：转发后端返回 5xx，或传输层错误（连不上/超时——由 ErrorHandler 侧记录）。
// 4xx 不算失败：那是后端对请求的**业务应答**，不是后端故障。
package extension

import (
	"sync"
	"time"
)

// 熔断默认参数。
const (
	CircuitThreshold = 5                // 进入 open 前允许的连续失败次数
	CircuitCooldown  = 30 * time.Second // open 状态的冷却期（过后进半开试探）
)

// CircuitState 熔断状态（对外可读，供测试/未来统计用）。
type CircuitState string

const (
	CircuitClosed   CircuitState = "closed"    // 正常放行
	CircuitOpen     CircuitState = "open"      // 熔断中，直接 fast-fail
	CircuitHalfOpen CircuitState = "half-open" // 冷却后的试探放行（仅一个）
)

// CircuitBreaker 每路由一个的熔断器实例（挂在 gatewayRouteEntry 上）。
//
// 并发：sync.Mutex 保护状态字段；试探名额用 halfOpenUsed 标记，
// 确保冷却后**只有一个**请求被放出去试探（其余在下次 Allow 时被快速拒绝）。
type CircuitBreaker struct {
	mu           sync.Mutex
	failures     int // 连续失败计数（成功即归零）
	state        CircuitState
	openedAt     time.Time // 进入 open 的时刻（用于计算冷却是否结束）
	halfOpenUsed bool      // 半开试探名额是否已被占用
}

// NewCircuitBreaker 构造一个 closed 状态的熔断器。
func NewCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{state: CircuitClosed}
}

// Allow 请求放行判定：closed/half-open 放行，open 直接拒绝。
// half-open 只放行一个试探请求（其余拒绝），避免后端故障时的试探风暴。
func (c *CircuitBreaker) Allow() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch c.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		if time.Since(c.openedAt) < CircuitCooldown {
			return false
		}
		// 冷却结束 → 半开，放行唯一试探名额。
		c.state = CircuitHalfOpen
		c.halfOpenUsed = true
		return true
	case CircuitHalfOpen:
		// 试探名额已被占用 ⇒ 后续请求仍拦（不放大故障域）。
		return !c.halfOpenUsed
	default:
		return true
	}
}

// RecordSuccess 记录一次成功（关闭状态并清零失败计数）。
func (c *CircuitBreaker) RecordSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures = 0
	c.state = CircuitClosed
	c.halfOpenUsed = false
}

// RecordFailure 记录一次失败。达到阈值即进入 open 并开始冷却计时。
// 半开试探失败 ⇒ 立即回到 open 并重新计时（不等到阈值）。
func (c *CircuitBreaker) RecordFailure() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures++
	if c.state == CircuitHalfOpen || c.failures >= CircuitThreshold {
		c.state = CircuitOpen
		c.openedAt = time.Now()
		c.halfOpenUsed = false
		return
	}
	c.state = CircuitClosed
}

// State 返回当前状态（测试用；生产经 /metrics 观测）。
func (c *CircuitBreaker) State() CircuitState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// OpenedAtForTest 把进入 open 的时刻拨到"冷却已结束"，供半开用例跳过真实 30s 等待。
// 仅测试使用（生产由 time.Since 自然推进）。
func (c *CircuitBreaker) OpenedAtForTest() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.openedAt = time.Now().Add(-CircuitCooldown - time.Second)
}
