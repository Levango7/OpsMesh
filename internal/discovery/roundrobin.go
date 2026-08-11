package discovery

import (
	"context"
	"sync"
	"sync/atomic"
)

// RoundRobin 轮询负载均衡器（M1-3）。
//
// 语义：每次 Next 返回下一个健康实例，均匀分配负载到各实例。
// 当实例列表更新（Update）时，重置计数器到 0（避免越界）。
// 不健康实例被跳过（Next 内部循环查找下一个健康实例）。
//
// 线程安全：instances 与 cursor 由 mu 保护（Update 与 Next 并发安全）。
// cursor 用 atomic.Int64 优化无 Update 时的并发读（避免锁竞争）。
type RoundRobin struct {
	mu        sync.RWMutex
	instances []Service
	cursor    atomic.Int64
}

// NewRoundRobin 构造一个 RoundRobin 负载均衡器。
// instances 为初始实例列表（应来自 ServiceDiscovery.List）。
// 自动过滤不健康实例。
func NewRoundRobin(instances []Service) *RoundRobin {
	r := &RoundRobin{}
	r.Update(instances)
	return r
}

// Update 更新实例列表（全量替换）。
// 重置 cursor 到 0（避免越界，且新列表顺序可能变化）。
// 自动过滤不健康实例。
func (r *RoundRobin) Update(instances []Service) {
	healthy := make([]Service, 0, len(instances))
	for _, s := range instances {
		if s.Healthy {
			healthy = append(healthy, s)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.instances = healthy
	r.cursor.Store(0)
}

// Next 返回下一个健康实例（轮询）。
// 无可用实例时返回 ErrNoInstances。
func (r *RoundRobin) Next(ctx context.Context) (Service, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n := len(r.instances)
	if n == 0 {
		return Service{}, ErrNoInstances
	}
	// 原子递增 cursor 并取模，避免锁竞争（无 Update 时 cursor 单调递增）。
	cur := r.cursor.Add(1) - 1
	idx := int(cur % int64(n))
	// cursor 可能因 Update 重置为 0 后再次递增，取模保证不越界。
	if idx < 0 || idx >= n {
		idx = 0
	}
	return r.instances[idx], nil
}
