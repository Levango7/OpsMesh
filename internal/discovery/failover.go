package discovery

import (
	"context"
	"sync"
)

// Failover 主备切换负载均衡器。
//
// 语义：优先用第一个健康实例（主），只有主不可用时才切换到下一个（备）。
// 与 RoundRobin 的区别：Failover 不会主动轮询，只在失败时才切换，适合"一主多备"场景。
//
// 单实例场景（向后兼容）：当 instances 只有一个实例时，Failover 退化为始终返回该实例，
// 与现有单控制面行为完全一致（不破坏现有行为）。
//
// 线程安全：instances 与 current 由 mu 保护（Update 与 Next 并发安全）。
type Failover struct {
	mu        sync.RWMutex
	instances []Service
	current   int // 当前使用的实例索引（0=主，1=备1，...）
}

// NewFailover 构造一个 Failover 负载均衡器。
// instances 为初始实例列表（应来自 ServiceDiscovery.List）。
// 自动过滤不健康实例。current 初始为 0（优先用主）。
func NewFailover(instances []Service) *Failover {
	f := &Failover{}
	f.Update(instances)
	return f
}

// Update 更新实例列表（全量替换）。
// 重置 current 到 0（优先用主）。
// 自动过滤不健康实例。
func (f *Failover) Update(instances []Service) {
	healthy := make([]Service, 0, len(instances))
	for _, s := range instances {
		if s.Healthy {
			healthy = append(healthy, s)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.instances = healthy
	f.current = 0
}

// Next 返回当前实例（主）。
//
// 注意：Failover 语义下，Next 始终返回 current 索引的实例（主），
// 只有调用方显式调用 Failover.MarkFailed 标记当前实例失败后，Next 才会切换到下一个实例。
// 这与 RoundRobin 的"每次 Next 都轮询"语义不同。
//
// 无可用实例时返回 ErrNoInstances。
func (f *Failover) Next(ctx context.Context) (Service, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.instances) == 0 {
		return Service{}, ErrNoInstances
	}
	// current 可能因 Update 缩短实例列表而越界，重置到 0（优先用主）。
	if f.current >= len(f.instances) {
		f.current = 0
	}
	return f.instances[f.current], nil
}

// MarkFailed 标记当前实例失败，切换到下一个实例（备）。
//
// 调用方在 gRPC 连接失败时调用此方法，触发主→备切换。
// 如果没有备可用（已是最后一个实例），current 重置为 0（回到主，下次 Next 仍尝试主）。
// 返回切换后的实例（备）；无备可用时返回 ErrNoInstances。
func (f *Failover) MarkFailed() (Service, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.instances) == 0 {
		return Service{}, ErrNoInstances
	}
	f.current++
	if f.current >= len(f.instances) {
		// 已是最后一个实例，无备可用，重置到主（让调用方下次 Next 仍尝试主，
		// 配合 gRPC 内置 Backoff 重连，避免永久卡在备）。
		f.current = 0
	}
	return f.instances[f.current], nil
}

// Current 返回当前实例索引（用于调试/监控）。
func (f *Failover) Current() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.current
}

// Len 返回实例总数（用于调试/监控）。
func (f *Failover) Len() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.instances)
}
