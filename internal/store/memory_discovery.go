package store

import (
	"time"
)

// memory_discovery.go MemoryStore 对 ServiceDiscoveryStore 接口的实现。
// 服务发现领域：注册/反注册/实例查询/心跳/过期清理。
// 复用 MemoryStore.mu（sync.RWMutex）保护并发安全；按 serviceID 索引实例。
//
// 设计要点：
//   - RegisterService 按 ServiceID 幂等 upsert；新建时填充 CreatedAt/LastHeartbeat。
//   - HeartbeatService 仅刷新已知实例的 LastHeartbeat/Status，未知返回 false。
//   - StaleServices 线性扫描，按 LastHeartbeat 早于 now-maxAge 过滤；tenantID 空串=全部租户。
//   - 读路径（ServiceInstances/AllServices/StaleServices）与 RegisterService 返回值均为
//     深拷贝副本，外部修改不影响内部状态（参照 cloneAPIKey 模式）。

// cloneServiceInstance 返回 inst 的深拷贝（含 Metadata map）。
// nil 返回 nil。用于读路径返回副本、写路径入 map 前拷贝。
func cloneServiceInstance(inst *ServiceInstance) *ServiceInstance {
	if inst == nil {
		return nil
	}
	cp := *inst
	if inst.Metadata != nil {
		cp.Metadata = make(map[string]string, len(inst.Metadata))
		for k, v := range inst.Metadata {
			cp.Metadata[k] = v
		}
	}
	return &cp
}

// cloneServiceInstances 返回 insts 中每个元素的深拷贝切片。
func cloneServiceInstances(insts []*ServiceInstance) []*ServiceInstance {
	if len(insts) == 0 {
		return nil
	}
	out := make([]*ServiceInstance, len(insts))
	for i, inst := range insts {
		out[i] = cloneServiceInstance(inst)
	}
	return out
}

// RegisterService 注册一个服务实例（按 ServiceID 幂等 upsert）。
// 已存在则更新可变字段（Address/Port/Metadata/Status/LastHeartbeat/TenantID），
// 不存在则新建并填充 CreatedAt（若调用方未提供）。返回更新后的实例深拷贝副本。
func (m *MemoryStore) RegisterService(inst *ServiceInstance) *ServiceInstance {
	if inst == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	existing, ok := m.services[inst.ServiceID]
	if !ok {
		// 先填充默认值（保持对外部 inst 的兼容修改），再拷贝入 map 隔离后续修改。
		if inst.CreatedAt.IsZero() {
			inst.CreatedAt = now
		}
		if inst.LastHeartbeat.IsZero() {
			inst.LastHeartbeat = now
		}
		if inst.Status == "" {
			inst.Status = "healthy"
		}
		cp := cloneServiceInstance(inst)
		m.services[inst.ServiceID] = cp
		return cloneServiceInstance(cp)
	}
	// 幂等 upsert：保留 CreatedAt，更新其余字段。
	existing.ServiceName = inst.ServiceName
	existing.Address = inst.Address
	existing.Port = inst.Port
	// Metadata 深拷贝，隔离外部 map 修改。
	if inst.Metadata != nil {
		existing.Metadata = make(map[string]string, len(inst.Metadata))
		for k, v := range inst.Metadata {
			existing.Metadata[k] = v
		}
	} else {
		existing.Metadata = nil
	}
	existing.Status = inst.Status
	existing.TenantID = inst.TenantID
	if !inst.LastHeartbeat.IsZero() {
		existing.LastHeartbeat = inst.LastHeartbeat
	}
	return cloneServiceInstance(existing)
}

// DeregisterService 反注册服务实例。返回是否删除成功。
func (m *MemoryStore) DeregisterService(tenantID, serviceID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	inst, ok := m.services[serviceID]
	if !ok {
		return false
	}
	// 租户隔离校验：tenantID 空串=管理操作，放行；否则必须匹配。
	if tenantID != "" && inst.TenantID != tenantID {
		return false
	}
	delete(m.services, serviceID)
	return true
}

// ServiceInstances 返回指定服务名下的全部实例（按 tenantID 隔离）。
// tenantID 空串=全部租户。结果按 ServiceID 升序稳定输出。
// 返回深拷贝副本，外部修改不影响内部状态。
func (m *MemoryStore) ServiceInstances(tenantID, serviceName string) []*ServiceInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*ServiceInstance
	for _, inst := range m.services {
		if inst.ServiceName != serviceName {
			continue
		}
		if tenantID != "" && inst.TenantID != tenantID {
			continue
		}
		out = append(out, cloneServiceInstance(inst))
	}
	sortServiceInstances(out)
	return out
}

// AllServices 返回全部服务实例（按 tenantID 隔离；空串=全部租户）。
// 返回深拷贝副本，外部修改不影响内部状态。
func (m *MemoryStore) AllServices(tenantID string) []*ServiceInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*ServiceInstance
	for _, inst := range m.services {
		if tenantID != "" && inst.TenantID != tenantID {
			continue
		}
		out = append(out, cloneServiceInstance(inst))
	}
	sortServiceInstances(out)
	return out
}

// HeartbeatService 服务实例心跳：刷新 LastHeartbeat 与 Status。返回是否已知该实例。
func (m *MemoryStore) HeartbeatService(tenantID, serviceID, status string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	inst, ok := m.services[serviceID]
	if !ok {
		return false
	}
	if tenantID != "" && inst.TenantID != tenantID {
		return false
	}
	inst.LastHeartbeat = time.Now()
	if status != "" {
		inst.Status = status
	}
	return true
}

// StaleServices 返回最后心跳早于 maxAge 的不健康实例（按 tenantID 隔离）。
// LastHeartbeat 早于 now-maxAge 视为过期；maxAge<=0 时不返回任何实例。
// 返回深拷贝副本，外部修改不影响内部状态。
func (m *MemoryStore) StaleServices(tenantID string, maxAge time.Duration) []*ServiceInstance {
	if maxAge <= 0 {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	threshold := time.Now().Add(-maxAge)
	var out []*ServiceInstance
	for _, inst := range m.services {
		// 心跳在 threshold 之后=健康，跳过；早于 threshold=过期。
		if !inst.LastHeartbeat.Before(threshold) {
			continue
		}
		if tenantID != "" && inst.TenantID != tenantID {
			continue
		}
		out = append(out, cloneServiceInstance(inst))
	}
	sortServiceInstances(out)
	return out
}

// sortServiceInstances 按 ServiceID 升序排序（稳定输出，便于测试断言）。
// 小规模数据用插入排序，避免引入 sort 包的额外依赖。
func sortServiceInstances(s []*ServiceInstance) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1].ServiceID > s[j].ServiceID; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
