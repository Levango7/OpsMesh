package store

import (
	"time"
)

// multi_schema_p03.go MultiSchemaStore 对 P0.3 三个新接口的委托实现。
//
// MultiSchemaStore 按 tenantID 路由到 per-tenant Store（SQLStore 或测试 mock），
// 各方法委托给底层 store。新接口方法签名与 MemoryStore/SQLStore 一致，
// 仅在路由层做租户隔离分发。
//
// 设计要点：
//   - 带 tenantID 参数的方法直接用 storeFor(tenantID) 路由。
//   - RegisterService(inst) 用 inst.TenantID 路由（空串归一为 default）。
//   - SetConfig(item) 用 item.TenantID 路由（空串归一为 default）。
//   - SetSecret(item, tenantID) 用参数 tenantID 路由。
//   - 路由失败返回零值（nil/false），与现有方法风格一致。

// ============================================================================
// ServiceDiscoveryStore 实现（6 方法）
// ============================================================================

// RegisterService 注册服务实例：用 inst.TenantID 路由（空串归一为 default）。
func (m *MultiSchemaStore) RegisterService(inst *ServiceInstance) *ServiceInstance {
	if inst == nil {
		return nil
	}
	if inst.TenantID == "" {
		inst.TenantID = "default"
	}
	s, err := m.storeFor(inst.TenantID)
	if err != nil {
		return nil
	}
	return s.RegisterService(inst)
}

// DeregisterService 反注册服务实例：用 tenantID 路由。
func (m *MultiSchemaStore) DeregisterService(tenantID, serviceID string) bool {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return false
	}
	return s.DeregisterService(tenantID, serviceID)
}

// ServiceInstances 返回指定服务名下的全部实例：用 tenantID 路由。
func (m *MultiSchemaStore) ServiceInstances(tenantID, serviceName string) []*ServiceInstance {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.ServiceInstances(tenantID, serviceName)
}

// AllServices 返回全部服务实例：用 tenantID 路由。
func (m *MultiSchemaStore) AllServices(tenantID string) []*ServiceInstance {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.AllServices(tenantID)
}

// HeartbeatService 服务实例心跳：用 tenantID 路由。
func (m *MultiSchemaStore) HeartbeatService(tenantID, serviceID, status string) bool {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return false
	}
	return s.HeartbeatService(tenantID, serviceID, status)
}

// StaleServices 返回过期实例：用 tenantID 路由。
func (m *MultiSchemaStore) StaleServices(tenantID string, maxAge time.Duration) []*ServiceInstance {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.StaleServices(tenantID, maxAge)
}

// ============================================================================
// ConfigStore 实现（6 方法）
// ============================================================================

// GetConfig 按 (tenantID, key) 返回配置项：用 tenantID 路由。
func (m *MultiSchemaStore) GetConfig(tenantID, key string) (*ConfigItem, bool) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.GetConfig(tenantID, key)
}

// SetConfig 写入/更新配置：用 item.TenantID 路由（空串归一为 default）。
func (m *MultiSchemaStore) SetConfig(item *ConfigItem) *ConfigItem {
	if item == nil {
		return nil
	}
	if item.TenantID == "" {
		item.TenantID = "default"
	}
	s, err := m.storeFor(item.TenantID)
	if err != nil {
		return nil
	}
	return s.SetConfig(item)
}

// DeleteConfig 删除配置：用 tenantID 路由。
func (m *MultiSchemaStore) DeleteConfig(tenantID, key string) bool {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return false
	}
	return s.DeleteConfig(tenantID, key)
}

// ListConfigs 列出指定租户的全部配置：用 tenantID 路由。
func (m *MultiSchemaStore) ListConfigs(tenantID string) []*ConfigItem {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.ListConfigs(tenantID)
}

// ConfigHistory 返回版本历史：用 tenantID 路由。
func (m *MultiSchemaStore) ConfigHistory(tenantID, key string) []*ConfigItem {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.ConfigHistory(tenantID, key)
}

// PublishConfig 发布配置变更：用 tenantID 路由。
func (m *MultiSchemaStore) PublishConfig(tenantID, key string) (*ConfigItem, bool) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.PublishConfig(tenantID, key)
}

// ============================================================================
// SecretStore 实现（6 方法）
// ============================================================================

// GetSecret 按 (tenantID, key) 返回密钥明文：用 tenantID 路由。
func (m *MultiSchemaStore) GetSecret(tenantID, key string) (*SecretItem, bool) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.GetSecret(tenantID, key)
}

// SetSecret 写入/轮换密钥：用参数 tenantID 路由（空串归一为 default）。
func (m *MultiSchemaStore) SetSecret(item *SecretItem, tenantID string) *SecretMeta {
	if item == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.SetSecret(item, tenantID)
}

// DeleteSecret 删除密钥：用 tenantID 路由。
func (m *MultiSchemaStore) DeleteSecret(tenantID, key string) bool {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return false
	}
	return s.DeleteSecret(tenantID, key)
}

// ListSecrets 列出指定租户的全部密钥元信息：用 tenantID 路由。
func (m *MultiSchemaStore) ListSecrets(tenantID string) []*SecretMeta {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.ListSecrets(tenantID)
}

// RotateSecret 轮换密钥：用 tenantID 路由。
func (m *MultiSchemaStore) RotateSecret(tenantID, key, newValue string) *SecretMeta {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.RotateSecret(tenantID, key, newValue)
}

// SecretVersions 返回密钥全部版本元信息：用 tenantID 路由。
func (m *MultiSchemaStore) SecretVersions(tenantID, key string) []*SecretMeta {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.SecretVersions(tenantID, key)
}
