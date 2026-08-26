package store

// multi_schema_p4.go MultiSchemaStore 对 Phase 4 两个新接口（NetworkStore / AutomationStore）的委托实现。
//
// MultiSchemaStore 按 tenantID 路由到 per-tenant Store（SQLStore 或测试 mock），
// 各方法委托给底层 store。新接口方法签名与 MemoryStore/SQLStore 一致，
// 仅在路由层做租户隔离分发。
//
// 设计要点（与 multi_schema_p2.go 风格一致）：
//   - 带 tenantID 参数的方法直接用 storeFor(tenantID) 路由。
//   - 路由失败返回零值（nil/false），与现有方法风格一致。

// ============================================================================
// NetworkStore 实现（8 方法）
// ============================================================================

// CreateNetworkDevice 创建网络设备：用参数 tenantID 路由（空串归一为 default）。
func (m *MultiSchemaStore) CreateNetworkDevice(tenantID string, d *NetworkDevice) *NetworkDevice {
	if d == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.CreateNetworkDevice(tenantID, d)
}

// GetNetworkDevice 按 (tenantID, id) 返回单个网络设备：用 tenantID 路由。
func (m *MultiSchemaStore) GetNetworkDevice(tenantID, id string) (*NetworkDevice, bool) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.GetNetworkDevice(tenantID, id)
}

// ListNetworkDevices 返回指定租户的全部网络设备：用 tenantID 路由。
func (m *MultiSchemaStore) ListNetworkDevices(tenantID string) []*NetworkDevice {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.ListNetworkDevices(tenantID)
}

// UpdateNetworkDevice 更新网络设备：用 tenantID 路由。
func (m *MultiSchemaStore) UpdateNetworkDevice(tenantID string, d *NetworkDevice) (*NetworkDevice, bool) {
	if d == nil {
		return nil, false
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.UpdateNetworkDevice(tenantID, d)
}

// DeleteNetworkDevice 删除网络设备：用 tenantID 路由。
func (m *MultiSchemaStore) DeleteNetworkDevice(tenantID, id string) bool {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return false
	}
	return s.DeleteNetworkDevice(tenantID, id)
}

// StoreNetworkMetrics 存储网络设备监控指标：按 deviceID 查找所属租户后路由。
func (m *MultiSchemaStore) StoreNetworkMetrics(deviceID string, metrics *NetworkMetrics) {
	if deviceID == "" || metrics == nil {
		return
	}
	tenant := m.lookupDeviceTenant(deviceID)
	s, err := m.storeFor(tenant)
	if err != nil {
		return
	}
	s.StoreNetworkMetrics(deviceID, metrics)
}

// GetNetworkMetrics 返回网络设备最近一次监控指标：按 deviceID 查找所属租户后路由。
func (m *MultiSchemaStore) GetNetworkMetrics(deviceID string) *NetworkMetrics {
	tenant := m.lookupDeviceTenant(deviceID)
	s, err := m.storeFor(tenant)
	if err != nil {
		return nil
	}
	return s.GetNetworkMetrics(deviceID)
}

// UpdateNetworkConfig 下发网络配置：用 tenantID 路由。
func (m *MultiSchemaStore) UpdateNetworkConfig(tenantID, id, config string) (*NetworkDevice, bool) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.UpdateNetworkConfig(tenantID, id, config)
}

// ============================================================================
// AutomationStore 实现（10 方法）
// ============================================================================

// CreateAutomationRule 创建自动化规则：用参数 tenantID 路由（空串归一为 default）。
func (m *MultiSchemaStore) CreateAutomationRule(tenantID string, r *AutomationRule) *AutomationRule {
	if r == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.CreateAutomationRule(tenantID, r)
}

// GetAutomationRule 按 (tenantID, id) 返回单个规则：用 tenantID 路由。
func (m *MultiSchemaStore) GetAutomationRule(tenantID, id string) (*AutomationRule, bool) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.GetAutomationRule(tenantID, id)
}

// ListAutomationRules 返回指定租户的全部规则：用 tenantID 路由。
func (m *MultiSchemaStore) ListAutomationRules(tenantID string) []*AutomationRule {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.ListAutomationRules(tenantID)
}

// UpdateAutomationRule 更新规则：用 tenantID 路由。
func (m *MultiSchemaStore) UpdateAutomationRule(tenantID string, r *AutomationRule) (*AutomationRule, bool) {
	if r == nil {
		return nil, false
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.UpdateAutomationRule(tenantID, r)
}

// DeleteAutomationRule 删除规则：用 tenantID 路由。
func (m *MultiSchemaStore) DeleteAutomationRule(tenantID, id string) bool {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return false
	}
	return s.DeleteAutomationRule(tenantID, id)
}

// EnableAutomationRule 启用规则：用 tenantID 路由。
func (m *MultiSchemaStore) EnableAutomationRule(tenantID, id string) (*AutomationRule, bool) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.EnableAutomationRule(tenantID, id)
}

// DisableAutomationRule 禁用规则：用 tenantID 路由。
func (m *MultiSchemaStore) DisableAutomationRule(tenantID, id string) (*AutomationRule, bool) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.DisableAutomationRule(tenantID, id)
}

// CreateAutomationExecution 创建执行记录：用参数 tenantID 路由（空串归一为 default）。
func (m *MultiSchemaStore) CreateAutomationExecution(tenantID string, e *AutomationExecution) *AutomationExecution {
	if e == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.CreateAutomationExecution(tenantID, e)
}

// GetAutomationExecution 按 (tenantID, id) 返回单条执行记录：用 tenantID 路由。
func (m *MultiSchemaStore) GetAutomationExecution(tenantID, id string) (*AutomationExecution, bool) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.GetAutomationExecution(tenantID, id)
}

// ListAutomationExecutions 返回指定租户的执行记录列表：用 tenantID 路由。
func (m *MultiSchemaStore) ListAutomationExecutions(tenantID string, limit int) []*AutomationExecution {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.ListAutomationExecutions(tenantID, limit)
}
