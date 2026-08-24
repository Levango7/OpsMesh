package store

import (
	"time"
)

// sql_discovery.go SQLStore 对 ServiceDiscoveryStore 接口的桩实现。
//
// TODO(p0.3): 接入 MySQL 持久化（services 表：service_id PK + tenant_id + service_name +
// address + port + metadata JSON + status + last_heartbeat + created_at）。
// MVP 用内存 map 做缓存，保证接口齐全 + go build 通过；多副本间数据不共享，
// 生产环境须落库后由各副本从 MySQL 读取。
//
// 与 MemoryStore 实现逻辑等价，仅锁类型不同（SQLStore.mu 为 sync.Mutex）。

// RegisterService 注册一个服务实例（按 ServiceID 幂等 upsert）。
// TODO(p0.3): 落库 services 表（INSERT ... ON DUPLICATE KEY UPDATE）。
func (s *SQLStore) RegisterService(inst *ServiceInstance) *ServiceInstance {
	if inst == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	existing, ok := s.services[inst.ServiceID]
	if !ok {
		if inst.CreatedAt.IsZero() {
			inst.CreatedAt = now
		}
		if inst.LastHeartbeat.IsZero() {
			inst.LastHeartbeat = now
		}
		if inst.Status == "" {
			inst.Status = "healthy"
		}
		s.services[inst.ServiceID] = inst
		return inst
	}
	existing.ServiceName = inst.ServiceName
	existing.Address = inst.Address
	existing.Port = inst.Port
	existing.Metadata = inst.Metadata
	existing.Status = inst.Status
	existing.TenantID = inst.TenantID
	if !inst.LastHeartbeat.IsZero() {
		existing.LastHeartbeat = inst.LastHeartbeat
	}
	return existing
}

// DeregisterService 反注册服务实例。
// TODO(p0.3): DELETE FROM services WHERE service_id=? AND tenant_id=?。
func (s *SQLStore) DeregisterService(tenantID, serviceID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.services[serviceID]
	if !ok {
		return false
	}
	if tenantID != "" && inst.TenantID != tenantID {
		return false
	}
	delete(s.services, serviceID)
	return true
}

// ServiceInstances 返回指定服务名下的全部实例。
// TODO(p0.3): SELECT * FROM services WHERE tenant_id=? AND service_name=? ORDER BY service_id。
func (s *SQLStore) ServiceInstances(tenantID, serviceName string) []*ServiceInstance {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*ServiceInstance
	for _, inst := range s.services {
		if inst.ServiceName != serviceName {
			continue
		}
		if tenantID != "" && inst.TenantID != tenantID {
			continue
		}
		out = append(out, inst)
	}
	sortServiceInstances(out)
	return out
}

// AllServices 返回全部服务实例。
// TODO(p0.3): SELECT * FROM services WHERE tenant_id=? ORDER BY service_id。
func (s *SQLStore) AllServices(tenantID string) []*ServiceInstance {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*ServiceInstance
	for _, inst := range s.services {
		if tenantID != "" && inst.TenantID != tenantID {
			continue
		}
		out = append(out, inst)
	}
	sortServiceInstances(out)
	return out
}

// HeartbeatService 服务实例心跳。
// TODO(p0.3): UPDATE services SET last_heartbeat=NOW(), status=? WHERE service_id=? AND tenant_id=?。
func (s *SQLStore) HeartbeatService(tenantID, serviceID, status string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.services[serviceID]
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

// StaleServices 返回过期实例。
// TODO(p0.3): SELECT * FROM services WHERE tenant_id=? AND last_heartbeat < NOW() - ?。
func (s *SQLStore) StaleServices(tenantID string, maxAge time.Duration) []*ServiceInstance {
	if maxAge <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	threshold := time.Now().Add(-maxAge)
	var out []*ServiceInstance
	for _, inst := range s.services {
		if !inst.LastHeartbeat.Before(threshold) {
			continue
		}
		if tenantID != "" && inst.TenantID != tenantID {
			continue
		}
		out = append(out, inst)
	}
	sortServiceInstances(out)
	return out
}
