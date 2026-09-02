// Helm 应用商店相关 API
//
// Endpoint 契约（后端 internal/controlplane/helm.go，mux 已注册）：
//   - GET    /helm/repos                       仓库列表 → {repos: [HelmRepo]}
//   - POST   /helm/repos                       添加仓库 → HelmRepo（201）（body: {name, url, username, password}）
//   - DELETE /helm/repos/{name}                删除仓库 → {status: "deleted"}
//   - GET    /helm/repos/{name}/charts         仓库 Chart 列表 → {charts: [Chart]}
//   - GET    /helm/charts/search?q=xxx         Chart 搜索 → {charts: [Chart]}
//   - GET    /helm/releases                    Release 列表 → {releases: [HelmRelease]}
//   - POST   /helm/releases                    安装 Release → HelmRelease（201）（body: {name, chart, namespace, values, repo}）
//   - PUT    /helm/releases/{name}             升级 Release → HelmRelease
//   - DELETE /helm/releases/{name}             卸载 Release → {status: "deleted"}
//   - POST   /helm/releases/{name}/rollback    回滚 Release → HelmRelease（body: {revision}）
//   - GET    /helm/releases/{name}/history     Release 历史 → {history: [ReleaseHistory]}
//   - GET    /helm/catalog                     预置目录 → {categories: [CatalogCategory]}
//
// 响应结构：
//   HelmRepo       {name, url, username, addedAt}
//   Chart          {name, repo, version, description, appVersion, icon, keywords, home}
//   HelmRelease    {name, namespace, chart, chartVersion,
//                   status: "deployed|failed|pending", revision, updatedAt}
//   ReleaseHistory {revision, chart, chartVersion, status, updatedAt}
//   CatalogCategory {name, description, charts: [Chart]}
import { getJSON, postJSON, putJSON, deleteJSON } from './request'

// ---------- 仓库 CRUD ----------

export const getHelmRepos = () => getJSON('/helm/repos')
export const createHelmRepo = (body) => postJSON('/helm/repos', body)
export const deleteHelmRepo = (name) => deleteJSON(`/helm/repos/${encodeURIComponent(name)}`)

// 仓库内 Chart 列表
export const getRepoCharts = (name) => getJSON(`/helm/repos/${encodeURIComponent(name)}/charts`)

// ---------- Chart 搜索 ----------

export const searchCharts = (q) => getJSON('/helm/charts/search', { q })

// ---------- Release CRUD + 操作 ----------

export const getHelmReleases = () => getJSON('/helm/releases')
export const createHelmRelease = (body) => postJSON('/helm/releases', body)
export const upgradeHelmRelease = (name, body) => putJSON(`/helm/releases/${encodeURIComponent(name)}`, body)
export const deleteHelmRelease = (name) => deleteJSON(`/helm/releases/${encodeURIComponent(name)}`)

// Release 回滚
export const rollbackHelmRelease = (name, body) => postJSON(`/helm/releases/${encodeURIComponent(name)}/rollback`, body)

// Release 历史
export const getReleaseHistory = (name) => getJSON(`/helm/releases/${encodeURIComponent(name)}/history`)

// ---------- 预置目录 ----------

export const getHelmCatalog = () => getJSON('/helm/catalog')