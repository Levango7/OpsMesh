// Package discovery InMemoryDiscovery 实现。
package discovery

import (
	"context"
	"sort"
	"strings"
	"sync"
)

// instanceKey 是 InMemoryDiscovery 内部实例的复合键。
type instanceKey struct {
	addr string
	port int
}

// InMemoryDiscovery 基于内存的服务发现后端，支持注册/注销/列表/监听与健康标记。
//
// 语义：
//   - Register：按 (Name, Addr, Port) 幂等 upsert；重复注册更新 Metadata/Healthy 等字段。
//   - Deregister：移除匹配实例；不存在返回 ErrNotRegistered。
//   - List：返回指定服务名的健康实例；无实例返回空切片 + nil。
//   - Watch：返回 channel，注册/注销/健康变更时推送全量列表；ctx 取消时关闭 channel；
//     订阅时立即推送当前快照。
//   - MarkHealthy：更新健康状态并通知 watcher。
//
// 线程安全：所有状态由 mu (sync.RWMutex) 保护；通知 watcher 时使用非阻塞发送避免死锁。
type InMemoryDiscovery struct {
	mu sync.RWMutex

	services map[string]map[instanceKey]Service // serviceName -> key -> Service

	watchers    map[string]map[int]chan []Service // serviceName -> watcherID -> channel
	nextWatcher int
}

// NewInMemoryDiscovery 构造一个空的 InMemoryDiscovery 实例。
func NewInMemoryDiscovery() *InMemoryDiscovery {
	return &InMemoryDiscovery{
		services: make(map[string]map[instanceKey]Service),
		watchers: make(map[string]map[int]chan []Service),
	}
}

// NewInMemoryDiscoveryFromAddrs 从逗号分隔的地址列表构造 InMemoryDiscovery。
// 地址解析规则与 NewStaticDiscoveryFromAddrs 一致（支持 host:port、带 scheme 与纯 host），
// 实例 ID 保留原始地址串（balancer 据此还原控制面地址）。
func NewInMemoryDiscoveryFromAddrs(serviceName, addrs string) *InMemoryDiscovery {
	d := NewInMemoryDiscovery()
	if addrs == "" {
		return d
	}
	for _, raw := range strings.Split(addrs, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		addr, port := parseAddrPort(raw)
		_ = d.Register(context.Background(), Service{
			ID:      raw,
			Name:    serviceName,
			Addr:    addr,
			Port:    port,
			Healthy: true,
		})
	}
	return d
}

// cloneService 返回 svc 的深拷贝（Metadata map 克隆）。
func cloneService(svc Service) Service {
	cp := svc
	if svc.Metadata != nil {
		cp.Metadata = make(map[string]string, len(svc.Metadata))
		for k, v := range svc.Metadata {
			cp.Metadata[k] = v
		}
	}
	return cp
}

func (d *InMemoryDiscovery) instanceKeyOf(svc Service) instanceKey {
	return instanceKey{addr: svc.Addr, port: svc.Port}
}

// snapshotLocked 在持有锁时生成指定服务名的健康实例快照（深拷贝）。
// 调用方必须已持有 d.mu（读或写锁）。
func (d *InMemoryDiscovery) snapshotLocked(name string) []Service {
	instances, ok := d.services[name]
	if !ok {
		return []Service{}
	}
	out := make([]Service, 0, len(instances))
	for _, svc := range instances {
		if svc.Healthy {
			out = append(out, cloneService(svc))
		}
	}
	sortServicesByID(out)
	// 二次稳定排序：ID 相同时按 Addr/Port
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		if out[i].Addr != out[j].Addr {
			return out[i].Addr < out[j].Addr
		}
		return out[i].Port < out[j].Port
	})
	return out
}

// allSnapshotLocked 生成包含全部实例（含不健康）的快照，供通知? 目前 List 只健康。
// 保留未使用；通知使用健康快照与 List 一致。

func (d *InMemoryDiscovery) notifyLocked(name string) (chanList []chan []Service, snap []Service) {
	watchers, ok := d.watchers[name]
	if !ok || len(watchers) == 0 {
		return nil, d.snapshotLocked(name)
	}
	out := make([]chan []Service, 0, len(watchers))
	for _, ch := range watchers {
		out = append(out, ch)
	}
	return out, d.snapshotLocked(name)
}

// Register 注册或更新一个服务实例（按 Name+Addr+Port 幂等）。
func (d *InMemoryDiscovery) Register(ctx context.Context, svc Service) error {
	d.mu.Lock()
	if d.services[svc.Name] == nil {
		d.services[svc.Name] = make(map[instanceKey]Service)
	}
	k := d.instanceKeyOf(svc)
	d.services[svc.Name][k] = cloneService(svc)
	chans, snap := d.notifyLocked(svc.Name)
	d.mu.Unlock()

	for _, ch := range chans {
		func(ch chan []Service) {
			defer func() {
				if r := recover(); r != nil {
					_ = r
				}
			}()
			select {
			case ch <- snap:
			default:
			}
		}(ch)
	}
	return nil
}

// Deregister 移除匹配实例；不存在返回 ErrNotRegistered。
func (d *InMemoryDiscovery) Deregister(ctx context.Context, serviceName, address string, port int) error {
	d.mu.Lock()
	instances, ok := d.services[serviceName]
	if !ok {
		d.mu.Unlock()
		return ErrNotRegistered
	}
	k := instanceKey{addr: address, port: port}
	if _, ok := instances[k]; !ok {
		d.mu.Unlock()
		return ErrNotRegistered
	}
	delete(instances, k)
	if len(instances) == 0 {
		delete(d.services, serviceName)
	}
	chans, snap := d.notifyLocked(serviceName)
	d.mu.Unlock()

	for _, ch := range chans {
		func(ch chan []Service) {
			defer func() {
				if r := recover(); r != nil {
					_ = r
				}
			}()
			select {
			case ch <- snap:
			default:
			}
		}(ch)
	}
	return nil
}

// List 返回指定服务名的健康实例；无实例返回空切片 + nil。
func (d *InMemoryDiscovery) List(ctx context.Context, serviceName string) ([]Service, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	instances, ok := d.services[serviceName]
	if !ok {
		return []Service{}, nil
	}
	out := make([]Service, 0, len(instances))
	for _, svc := range instances {
		if svc.Healthy {
			out = append(out, cloneService(svc))
		}
	}
	sortServicesByID(out)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		if out[i].Addr != out[j].Addr {
			return out[i].Addr < out[j].Addr
		}
		return out[i].Port < out[j].Port
	})
	if out == nil {
		out = []Service{}
	}
	return out, nil
}

// Watch 返回监听 channel；订阅时推送快照；ctx 取消时关闭 channel。
func (d *InMemoryDiscovery) Watch(ctx context.Context, serviceName string) (<-chan []Service, error) {
	ch := make(chan []Service, 16)

	d.mu.Lock()
	if d.watchers[serviceName] == nil {
		d.watchers[serviceName] = make(map[int]chan []Service)
	}
	id := d.nextWatcher
	d.nextWatcher++
	d.watchers[serviceName][id] = ch
	snap := d.snapshotLocked(serviceName)
	d.mu.Unlock()

	// 订阅快照
	select {
	case ch <- snap:
	case <-ctx.Done():
	}

	// ctx 取消时清理并关闭 channel
	go func() {
		<-ctx.Done()
		d.mu.Lock()
		if watchers, ok := d.watchers[serviceName]; ok {
			delete(watchers, id)
			if len(watchers) == 0 {
				delete(d.watchers, serviceName)
			}
		}
		d.mu.Unlock()
		// 避免重复关闭
		func() {
			defer func() {
				if r := recover(); r != nil {
					_ = r
				}
			}()
			close(ch)
		}()
	}()

	// 若 ctx 已取消，立即清理
	if ctx.Err() != nil {
		d.mu.Lock()
		if watchers, ok := d.watchers[serviceName]; ok {
			delete(watchers, id)
			if len(watchers) == 0 {
				delete(d.watchers, serviceName)
			}
		}
		d.mu.Unlock()
		func() {
			defer func() {
				if r := recover(); r != nil {
					_ = r
				}
			}()
			close(ch)
		}()
	}
	return ch, nil
}

// MarkHealthy 更新健康状态并通知 watcher；不存在返回 ErrNotRegistered。
func (d *InMemoryDiscovery) MarkHealthy(ctx context.Context, serviceName, address string, port int, healthy bool) error {
	d.mu.Lock()
	instances, ok := d.services[serviceName]
	if !ok {
		d.mu.Unlock()
		return ErrNotRegistered
	}
	k := instanceKey{addr: address, port: port}
	svc, ok := instances[k]
	if !ok {
		d.mu.Unlock()
		return ErrNotRegistered
	}
	svc.Healthy = healthy
	instances[k] = cloneService(svc)
	chans, snap := d.notifyLocked(serviceName)
	d.mu.Unlock()

	for _, ch := range chans {
		func(ch chan []Service) {
			defer func() {
				if r := recover(); r != nil {
					_ = r
				}
			}()
			select {
			case ch <- snap:
			default:
			}
		}(ch)
	}
	return nil
}
