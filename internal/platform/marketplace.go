
// marketplace.go 实现插件市场引擎（平台化）。
//
// 插件市场支持安装/卸载第三方扩展插件，扩展控制面能力：
//   - agent 插件：扩展 agent 端能力（如自定义采集器）；
//   - controlplane 插件：扩展控制面 API（如自定义 handler）；
//   - ui 插件：扩展前端界面（如自定义仪表盘 widget）。
//
// 设计要点：
//   - 插件元数据（Plugin）持久化在 store；插件二进制/源码由 DownloadURL 外链；
//   - Checksum（SHA-256）用于下载完整性校验（防篡改）；
//   - Installed 标记是否已安装；Enabled 标记是否已启用；
//   - 安装/卸载/启停均为幂等操作。
package platform

import (
	"errors"
	"fmt"

	"opsmesh/internal/store"
)

// 复用 store 包 Plugin 数据模型。
type Plugin = store.Plugin

// PluginManager 插件市场引擎。
type PluginManager struct {
	store store.PluginStore
}

// NewPluginManager 构造插件市场引擎。
func NewPluginManager(s store.PluginStore) *PluginManager {
	return &PluginManager{store: s}
}

// InstallPlugin 安装插件。
// 行为：
//   - 插件必须已注册（存在于 store）；
//   - 已安装则幂等返回 nil；
//   - 安装：置 Installed=true, Enabled=true。
// TODO(p6): 实际下载 + Checksum 校验 + 解压到插件目录。
func (m *PluginManager) InstallPlugin(pluginID string) error {
	if pluginID == "" {
		return errors.New("plugin id is required")
	}
	p, ok := m.store.GetPlugin(pluginID)
	if !ok || p == nil {
		return fmt.Errorf("plugin %q not found in marketplace", pluginID)
	}
	if p.Installed {
		// 幂等：已安装直接返回。
		return nil
	}
	p.Installed = true
	p.Enabled = true
	if _, ok := m.store.UpdatePlugin(p); !ok {
		return fmt.Errorf("update plugin %q failed", pluginID)
	}
	return nil
}

// UninstallPlugin 卸载插件。
// 行为：
//   - 未安装则返回错误；
//   - 卸载：置 Installed=false, Enabled=false。
// TODO(p6): 实际清理插件目录 + 卸载资源。
func (m *PluginManager) UninstallPlugin(pluginID string) error {
	if pluginID == "" {
		return errors.New("plugin id is required")
	}
	p, ok := m.store.GetPlugin(pluginID)
	if !ok || p == nil {
		return fmt.Errorf("plugin %q not found", pluginID)
	}
	if !p.Installed {
		return fmt.Errorf("plugin %q is not installed", pluginID)
	}
	p.Installed = false
	p.Enabled = false
	if _, ok := m.store.UpdatePlugin(p); !ok {
		return fmt.Errorf("update plugin %q failed", pluginID)
	}
	return nil
}

// ListInstalled 返回已安装的插件列表。
func (m *PluginManager) ListInstalled() []*Plugin {
	all := m.store.ListPlugins()
	out := make([]*Plugin, 0, len(all))
	for _, p := range all {
		if p != nil && p.Installed {
			out = append(out, p)
		}
	}
	return out
}

// EnablePlugin 启用已安装插件。
func (m *PluginManager) EnablePlugin(pluginID string) error {
	if pluginID == "" {
		return errors.New("plugin id is required")
	}
	p, ok := m.store.GetPlugin(pluginID)
	if !ok || p == nil {
		return fmt.Errorf("plugin %q not found", pluginID)
	}
	if !p.Installed {
		return fmt.Errorf("plugin %q is not installed", pluginID)
	}
	p.Enabled = true
	if _, ok := m.store.UpdatePlugin(p); !ok {
		return fmt.Errorf("update plugin %q failed", pluginID)
	}
	return nil
}

// DisablePlugin 禁用已安装插件。
func (m *PluginManager) DisablePlugin(pluginID string) error {
	if pluginID == "" {
		return errors.New("plugin id is required")
	}
	p, ok := m.store.GetPlugin(pluginID)
	if !ok || p == nil {
		return fmt.Errorf("plugin %q not found", pluginID)
	}
	if !p.Installed {
		return fmt.Errorf("plugin %q is not installed", pluginID)
	}
	p.Enabled = false
	if _, ok := m.store.UpdatePlugin(p); !ok {
		return fmt.Errorf("update plugin %q failed", pluginID)
	}
	return nil
}