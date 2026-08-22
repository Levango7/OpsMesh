package discovery

import (
	"context"
	"errors"
)

// Balancer 负载均衡器接口。
//
// 实现需保证并发安全：agent 多 goroutine（heartbeat/dispatch/cancel）会并发调用 Next。
// Next 返回的 Service 由调用方据此发起 gRPC 连接；连接失败时调用方应再次调用 Next 获取下一个实例。
//
// 实现策略：
//   - RoundRobin：轮询，均匀分配负载到各实例。
//   - Failover：主→备切换，优先用第一个健康实例，失败时切换到下一个。
type Balancer interface {
	// Next 选择下一个服务实例。
	// 返回的实例保证 Healthy=true（不健康实例应被跳过）。
	// 无可用实例时返回 ErrNoInstances。
	Next(ctx context.Context) (Service, error)
}

// ErrBalancerClosed 负载均衡器已关闭错误。
// 调用方在 balancer 关闭后调用 Next 时返回此错误。
var ErrBalancerClosed = errors.New("负载均衡器已关闭")

// NewBalancer 根据策略名构造负载均衡器。
//
// 策略名：
//   - "round-robin"（或 "roundrobin"、"rr"）：RoundRobin
//   - "failover"（或 "fo"）：Failover
//   - 空或未知：默认 Failover（向后兼容现有单控制面行为）
//
// instances 为初始实例列表（应来自 ServiceDiscovery.List）。
// 调用方应在实例列表变更时调用 Update 更新 balancer 的实例列表。
func NewBalancer(strategy string, instances []Service) Balancer {
	switch strategy {
	case "round-robin", "roundrobin", "rr":
		return NewRoundRobin(instances)
	case "failover", "fo", "":
		return NewFailover(instances)
	default:
		// 未知策略默认 Failover（向后兼容）
		return NewFailover(instances)
	}
}

// UpdatableBalancer 可更新实例列表的 Balancer 扩展接口。
//
// 当 ServiceDiscovery.Watch 推送新实例列表时，调用方应通过 Update 更新 balancer 的实例列表。
// 不是所有 Balancer 实现都支持 Update（如无状态 balancer），调用方应通过类型断言检查。
type UpdatableBalancer interface {
	Balancer
	// Update 更新 balancer 的实例列表。
	// 新列表完全替换旧列表（全量更新，非增量）。
	Update(instances []Service)
}
