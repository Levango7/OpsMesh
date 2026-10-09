// host.go 控制面的插件管理器持有器（TD-87 批 2：自父包 plugin_host.go 迁入）。
//
// # 为什么这一层存在
//
// internal/plugin 提供完整 Manager（注册/钩子/生命周期/并发安全），但**没有任何一处
// 在生产启动路径上构造它**——SetPluginManager 曾经只有测试调用，那正是 TD-62 潜伏的形态
// （框架齐备、交付物里零接线）。现在 NewServer 在启动路径调用 SetPluginManager。
//
// 全局而非 Server 字段：控制面的扩展点在 Server 之外也可能被触发（例如子包接线点），
// 且生命周期是「启动期注入一次、之后只读」；nil 是合法状态（未启用插件宿主）。
package pluginhost

import "github.com/Levango7/OpsMesh/internal/plugin"

// pluginMgr 是控制面的插件管理器（进程级唯一）。nil = 本次进程没有插件宿主。
var pluginMgr *plugin.Manager

// SetPluginManager 注入插件管理器（启动期调用一次，之后只读）。传 nil 表示不启用。
func SetPluginManager(m *plugin.Manager) { pluginMgr = m }

// PluginManager 返回当前插件管理器（nil 表示未启用）。
func PluginManager() *plugin.Manager { return pluginMgr }
