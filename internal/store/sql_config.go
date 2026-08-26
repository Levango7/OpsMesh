package store

import (
	"sort"
	"time"
)

// sql_config.go SQLStore 对 ConfigStore 接口的桩实现。
//
// TODO(p0.3): 接入 MySQL 持久化：
//   - configs 表：(tenant_id, key) PK + value + format + version + description + updated_by + updated_at
//   - config_history 表：(tenant_id, key, version) PK + value + format + description + updated_by + updated_at
//   - SetConfig 用事务：INSERT 历史版本 + UPSERT 当前版本（version = version+1）。
// MVP 用内存 map 做缓存，保证接口齐全 + go build 通过。

// RegisterService 等方法在 sql_discovery.go；此处仅 ConfigStore 方法。

// GetConfig 按 (tenantID, key) 返回当前配置项。
// TODO(p0.3): SELECT * FROM configs WHERE tenant_id=? AND key=?。
func (s *SQLStore) GetConfig(tenantID, key string) (*ConfigItem, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.configs[configKey(tenantID, key)]
	if !ok {
		return nil, false
	}
	return item, true
}

// SetConfig 写入/更新配置。
// TODO(p0.3): 事务——INSERT INTO config_history(...) VALUES(旧版本); UPSERT configs SET version=version+1, ...。
func (s *SQLStore) SetConfig(item *ConfigItem) *ConfigItem {
	if item == nil {
		return nil
	}
	if item.TenantID == "" {
		item.TenantID = "default"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ck := configKey(item.TenantID, item.Key)
	now := time.Now()
	if existing, ok := s.configs[ck]; ok {
		prev := *existing
		s.configHistory[ck] = append(s.configHistory[ck], &prev)
		if len(s.configHistory[ck]) > configMaxHistory {
			s.configHistory[ck] = s.configHistory[ck][len(s.configHistory[ck])-configMaxHistory:]
		}
		existing.Value = item.Value
		existing.Format = item.Format
		existing.Description = item.Description
		existing.UpdatedBy = item.UpdatedBy
		existing.Version++
		existing.UpdatedAt = now
		return existing
	}
	if item.Version == 0 {
		item.Version = 1
	}
	item.UpdatedAt = now
	s.configs[ck] = item
	return item
}

// DeleteConfig 删除配置（含版本历史）。
// TODO(p0.3): 事务——DELETE FROM configs WHERE ...; DELETE FROM config_history WHERE ...。
func (s *SQLStore) DeleteConfig(tenantID, key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	ck := configKey(tenantID, key)
	if _, ok := s.configs[ck]; !ok {
		return false
	}
	delete(s.configs, ck)
	delete(s.configHistory, ck)
	return true
}

// ListConfigs 列出指定租户的全部配置。
// TODO(p0.3): SELECT * FROM configs WHERE tenant_id=? ORDER BY key。
func (s *SQLStore) ListConfigs(tenantID string) []*ConfigItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*ConfigItem
	for _, item := range s.configs {
		if item.TenantID != tenantID {
			continue
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// ConfigHistory 返回版本历史。
// TODO(p0.3): SELECT * FROM config_history WHERE tenant_id=? AND key=? ORDER BY version。
func (s *SQLStore) ConfigHistory(tenantID, key string) []*ConfigItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	hist := s.configHistory[configKey(tenantID, key)]
	if len(hist) == 0 {
		return nil
	}
	out := make([]*ConfigItem, len(hist))
	copy(out, hist)
	return out
}

// PublishConfig 发布配置变更。
// TODO(p0.3): 标记版本为已发布（configs.published_at = NOW()）+ 触发事件总线通知。
func (s *SQLStore) PublishConfig(tenantID, key string) (*ConfigItem, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.configs[configKey(tenantID, key)]
	if !ok {
		return nil, false
	}
	return item, true
}
