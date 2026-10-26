// 云账号管理相关 API
//
// 后端契约（internal/controlplane/cloud_account.go + server_lifecycle.go 注册）：
//   - GET    /api/v1/cloud-accounts                     → {accounts: [CloudAccount], total, offset, limit}
//   - POST   /api/v1/cloud-accounts                     → CloudAccount（201）
//   - GET    /api/v1/cloud-accounts/{id}                → CloudAccount
//   - PUT    /api/v1/cloud-accounts/{id}                → {status: "updated"}
//   - DELETE /api/v1/cloud-accounts/{id}                → 204 No Content（无响应体）
//   - POST   /api/v1/cloud-accounts/{id}/sync           → {status:"synced", syncStatus, discoveredCount}
//   - POST   /api/v1/cloud-accounts/{id}/test           → {status:"ok"|"failed", message}
//   - GET    /api/v1/cloud-accounts/{id}/regions        → {provider, regions: [string]}
//   - GET    /api/v1/cloud-resources                    → {resources: [CloudResource], total, offset, limit}
// CloudAccount 字段（store.CloudAccount，camelCase）：id/tenantId/workspaceId/name/provider/region/
//   authType/status("active"|"pending"|"failed"|"disabled")/syncStatus/credentialsRef/
//   lastSyncAt/discoveredCount/createdAt/updatedAt/createdBy
//
// 路径写法：axios 实例的 baseURL 已经是 /api/v1（见 src/api/request.js），
// 这里只写 baseURL 之后的路径。此前本模块把 "/api/v1" 也写进了路径，
// 拼出来是 /api/v1/api/v1/... → 全量 404（云账号页整页失效）。
//
// {id} 子路径与 src/api/k8s.js 保持同一写法：id 先 encodeURIComponent 再拼，
// cloud-accounts/{id} 后面不再补尾斜杠。
// 后端把 "/api/v1/cloud-accounts/" 注册成子树前缀（server_lifecycle.go），
// handleCloudAccountRouting 用 TrimPrefix 去掉该前缀后按 "/" 切分取 id 与子动作，
// 故 /{id}、/{id}/sync、/{id}/test、/{id}/regions 都是平铺写法（无尾斜杠）。
import { http as request } from '@/api/request'

export function listCloudAccounts(params) {
  return request.get('/cloud-accounts', { params })
}
export function createCloudAccount(data) {
  // 后端创建端点按驼峰取字段（json:"authType"，缺该字段直接 400
  // "name, provider and authType are required"），而页面表单传的是 auth_type。
  // 这里做一次兼容映射（两种写法都能用），否则创建永远 400。
  const body = { ...data }
  if (body.authType === undefined && body.auth_type !== undefined) {
    body.authType = body.auth_type
  }
  return request.post('/cloud-accounts', body)
}
export function getCloudAccount(id) {
  return request.get(`/cloud-accounts/${encodeURIComponent(id)}`)
}
export function updateCloudAccount(id, data) {
  // 更新端点把 body 当 map 交给 store，store 只认 snake_case 列名
  // （name/provider/region/auth_type/...），故这里原样透传、不做驼峰转换。
  return request.put(`/cloud-accounts/${encodeURIComponent(id)}`, data)
}
export function deleteCloudAccount(id) {
  return request.delete(`/cloud-accounts/${encodeURIComponent(id)}`)
}
export function syncCloudAccount(id) {
  return request.post(`/cloud-accounts/${encodeURIComponent(id)}/sync`)
}
export function testCloudAccount(id) {
  return request.post(`/cloud-accounts/${encodeURIComponent(id)}/test`)
}
export function listCloudAccountRegions(id) {
  return request.get(`/cloud-accounts/${encodeURIComponent(id)}/regions`)
}
export function listCloudResources(params) {
  return request.get('/cloud-resources', { params })
}
