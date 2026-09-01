// 租户配额相关 API
//
// 后端契约（internal/controlplane/quota.go，server_lifecycle.go 注册）：
//   - GET    /api/v1/quotas           → {enabled, current: QuotaUsage}
//       （MVP：仅返回当前 actx 租户视图，非 admin 不能跨租户列配额）
//   - GET    /api/v1/quotas/{tenantID}  → QuotaUsage
//       {devices, tasks, alerts, quota: {maxDevices, maxTasks, maxAlerts}}
//       租户隔离：非 admin 仅能查看自己租户的配额
//   - PUT    /api/v1/quotas/{tenantID}  → {tenantID, quota, updatedAt}
//       请求体：{maxDevices, maxTasks, maxAlerts}（0=不限，须非负；仅 admin）
//   - DELETE /api/v1/quotas/{tenantID}  → {tenantID, status: "cleared"}（回退默认配额，仅 admin）
import { getJSON, putJSON, deleteJSON } from './request'

export const listQuotas = () => getJSON('/quotas')
export const getQuota = (tenantID) => getJSON(`/quotas/${encodeURIComponent(tenantID)}`)
export const setQuota = (tenantID, body) => putJSON(`/quotas/${encodeURIComponent(tenantID)}`, body)
export const deleteQuota = (tenantID) => deleteJSON(`/quotas/${encodeURIComponent(tenantID)}`)
