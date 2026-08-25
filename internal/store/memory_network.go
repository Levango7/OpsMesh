
package store

// memory_network.go 实现 MemoryStore 的 NetworkStore 子接口（Phase 4 网络管理）。
//
// 网络管理内存实现：
//   - networkDevices 字段在 MemoryStore struct 中定义（map[string]*NetworkDevice）；
//   - NewMemoryStore 中初始化为空 map；
//   - 本文件实现 8 个方法，全部经 m.mu 互斥保护，并发安全。
//
// 设计要点（与 memory_traffic.go 风格一致）：
//   - ListNetworkDevices 返回深拷贝避免外部修改破坏内部状态；
//   - CreateNetworkDevice 分配随机 ID（"netdev-" + 16 字节 hex）；
//   - StoreNetworkMetrics 用 metricsRing 保留最近 N 条历史。

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// randNetworkDeviceID 生成随机网络设备 ID（"netdev-" + 16 字节 hex）。
func randNetworkDeviceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("netdev-%d", time.Now().UnixNano())
	}
	return "netdev-" + hex.EncodeToString(b)
}

// cloneNetworkDevice 返回 d 的深拷贝。
func cloneNetworkDevice(d *NetworkDevice) *NetworkDevice {
	if d == nil {
		return nil
	}
	cp := *d
	return &cp
}

// CreateNetworkDevice 创建网络设备（ID 为空时分配随机 ID）。
func (m *MemoryStore) CreateNetworkDevice(tenantID string, d *NetworkDevice) *NetworkDevice {
	if d == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	d.TenantID = tenantID
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if d.ID == "" {
		d.ID = randNetworkDeviceID()
	}
	if d.Status == "" {
		d.Status = "unknown"
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	d.UpdatedAt = now
	m.networkDevices[d.ID] = d
	return cloneNetworkDevice(d)
}

// GetNetworkDevice 按 (tenantID, id) 返回单个网络设备（深拷贝）。
func (m *MemoryStore) GetNetworkDevice(tenantID, id string) (*NetworkDevice, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	d, ok := m.networkDevices[id]
	if !ok {
		return nil, false
	}
	if tenantID != "" && d.TenantID != tenantID {
		return nil, false
	}
	return cloneNetworkDevice(d), true
}

// ListNetworkDevices 返回指定租户的全部网络设备（深拷贝，按创建时间降序）。
func (m *MemoryStore) ListNetworkDevices(tenantID string) []*NetworkDevice {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*NetworkDevice, 0)
	for _, d := range m.networkDevices {
		if tenantID != "" && d.TenantID != tenantID {
			continue
		}
		out = append(out, cloneNetworkDevice(d))
	}
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].CreatedAt.After(out[j-1].CreatedAt) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

// UpdateNetworkDevice 更新网络设备（按 d.ID 定位，校验 tenantID 归属）。
func (m *MemoryStore) UpdateNetworkDevice(tenantID string, d *NetworkDevice) (*NetworkDevice, bool) {
	if d == nil || d.ID == "" {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.networkDevices[d.ID]
	if !ok {
		return nil, false
	}
	if tenantID != "" && existing.TenantID != tenantID {
		return nil, false
	}
	d.TenantID = existing.TenantID
	d.CreatedAt = existing.CreatedAt
	d.UpdatedAt = time.Now()
	m.networkDevices[d.ID] = d
	return cloneNetworkDevice(d), true
}

// DeleteNetworkDevice 删除网络设备。
func (m *MemoryStore) DeleteNetworkDevice(tenantID, id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.networkDevices[id]
	if !ok {
		return false
	}
	if tenantID != "" && d.TenantID != tenantID {
		return false
	}
	delete(m.networkDevices, id)
	return true
}

// StoreNetworkMetrics 存储网络设备监控指标（按 deviceID 关联，保留最近 N 条历史）。
func (m *MemoryStore) StoreNetworkMetrics(deviceID string, metrics *NetworkMetrics) {
	if deviceID == "" || metrics == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	metrics.DeviceID = deviceID
	if metrics.Timestamp.IsZero() {
		metrics.Timestamp = time.Now()
	}
	if m.networkMetricsHistory == nil {
		m.networkMetricsHistory = make(map[string][]*NetworkMetrics)
	}
	hist := m.networkMetricsHistory[deviceID]
	hist = append(hist, metrics)
	// 保留最近 100 条。
	if len(hist) > 100 {
		hist = hist[len(hist)-100:]
	}
	m.networkMetricsHistory[deviceID] = hist
}

// GetNetworkMetrics 返回网络设备最近一次监控指标（不存在返回 nil）。
func (m *MemoryStore) GetNetworkMetrics(deviceID string) *NetworkMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	hist := m.networkMetricsHistory[deviceID]
	if len(hist) == 0 {
		return nil
	}
	cp := *hist[len(hist)-1]
	return &cp
}

// UpdateNetworkConfig 下发网络配置（更新 d.Config 字段，返回更新后的设备）。
func (m *MemoryStore) UpdateNetworkConfig(tenantID, id, config string) (*NetworkDevice, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.networkDevices[id]
	if !ok {
		return nil, false
	}
	if tenantID != "" && d.TenantID != tenantID {
		return nil, false
	}
	d.Config = config
	d.UpdatedAt = time.Now()
	return cloneNetworkDevice(d), true
}