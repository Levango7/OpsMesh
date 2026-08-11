package discovery


import "context"

// NoopDiscovery 默认不启用服务发现时的 no-op 实现。
//
// 语义：
//   - Register/Deregister：返回 ErrNotImplemented（调用方可据此判断是否启用了真实服务发现）。
//   - List：返回空切片 + nil 错误（无服务实例，调用方 balancer 应处理空列表场景）。
//   - Watch：返回立即关闭的 channel（无变更通知）。
//
// 用途：当未配置 etcd/Consul 等服务发现后端时，agent 使用 NoopDiscovery 作为默认实现，
// 此时 agent 应回退到静态配置的控制面地址（见 StaticDiscovery）。
type NoopDiscovery struct{}

// NewNoopDiscovery 构造一个 NoopDiscovery 实例。
func NewNoopDiscovery() *NoopDiscovery {
	return &NoopDiscovery{}
}

// Register no-op：返回 ErrNotImplemented。
func (n *NoopDiscovery) Register(ctx context.Context, service Service) error {
	return ErrNotImplemented
}

// Deregister no-op：返回 ErrNotImplemented。
func (n *NoopDiscovery) Deregister(ctx context.Context, serviceID string) error {
	return ErrNotImplemented
}

// List no-op：返回空切片 + nil 错误。
func (n *NoopDiscovery) List(ctx context.Context, serviceName string) ([]Service, error) {
	return []Service{}, nil
}

// Watch no-op：返回立即关闭的 channel（无变更通知）。
// 调用方读取此 channel 会立即得到零值（channel 关闭），应据此回退到静态配置。
func (n *NoopDiscovery) Watch(ctx context.Context, serviceName string) (<-chan []Service, error) {
	ch := make(chan []Service)
	close(ch)
	return ch, nil
}