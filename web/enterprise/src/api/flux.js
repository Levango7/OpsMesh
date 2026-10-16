// Flux 应用相关 API（GitOps 原生第二大支柱，与 ArgoCD 并列）。
//
// 后端契约（internal/controlplane/flux.go，server_lifecycle.go 注册）：
//   - GET    /api/v1/flux/apps          → {apps: [FluxApp]}
//   - POST   /api/v1/flux/apps          → FluxApp（201）
//   - GET    /api/v1/flux/apps/{id}     → FluxApp
//   - PUT    /api/v1/flux/apps/{id}     → FluxApp
//   - DELETE /api/v1/flux/apps/{id}     → {status: "deleted"}
//   - POST   /api/v1/flux/apps/{id}/sync → FluxApp（同步成功后返回更新状态）
// FluxApp 字段：id/name/namespace/type("kustomization"|"helmrelease")/
//   sourceURL/sourcePath/targetRevision/clusterURL/
//   syncPolicy("manual"|"auto")/
//   status("synced"|"outofsync"|"unknown"|"progressing")/
//   healthStatus("healthy"|"degraded"|"missing"|"unknown")/
//   createdAt/updatedAt
import { getJSON, postJSON, putJSON, deleteJSON } from './request'

export const listFluxApps = () => getJSON('/flux/apps')
export const createFluxApp = (body) => postJSON('/flux/apps', body)
export const getFluxApp = (id) => getJSON(`/flux/apps/${encodeURIComponent(id)}`)
export const updateFluxApp = (id, body) => putJSON(`/flux/apps/${encodeURIComponent(id)}`, body)
export const deleteFluxApp = (id) => deleteJSON(`/flux/apps/${encodeURIComponent(id)}`)
export const syncFluxApp = (id) => postJSON(`/flux/apps/${encodeURIComponent(id)}/sync`)
