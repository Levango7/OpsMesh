package discovery

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// StaticDiscovery 静态配置的服务发现实现（M1-3）。
//
// 语义：
//   - 从配置加载一组固定的服务实例（如多控制面地址），无注册中心依赖。
//   - Register/Deregister：维护内存中的实例列表（允许动态增删，但不持久化）。
//   - List：返回当前内存中指定服务名的健康实例。
//   - Watch：周期性推送当前实例列表（默认 30s 一次，便于 balancer 拿到最新列表）。
//
// 用途：MVP 默认实现，agent 通过 --controlplane-endpoints 配置多控制面地址，
// 无需部署 etcd/Consul 即可实现多控制面 failover。生产环境可切换到 etcd/Consul 实现。
//
// 线程安全：services map 由 mu 保护，Register/Deregister/List 并发安全。
type StaticDiscovery struct {
	mu       sync.RWMutex
	services map[string]map[string]Service // serviceName -> serviceID -> Service
}

// NewStaticDiscovery 构造一个空的 StaticDiscovery 实例。
// 调用方通过 Register 添加服务实例，或用 NewStaticDiscoveryFromAddrs 从地址列表构造。
func NewStaticDiscovery() *StaticDiscovery {
	return &StaticDiscovery{
		services: make(map[string]map[string]Service),
	}
}

// NewStaticDiscoveryFromAddrs 从逗号分隔的地址列表构造 StaticDiscovery。
//
// 地址格式：
//   - host:port（如 cp1:9090）→ Service{ID: "cp1:9090", Name: serviceName, Addr: "cp1", Port: 9090, Healthy: true}
//   - http://host:port（如 http://cp1:8080）→ 同上，剥离 scheme
//   - 纯 host（如 cp1）→ Service{ID: "cp1", Name: serviceName, Addr: "cp1", Port: 0, Healthy: true}
//
// serviceName 为服务名（如 "opsmesh-controlplane"），所有地址注册到同一服务名下。
// 空 addrs 返回空 StaticDiscovery（不报错，调用方 List 时得到空切片）。
func NewStaticDiscoveryFromAddrs(serviceName, addrs string) *StaticDiscovery {
	d := NewStaticDiscovery()
	if addrs == "" {
		return d
	}
	for _, raw := range strings.Split(addrs, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		addr, port := parseAddrPort(raw)
		svc := Service{
			ID:      raw, // 用原始地址作为 ID（保证唯一）
			Name:    serviceName,
			Addr:    addr,
			Port:    port,
			Healthy: true,
		}
		_ = d.Register(context.Background(), svc)
	}
	return d
}

// parseAddrPort 解析地址字符串为 host 与 port。
//
// 规则：
//   - 带 scheme（http://host:port）：剥离 scheme，解析 host:port。
//   - host:port：解析为 host 与 port。
//   - 纯 host：返回 (host, 0)。
//   - 解析失败：原样返回（addr=raw, port=0），由调用方决定如何处理。
func parseAddrPort(raw string) (addr string, port int) {
	s := raw
	if strings.Contains(s, "://") {
		if i := strings.Index(s, "://"); i != -1 {
			s = s[i+3:]
		}
	}
	// 处理 IPv6 [::1]:port 形式
	if strings.HasPrefix(s, "[") {
		if i := strings.LastIndex(s, "]:"); i != -1 {
			return s[:i+1], parsePort(s[i+2:])
		}
		return s, 0
	}
	if i := strings.LastIndex(s, ":"); i != -1 {
		return s[:i], parsePort(s[i+1:])
	}
	return s, 0
}

// parsePort 解析端口字符串，失败返回 0。
func parsePort(s string) int {
	var p int
	if _, err := fmt.Sscanf(s, "%d", &p); err != nil {
		return 0
	}
	return p
}

// Register 注册或更新一个服务实例。
// 同 ID 的实例会被覆盖（更新 Metadata/Healthy 等）。
func (d *StaticDiscovery) Register(ctx context.Context, service Service) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.services[service.Name] == nil {
		d.services[service.Name] = make(map[string]Service)
	}
	d.services[service.Name][service.ID] = service
	return nil
}

// Deregister 注销一个服务实例。
// serviceID 不存在时不报错（幂等）。
func (d *StaticDiscovery) Deregister(ctx context.Context, serviceID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for name, instances := range d.services {
		if _, ok := instances[serviceID]; ok {
			delete(instances, serviceID)
			if len(instances) == 0 {
				delete(d.services, name)
			}
			return nil
		}
	}
	return nil
}

// List 列出指定服务名的所有健康实例。
// 返回顺序按 ID 升序（稳定排序，便于 balancer 决策可预测）。
// 无实例时返回空切片 + nil 错误。
func (d *StaticDiscovery) List(ctx context.Context, serviceName string) ([]Service, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	instances, ok := d.services[serviceName]
	if !ok {
		return []Service{}, nil
	}
	out := make([]Service, 0, len(instances))
	for _, svc := range instances {
		if svc.Healthy {
			out = append(out, svc)
		}
	}
	// 按 ID 升序排序（稳定顺序，便于 balancer 决策可预测）
	sortServicesByID(out)
	return out, nil
}

// Watch 周期性推送指定服务名的实例列表。
//
// 实现：每 DefaultWatchTimeout 推送一次当前列表（全量推送）。
// ctx 取消时关闭 channel 并退出。
// 静态实现中实例列表通常不变，但允许通过 Register/Deregister 动态调整后由 Watch 推送最新列表。
func (d *StaticDiscovery) Watch(ctx context.Context, serviceName string) (<-chan []Service, error) {
	ch := make(chan []Service, 1)
	// 立即推送一次当前列表（让调用方尽快拿到初始列表）
	cur, err := d.List(ctx, serviceName)
	if err != nil {
		close(ch)
		return ch, err
	}
	ch <- cur
	go func() {
		defer close(ch)
		ticker := time.NewTicker(DefaultWatchTimeout)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				latest, err := d.List(ctx, serviceName)
				if err != nil {
					return
				}
				select {
				case ch <- latest:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return ch, nil
}

// sortServicesByID 按 Service.ID 升序排序（插入排序，实例数通常 < 10）。
func sortServicesByID(s []Service) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1].ID > s[j].ID; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
