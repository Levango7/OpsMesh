// Plugin 插件市场 API
// 契约：
//   GET    /api/v1/marketplace/plugins                              → 200 {plugins: [{id,name,description,version,author,category,downloads,status,installedAt}]}
//   GET    /api/v1/marketplace/plugins/{id}                         → 200 Plugin
//   POST   /api/v1/marketplace/plugins/{id}/install                 → 200 {status,message}
//   POST   /api/v1/marketplace/plugins/{id}/uninstall               → 200 {status,message}
//   GET    /api/v1/marketplace/plugins/{id}/versions                → 200 {versions: [{version,changelog,releasedAt}]}
//   GET    /api/v1/marketplace/plugins/categories                   → 200 {categories: [{id,name,count}]}
import { getJSON, postJSON } from './request'

// ---------- 插件列表 ----------

export const getPlugins = (category, search) =>
  getJSON('/marketplace/plugins', { category, search })

export const getPlugin = (id) =>
  getJSON(`/marketplace/plugins/${encodeURIComponent(id)}`)

// ---------- 安装/卸载 ----------

export const installPlugin = (id) =>
  postJSON(`/marketplace/plugins/${encodeURIComponent(id)}/install`)

export const uninstallPlugin = (id) =>
  postJSON(`/marketplace/plugins/${encodeURIComponent(id)}/uninstall`)

// ---------- 版本历史 ----------

export const getPluginVersions = (id) =>
  getJSON(`/marketplace/plugins/${encodeURIComponent(id)}/versions`)

// ---------- 分类 ----------

export const getPluginCategories = () => getJSON('/marketplace/plugins/categories')

// 兼容旧引用：聚合对象形式
export const pluginApi = {
  getPlugins,
  getPlugin,
  installPlugin,
  uninstallPlugin,
  getPluginVersions,
  getPluginCategories
}
