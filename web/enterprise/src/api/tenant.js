// 租户管理相关 API
//
// 后端契约（internal/controlplane/tenant.go，server_lifecycle.go 注册）：
//   - GET    /api/v1/tenants                    → {tenants: [Tenant]}
//   - POST   /api/v1/tenants                    → Tenant（201，body.name 必填）
//   - GET    /api/v1/tenants/{id}               → Tenant
//   - PUT    /api/v1/tenants/{id}               → Tenant
//   - DELETE /api/v1/tenants/{id}               → {status: "deleted"}
//   - POST   /api/v1/tenants/{id}/suspend       → Tenant（暂停租户）
//   - POST   /api/v1/tenants/{id}/activate      → Tenant（激活租户）
// Tenant 字段：id/name(唯一 URL-safe)/displayName/status("active"|"suspended"|"disabled")/
//   quota/usage/createdAt/updatedAt
import { getJSON, postJSON, putJSON, deleteJSON } from './request'

export const listTenants = () => getJSON('/tenants')
export const createTenant = (body) => postJSON('/tenants', body)
export const getTenant = (id) => getJSON(`/tenants/${encodeURIComponent(id)}`)
export const updateTenant = (id, body) => putJSON(`/tenants/${encodeURIComponent(id)}`, body)
export const deleteTenant = (id) => deleteJSON(`/tenants/${encodeURIComponent(id)}`)
export const suspendTenant = (id) => postJSON(`/tenants/${encodeURIComponent(id)}/suspend`)
export const activateTenant = (id) => postJSON(`/tenants/${encodeURIComponent(id)}/activate`)
