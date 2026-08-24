package store

import (
	"sort"
	"time"
)

// memory_config.go MemoryStore 对 ConfigStore 接口的实现。
// 配置中心领域：Get/Set/Delete/List + 版本历史 + 发布。
// 复用 MemoryStore.mu（sync.RWMutex）保护并发安全。
//
// 设计要点：
//   - 按 (tenantID, key) 唯一；复合键形如 "tenantID|key"。
//   - SetConfig 已存在则 Version+1 并把前版本写入 configHistory；不存在则 Version=1。
//   - ConfigHistory 返回升序版本切片（最近 N 条，N 由实现决定，这里无上限）。
//   - PublishConfig MVP 仅返回当前配置（标记发布语义留待事件总线联动）。

// configMaxHistory 每个配置键保留的版本历史上限。
// 避免无界增长；超过则丢弃最旧版本（FIFO）。
const configMaxHistory = 64

// configKey 拼装复合键。
func configKey(tenantID, key string) string { return tenantID + "|" + key }

// GetConfig 按 (tenantID, key) 返回当前配置项；不存在返回 (nil, false)。
func (m *MemoryStore) GetConfig(tenantID, key string) (*ConfigItem, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item, ok := m.configs[configKey(tenantID, key)]
	if !ok {
		return nil, false
	}
	return item, true
}

// SetConfig 写入/更新配置（按 key 幂等）。已存在则 Version+1 并把前版本写入历史；
// 不存在则 Version=1。返回更新后的配置项。
func (m *MemoryStore) SetConfig(item *ConfigItem) *ConfigItem {
	if item == nil {
		return nil
	}
	if item.TenantID == "" {
		item.TenantID = "default"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	ck := configKey(item.TenantID, item.Key)
	now := time.Now()
	if existing, ok := m.configs[ck]; ok {
		// 写入前版本到历史（深拷贝以避免后续修改影响历史）。
		prev := *existing
		m.configHistory[ck] = append(m.configHistory[ck], &prev)
		if len(m.configHistory[ck]) > configMaxHistory {
			m.configHistory[ck] = m.configHistory[ck][len(m.configHistory[ck])-configMaxHistory:]
		}
		// 更新当前版本。
		existing.Value = item.Value
		existing.Format = item.Format
		existing.Description = item.Description
		existing.UpdatedBy = item.UpdatedBy
		existing.Version++
		existing.UpdatedAt = now
		return existing
	}
	// 新建。
	if item.Version == 0 {
		item.Version = 1
	}
	item.UpdatedAt = now
	m.configs[ck] = item
	return item
}

// DeleteConfig 删除配置（按 tenantID + key，含版本历史）。返回是否删除成功。
func (m *MemoryStore) DeleteConfig(tenantID, key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	ck := configKey(tenantID, key)
	if _, ok := m.configs[ck]; !ok {
		return false
	}
	delete(m.configs, ck)
	delete(m.configHistory, ck)
	return true
}

// ListConfigs 列出指定租户的全部配置（按 key 升序）。
func (m *MemoryStore) ListConfigs(tenantID string) []*ConfigItem {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*ConfigItem
	for _, item := range m.configs {
		if item.TenantID != tenantID {
			continue
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// ConfigHistory 返回指定配置的版本历史（按 version 升序）。
// 注意：历史不含当前版本（当前版本在 GetConfig 中获取）；如需全量含当前版本，
// 调用方可自行 append。这里保持与"版本历史"语义一致——历史=已过去的版本。
func (m *MemoryStore) ConfigHistory(tenantID, key string) []*ConfigItem {
	m.mu.RLock()
	defer m.mu.RUnlock()
	hist := m.configHistory[configKey(tenantID, key)]
	if len(hist) == 0 {
		return nil
	}
	// 返回副本以避免外部修改影响内部状态。
	out := make([]*ConfigItem, len(hist))
	copy(out, hist)
	return out
}

// PublishConfig 发布配置变更（MVP 仅返回当前配置；后续可触发事件总线）。
// 不存在返回 (nil, false)。
func (m *MemoryStore) PublishConfig(tenantID, key string) (*ConfigItem, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item, ok := m.configs[configKey(tenantID, key)]
	if !ok {
		return nil, false
	}
	return item, true
}
