// ArgoCD 应用相关 API
//
// 后端契约（internal/controlplane/argocd.go，server_lifecycle.go 注册）：
//   - GET    /api/v1/argocd/apps          → {apps: [ArgoCDApp]}
//   - POST   /api/v1/argocd/apps          → ArgoCDApp（201）
//   - GET    /api/v1/argocd/apps/{id}     → ArgoCDApp
//   - PUT    /api/v1/argocd/apps/{id}     → ArgoCDApp
//   - DELETE /api/v1/argocd/apps/{id}     → {status: "deleted"}
//   - POST   /api/v1/argocd/apps/{id}/sync → ArgoCDApp（同步成功后返回更新状态）
// ArgoCDApp 字段：id/name/namespace/repoURL/path/targetRevision("main"|"HEAD")/
//   clusterURL/syncPolicy("manual"|"auto")/status("synced"|"outofsync"|"unknown")/
//   healthStatus("healthy"|"degraded"|"missing"|"unknown")/createdAt/updatedAt
import { getJSON, postJSON, putJSON, deleteJSON } from './request'

export const listApps = () => getJSON('/argocd/apps')
export const createApp = (body) => postJSON('/argocd/apps', body)
export const getApp = (id) => getJSON(`/argocd/apps/${encodeURIComponent(id)}`)
export const updateApp = (id, body) => putJSON(`/argocd/apps/${encodeURIComponent(id)}`, body)
export const deleteApp = (id) => deleteJSON(`/argocd/apps/${encodeURIComponent(id)}`)
export const syncApp = (id) => postJSON(`/argocd/apps/${encodeURIComponent(id)}/sync`)
