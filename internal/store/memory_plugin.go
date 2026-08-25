
// memory_plugin.go 实现 MemoryStore 的 PluginStore 子接口（Phase 6 插件市场）。
package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// randPluginID 生成随机插件 ID（"plugin-" + 16 字节 hex）。
func randPluginID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("plugin-%d", time.Now().UnixNano())
	}
	return "plugin-" + hex.EncodeToString(b)
}

// clonePlugin 返回 p 的深拷贝。
func clonePlugin(p *Plugin) *Plugin {
	if p == nil {
		return nil
	}
	cp := *p
	return &cp
}

// CreatePlugin 创建插件（按 ID 幂等；ID 为空时分配随机 ID）。
func (m *MemoryStore) CreatePlugin(plugin *Plugin) *Plugin {
	if plugin == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if plugin.ID == "" {
		plugin.ID = randPluginID()
	}
	if plugin.CreatedAt.IsZero() {
		plugin.CreatedAt = time.Now()
	}
	m.plugins[plugin.ID] = plugin
	return clonePlugin(plugin)
}

// GetPlugin 按 ID 返回单个插件（深拷贝；不存在返回 (nil, false)）。
func (m *MemoryStore) GetPlugin(id string) (*Plugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.plugins[id]
	if !ok {
		return nil, false
	}
	return clonePlugin(p), true
}

// UpdatePlugin 更新插件（按 plugin.ID 定位）。
func (m *MemoryStore) UpdatePlugin(plugin *Plugin) (*Plugin, bool) {
	if plugin == nil || plugin.ID == "" {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.plugins[plugin.ID]
	if !ok {
		return nil, false
	}
	plugin.ID = existing.ID
	plugin.CreatedAt = existing.CreatedAt
	m.plugins[plugin.ID] = plugin
	return clonePlugin(plugin), true
}

// ListPlugins 返回全部插件（按创建时间升序）。
func (m *MemoryStore) ListPlugins() []*Plugin {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Plugin, 0, len(m.plugins))
	for _, p := range m.plugins {
		out = append(out, clonePlugin(p))
	}
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].CreatedAt.Before(out[j-1].CreatedAt) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

// DeletePlugin 按 ID 删除插件。不存在返回 false。
func (m *MemoryStore) DeletePlugin(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.plugins[id]; !ok {
		return false
	}
	delete(m.plugins, id)
	return true
}