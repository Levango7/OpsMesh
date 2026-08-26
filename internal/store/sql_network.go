package store

// sql_network.go 实现 SQLStore 的 NetworkStore 子接口（Phase 4 网络管理）。
//
// 【未实现的桩】本文件全部方法未接入 MySQL 持久化，经 stub_guard.StubNotImplemented
// 显式告警（每 (domain,method) 首次必打、60s 限频），并按统一约定返回零值：
//   - Create/Update 类返回 nil（不返回填充后的假对象）；
//   - Get/Delete 返回 (nil, false) / false；List 返回非 nil 空切片。
//
// TODO(p4): 接入 MySQL 持久化（network_devices 表：id PK + tenant_id + name +
// type + vendor + model + ip + mask + mac + location + snmp_community + status +
// config + created_at + updated_at；network_metrics 表：device_id + timestamp +
// cpu/memory/temp/uptime）。

// CreateNetworkDevice 创建网络设备（未实现的桩）。
func (s *SQLStore) CreateNetworkDevice(tenantID string, d *NetworkDevice) *NetworkDevice {
	StubNotImplemented("network", "CreateNetworkDevice")
	return nil
}

// GetNetworkDevice 按 (tenantID, id) 返回单个网络设备（未实现的桩）。
func (s *SQLStore) GetNetworkDevice(tenantID, id string) (*NetworkDevice, bool) {
	StubNotImplemented("network", "GetNetworkDevice")
	return nil, false
}

// ListNetworkDevices 返回指定租户的全部网络设备（未实现的桩；返回非 nil 空切片防上层 range panic）。
func (s *SQLStore) ListNetworkDevices(tenantID string) []*NetworkDevice {
	StubNotImplemented("network", "ListNetworkDevices")
	return []*NetworkDevice{}
}

// UpdateNetworkDevice 更新网络设备（未实现的桩）。
func (s *SQLStore) UpdateNetworkDevice(tenantID string, d *NetworkDevice) (*NetworkDevice, bool) {
	StubNotImplemented("network", "UpdateNetworkDevice")
	return nil, false
}

// DeleteNetworkDevice 删除网络设备（未实现的桩）。
func (s *SQLStore) DeleteNetworkDevice(tenantID, id string) bool {
	StubNotImplemented("network", "DeleteNetworkDevice")
	return false
}

// StoreNetworkMetrics 存储网络设备监控指标（未实现的桩；no-op，仅告警）。
func (s *SQLStore) StoreNetworkMetrics(deviceID string, m *NetworkMetrics) {
	StubNotImplemented("network", "StoreNetworkMetrics")
}

// GetNetworkMetrics 返回网络设备最近一次监控指标（未实现的桩）。
func (s *SQLStore) GetNetworkMetrics(deviceID string) *NetworkMetrics {
	StubNotImplemented("network", "GetNetworkMetrics")
	return nil
}

// UpdateNetworkConfig 下发网络配置（未实现的桩）。
func (s *SQLStore) UpdateNetworkConfig(tenantID, id, config string) (*NetworkDevice, bool) {
	StubNotImplemented("network", "UpdateNetworkConfig")
	return nil, false
}
