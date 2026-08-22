// Package discovery 提供服务注册发现抽象与多种实现。
//
// 设计目标：
//   - 解耦 agent 与控制面地址获取方式：agent 不再硬编码控制面地址，
//     而是通过 ServiceDiscovery 接口动态获取控制面实例列表。
//   - 支持多种服务发现后端：noop（默认，无服务发现）、static（静态配置多控制面）、
//     etcd/Consul（可选，未来扩展）。
//   - 配合 Balancer 接口实现多控制面负载均衡（round-robin/failover）。
//
// 接口契约：
//   - Register/Deregister 用于 agent 自身注册到服务注册中心（如 etcd/Consul）。
//   - List/Watch 用于获取控制面实例列表（agent 据此选择控制面连接）。
//
// 当前实现：
//   - NoopDiscovery：默认不启用服务发现时的 no-op 实现，所有方法返回空/错误。
//   - StaticDiscovery：静态配置的多控制面地址，从配置加载，无注册中心依赖。
//
// 未来扩展（etcd/Consul）只需实现 ServiceDiscovery 接口即可无缝接入。
package discovery

import (
	"context"
	"errors"
	"time"
)

// Service 描述一个服务实例（如一个控制面副本）。
//
// 字段说明：
//   - ID：服务实例唯一标识（如 "cp1"、"cp2" 或 etcd lease key）。
//   - Name：服务名（如 "opsmesh-controlplane"），同名的多个实例构成一个服务集群。
//   - Addr：服务实例地址（host 或 host:port，不含 scheme）。
//   - Port：服务实例端口（与 Addr 配合使用；Addr 已含端口时此字段可忽略）。
//   - Metadata：自定义元数据（如版本、region、weight 等，供 balancer 决策）。
//   - Healthy：实例健康状态（false 时 balancer 应跳过）。
type Service struct {
	ID       string
	Name     string
	Addr     string
	Port     int
	Metadata map[string]string
	Healthy  bool
}

// ServiceDiscovery 服务注册发现抽象接口。
//
// 实现需保证并发安全：agent 多 goroutine（heartbeat/dispatch/cancel）会并发调用 List。
// Watch 返回的 channel 在服务实例变更时推送最新列表，关闭 channel 表示发现后端不可用。
type ServiceDiscovery interface {
	// Register 注册一个服务实例到服务注册中心。
	// 用于 agent 自身注册（如注册到 etcd/Consul 供控制面发现 agent）。
	// 静态/noop 实现为 no-op。
	Register(ctx context.Context, service Service) error

	// Deregister 注销一个已注册的服务实例。
	// serviceID 为 Register 时的 Service.ID。
	Deregister(ctx context.Context, serviceID string) error

	// List 列出指定服务名的所有健康实例。
	// 返回的列表应按稳定顺序排列（便于 balancer 决策可预测）。
	// 无实例时返回空切片 + nil 错误（不视为错误）。
	List(ctx context.Context, serviceName string) ([]Service, error)

	// Watch 监听指定服务名的实例变更。
	// 返回的 channel 在实例列表变更时推送最新列表（全量推送，非增量）。
	// 调用方应持续读取直到 channel 关闭或 ctx 取消。
	// 静态/noop 实现返回立即关闭的 channel（无变更）。
	Watch(ctx context.Context, serviceName string) (<-chan []Service, error)
}

// ErrNotImplemented 未实现的操作错误。
// noop 实现的 Register/Deregister 返回此错误（调用方可据此判断是否启用了真实服务发现）。
var ErrNotImplemented = errors.New("服务发现后端未实现该操作")

// ErrNoInstances 未找到任何服务实例错误。
// List 在服务名不存在或所有实例不健康时返回此错误（区别于"空切片+nil"的"服务存在但无健康实例"语义）。
// 实际上 List 约定无实例时返回空切片+nil，此错误保留给未来需要区分的场景。
var ErrNoInstances = errors.New("未找到服务实例")

// DefaultWatchTimeout Watch channel 的默认内部刷新超时。
// 静态实现用此超时周期性推送当前列表（模拟变更通知，便于 balancer 拿到最新列表）。
const DefaultWatchTimeout = 30 * time.Second
