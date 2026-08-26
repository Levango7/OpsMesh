// marketplace.go 保留插件市场数据模型类型别名（handler 经 store 引用）。
//
// 历史：原 PluginManager 插件市场引擎（InstallPlugin/UninstallPlugin/
// EnablePlugin/DisablePlugin/ListInstalled）已作为 H7 平台死代码清理删除——
// controlplane marketplace handler 直接调用 store.PluginStore 接口，
// 安装/卸载/启停逻辑在 handler 侧就近实现，不经 platform 层冗余封装。
//
// 设计要点：
//   - 插件元数据（Plugin）持久化在 store；插件二进制/源码由 DownloadURL 外链；
//   - Checksum（SHA-256）用于下载完整性校验（防篡改）；
//   - Installed 标记是否已安装；Enabled 标记是否已启用；
//   - 安装/卸载/启停均为幂等操作。
package platform

import (
	"opsmesh/internal/store"
)

// 复用 store 包 Plugin 数据模型。
type Plugin = store.Plugin
