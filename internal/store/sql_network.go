
package store

// sql_network.go 实现 SQLStore 的 NetworkStore 子接口（Phase 4 网络管理，桩实现）。
//
// TODO(p4): 接入 MySQL 持久化（network_devices 表：id PK + tenant_id + name +
// type + vendor + model + ip + mask + mac + location + snmp_community + status +
// config + created_at + updated_at；network_metrics 表：device_id + timestamp +
// cpu/memory/temp/uptime）。
// MVP 用内存桩实现，保证接口齐全 + go build 通过；DB 不可用时返回零值，
// 不 panic，与 SQLStore 其他桩方法风格一致（参考 sql_traffic.go）。

// CreateNetworkDevice 创建网络设备（桩实现）。
func (s *SQLStore) CreateNetworkDevice(tenantID string, d *NetworkDevice) *NetworkDevice {
	return nil
}

// GetNetworkDevice 按 (tenantID, id) 返回单个网络设备（桩实现）。
func (s *SQLStore) GetNetworkDevice(tenantID, id string) (*NetworkDevice, bool) {
	return nil, false
}

// ListNetworkDevices 返回指定租户的全部网络设备（桩实现）。
func (s *SQLStore) ListNetworkDevices(tenantID string) []*NetworkDevice {
	return []*NetworkDevice{}
}

// UpdateNetworkDevice 更新网络设备（桩实现）。
func (s *SQLStore) UpdateNetworkDevice(tenantID string, d *NetworkDevice) (*NetworkDevice, bool) {
	return nil, false
}

// DeleteNetworkDevice 删除网络设备（桩实现）。
func (s *SQLStore) DeleteNetworkDevice(tenantID, id string) bool {
	return false
}

// StoreNetworkMetrics 存储网络设备监控指标（桩实现）。
func (s *SQLStore) StoreNetworkMetrics(deviceID string, m *NetworkMetrics) {}

// GetNetworkMetrics 返回网络设备最近一次监控指标（桩实现）。
func (s *SQLStore) GetNetworkMetrics(deviceID string) *NetworkMetrics {
	return nil
}

// UpdateNetworkConfig 下发网络配置（桩实现）。
func (s *SQLStore) UpdateNetworkConfig(tenantID, id, config string) (*NetworkDevice, bool) {
	return nil, false
}