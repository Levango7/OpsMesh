
// sql_plugin.go 实现 SQLStore 的 PluginStore 子接口（Phase 6，桩实现）。
package store

import "time"

// CreatePlugin 创建插件（桩实现）。
func (s *SQLStore) CreatePlugin(plugin *Plugin) *Plugin {
	if plugin == nil {
		return nil
	}
	if plugin.ID == "" {
		plugin.ID = randPluginID()
	}
	if plugin.CreatedAt.IsZero() {
		plugin.CreatedAt = time.Now().UTC()
	}
	return plugin
}

// GetPlugin 按 ID 返回单个插件（桩实现）。
func (s *SQLStore) GetPlugin(id string) (*Plugin, bool) {
	return nil, false
}

// UpdatePlugin 更新插件（桩实现）。
func (s *SQLStore) UpdatePlugin(plugin *Plugin) (*Plugin, bool) {
	return nil, false
}

// ListPlugins 返回全部插件（桩实现）。
func (s *SQLStore) ListPlugins() []*Plugin {
	return []*Plugin{}
}

// DeletePlugin 按 ID 删除插件（桩实现）。
func (s *SQLStore) DeletePlugin(id string) bool {
	return false
}