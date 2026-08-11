// Package circuitbreaker 实现通用熔断器（Circuit Breaker），用于 agent 端任务执行熔断
// 与控制面 API 熔断（限流+降级）。
//
// 状态机：Closed（正常）→ Open（熔断）→ HalfOpen（半开探测）→ Closed。
//   - Closed：正常放行请求；连续失败达 FailureThreshold 次后转为 Open。
//   - Open：熔断中，拒绝所有请求（返回 ErrCircuitOpen）；经过 RecoveryTimeout 后转为 HalfOpen。
//   - HalfOpen：半开探测，允许最多 HalfOpenMaxCalls 个并发探测请求；
//     任一探测失败 → 立即转回 Open（重置计时）；全部探测成功 → 转为 Closed。
//
// 线程安全：所有状态读写经 sync.Mutex 保护，可被多 goroutine 并发调用 Execute。
//
// 可选禁用：FailureThreshold <= 0 时熔断器退化为透传（不计数、不熔断），用于 agent 配置关闭熔断的场景。
package circuitbreaker

import (
	"errors"
	"sync"
	"time"
)

// 熔断器状态常量。
const (
	StateClosed   = "closed"   // 正常放行
	StateOpen     = "open"     // 熔断中，拒绝请求
	StateHalfOpen = "halfopen" // 半开探测，限量放行
)

// ErrCircuitOpen 熔断器处于 Open 状态时 Execute 返回此错误。
// 调用方（如 agent worker）据此跳过任务执行并返回 "circuit breaker open" 错误。
var ErrCircuitOpen = errors.New("circuit breaker open")

// StateChangeCallback 状态变更回调（可选）。
// 在状态机转换时同步调用（已持锁，回调内禁止再次调用 Execute 等需锁方法，避免死锁）。
// from/to 为 StateClosed/StateOpen/StateHalfOpen 常量。
type StateChangeCallback func(name, from, to string)

// Config 熔断器配置。
type Config struct {
	// Name 熔断器实例名（用于日志/指标标识，如 deviceID）。
	Name string
	// FailureThreshold 连续失败多少次后熔断。<=0 表示禁用熔断器（透传）。
	FailureThreshold int
	// RecoveryTimeout 熔断后等待多久才进入 HalfOpen 探测。<=0 时默认 30s。
	RecoveryTimeout time.Duration
	// HalfOpenMaxCalls HalfOpen 状态下允许的最大并发探测调用数。<=0 时默认 1。
	HalfOpenMaxCalls int
	// OnStateChange 状态变更回调（可选，nil=不回调）。
	OnStateChange StateChangeCallback
}

// CircuitBreaker 熔断器实例。
//
// 状态机字段（state/failureCount/openedAt/halfOpenCalls）经 mu 保护，并发安全。
// 禁用模式（FailureThreshold<=0）下 Execute 直接透传 fn，不触碰状态字段。
type CircuitBreaker struct {
	cfg Config

	mu            sync.Mutex
	state         string // 当前状态（StateClosed/StateOpen/StateHalfOpen）
	failureCount  int    // Closed 状态下连续失败计数（成功即清零）
	openedAt      time.Time // 进入 Open 状态的时刻（用于判断是否已过 RecoveryTimeout）
	halfOpenCalls int    // HalfOpen 状态下已发放的探测调用数
	halfOpenSucc  int    // HalfOpen 状态下已成功的探测数（达到 HalfOpenMaxCalls 即转 Closed）
}

// New 构造熔断器实例。
//   - cfg.FailureThreshold <= 0：禁用模式（透传，不熔断）。
//   - cfg.RecoveryTimeout <= 0：默认 30s。
//   - cfg.HalfOpenMaxCalls <= 0：默认 1。
func New(cfg Config) *CircuitBreaker {
	if cfg.RecoveryTimeout <= 0 {
		cfg.RecoveryTimeout = 30 * time.Second
	}
	if cfg.HalfOpenMaxCalls <= 0 {
		cfg.HalfOpenMaxCalls = 1
	}
	return &CircuitBreaker{
		cfg:   cfg,
		state: StateClosed,
	}
}

// Name 返回熔断器实例名（用于日志/指标）。
func (cb *CircuitBreaker) Name() string {
	return cb.cfg.Name
}

// State 返回当前状态（StateClosed/StateOpen/StateHalfOpen）。
// 禁用模式下始终返回 StateClosed。
func (cb *CircuitBreaker) State() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// Enabled 返回熔断器是否启用（FailureThreshold > 0）。
func (cb *CircuitBreaker) Enabled() bool {
	return cb.cfg.FailureThreshold > 0
}

// Execute 通过熔断器执行函数 fn。
//   - 禁用模式（FailureThreshold<=0）：直接透传 fn，不计数不熔断。
//   - Open 状态：返回 ErrCircuitOpen（不调用 fn）；若已过 RecoveryTimeout 则先转 HalfOpen 再放行。
//   - HalfOpen 状态：已发放探测数达 HalfOpenMaxCalls 时返回 ErrCircuitOpen；否则放行并计数。
//   - Closed 状态：放行；fn 返回 nil 视为成功（清零失败计数），非 nil 视为失败（累加，达阈值转 Open）。
//
// 返回值：
//   - fn 的返回 error（fn 被调用时）；
//   - ErrCircuitOpen（熔断中未调用 fn 时）。
func (cb *CircuitBreaker) Execute(fn func() error) error {
	// 禁用模式：透传，不触碰状态字段，零开销。
	if cb.cfg.FailureThreshold <= 0 {
		return fn()
	}

	// 申请调用许可（可能转 HalfOpen），返回 false 表示熔断中拒绝。
	if !cb.beforeCall() {
		return ErrCircuitOpen
	}
	err := fn()
	cb.afterCall(err)
	return err
}

// beforeCall 在调用 fn 前申请许可，返回 true 表示放行、false 表示熔断中拒绝。
// 内部处理 Open→HalfOpen 转换（已过 RecoveryTimeout 时）。
func (cb *CircuitBreaker) beforeCall() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		// 已过 RecoveryTimeout → 转 HalfOpen，放行首个探测调用。
		if time.Since(cb.openedAt) >= cb.cfg.RecoveryTimeout {
			cb.transition(StateHalfOpen)
			cb.halfOpenCalls = 1 // 本次调用占一个探测名额
			cb.halfOpenSucc = 0
			return true
		}
		return false // 仍在熔断窗口内，拒绝
	case StateHalfOpen:
		// 探测名额未满 → 放行并占一个名额；已满 → 拒绝。
		if cb.halfOpenCalls < cb.cfg.HalfOpenMaxCalls {
			cb.halfOpenCalls++
			return true
		}
		return false
	default:
		// 防御性：未知状态视为 Closed 放行。
		return true
	}
}

// afterCall 在 fn 返回后更新状态机。
//   - Closed：成功清零失败计数；失败累加，达 FailureThreshold 转 Open。
//   - HalfOpen：成功累加探测成功数，达 HalfOpenMaxCalls 转 Closed；失败立即转 Open。
//   - Open：不应到达（beforeCall 已拒绝），防御性 no-op。
func (cb *CircuitBreaker) afterCall(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		if err == nil {
			cb.failureCount = 0
		} else {
			cb.failureCount++
			if cb.failureCount >= cb.cfg.FailureThreshold {
				cb.transition(StateOpen)
				cb.openedAt = time.Now()
				cb.failureCount = 0
			}
		}
	case StateHalfOpen:
		if err == nil {
			cb.halfOpenSucc++
			if cb.halfOpenSucc >= cb.cfg.HalfOpenMaxCalls {
				// 全部探测成功，恢复正常。
				cb.transition(StateClosed)
				cb.failureCount = 0
				cb.halfOpenCalls = 0
				cb.halfOpenSucc = 0
			}
		} else {
			// 探测失败，立即重新熔断。
			cb.transition(StateOpen)
			cb.openedAt = time.Now()
			cb.failureCount = 0
			cb.halfOpenCalls = 0
			cb.halfOpenSucc = 0
		}
	case StateOpen:
		// 防御性：beforeCall 在 Open 状态应返回 false，不会到达 afterCall。
	}
}

// transition 切换状态并触发回调（调用方须已持锁）。
func (cb *CircuitBreaker) transition(to string) {
	from := cb.state
	if from == to {
		return
	}
	cb.state = to
	if cb.cfg.OnStateChange != nil {
		cb.cfg.OnStateChange(cb.cfg.Name, from, to)
	}
}

// Reset 强制重置熔断器到 Closed 状态（清零所有计数）。
// 用于运维主动恢复或测试。生产场景一般不调用，让状态机自动恢复。
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.transition(StateClosed)
	cb.failureCount = 0
	cb.halfOpenCalls = 0
	cb.halfOpenSucc = 0
}

// FailureCount 返回当前连续失败计数（仅 Closed 状态有意义，其他状态返回 0）。
// 主要用于测试与运维观测。
func (cb *CircuitBreaker) FailureCount() int {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.failureCount
}

// BreakerSet 熔断器集合，按 key（如 deviceID/tenantID/IP）隔离。
// 典型用法：agent 端按 deviceID 隔离，控制面按 IP/tenant 限流。
// 零值可用（首次 Get 时懒初始化内部 map），但建议用 NewBreakerSet 构造。
type BreakerSet struct {
	mu       sync.Mutex
	breakers map[string]*CircuitBreaker
	// template 构造新熔断器时使用的配置模板（Name 字段会被 key 覆盖）。
	template Config
}

// NewBreakerSet 构造熔断器集合。template 为创建新实例的配置模板，
// template.Name 会被各实例的 key 覆盖。
func NewBreakerSet(template Config) *BreakerSet {
	return &BreakerSet{
		breakers: make(map[string]*CircuitBreaker),
		template: template,
	}
}

// Get 按 key 获取或创建熔断器实例。
// 首次访问 key 时以 template 配置创建（Name 字段设为 key）。
// template.FailureThreshold <= 0 时返回的实例为禁用模式（透传），仍缓存以便统一管理。
func (bs *BreakerSet) Get(key string) *CircuitBreaker {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	if cb, ok := bs.breakers[key]; ok {
		return cb
	}
	cfg := bs.template
	cfg.Name = key
	cb := New(cfg)
	bs.breakers[key] = cb
	return cb
}

// Execute 便捷方法：按 key 获取熔断器并通过其执行 fn。
// 等价于 bs.Get(key).Execute(fn)。
func (bs *BreakerSet) Execute(key string, fn func() error) error {
	return bs.Get(key).Execute(fn)
}

// States 返回所有实例的当前状态快照（key → state）。
// 用于运维观测/指标导出。返回副本，调用后修改不影响内部状态。
func (bs *BreakerSet) States() map[string]string {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	out := make(map[string]string, len(bs.breakers))
	for k, cb := range bs.breakers {
		out[k] = cb.State()
	}
	return out
}

// Len 返回集合中熔断器实例数量。
func (bs *BreakerSet) Len() int {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	return len(bs.breakers)
}
