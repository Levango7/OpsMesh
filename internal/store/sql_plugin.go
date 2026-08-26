// sql_plugin.go 实现 SQLStore 的 PluginStore 子接口（Phase 6 插件市场）。
//
// 【未实现的桩】本文件全部方法未接入 MySQL 持久化，经 stub_guard.StubNotImplemented
// 显式告警（每 (domain,method) 首次必打、60s 限频），并按统一约定返回零值：
//   - CreatePlugin 返回 nil（不返回填充后的假对象——杜绝「201 假成功 → GET 404」链路）；
//   - Get/Update/Delete 返回 (nil, false) / false；ListPlugins 返回非 nil 空切片。
package store

// CreatePlugin 创建插件（未实现的桩）。
func (s *SQLStore) CreatePlugin(plugin *Plugin) *Plugin {
	StubNotImplemented("plugin", "CreatePlugin")
	return nil
}

// GetPlugin 按 ID 返回单个插件（未实现的桩）。
func (s *SQLStore) GetPlugin(id string) (*Plugin, bool) {
	StubNotImplemented("plugin", "GetPlugin")
	return nil, false
}

// UpdatePlugin 更新插件（未实现的桩）。
func (s *SQLStore) UpdatePlugin(plugin *Plugin) (*Plugin, bool) {
	StubNotImplemented("plugin", "UpdatePlugin")
	return nil, false
}

// ListPlugins 返回全部插件（未实现的桩；返回非 nil 空切片防上层 range panic）。
func (s *SQLStore) ListPlugins() []*Plugin {
	StubNotImplemented("plugin", "ListPlugins")
	return []*Plugin{}
}

// DeletePlugin 按 ID 删除插件（未实现的桩）。
func (s *SQLStore) DeletePlugin(id string) bool {
	StubNotImplemented("plugin", "DeletePlugin")
	return false
}
