// 审计相关 API（Phase 3：事件查询 + 导出，均只读；P2 扩展审计检索）
// 查询参数：action / user / from(RFC3339) / to(RFC3339) / limit
//
// P2 新增：
//   - GET /audits  审计检索 → {audits: [AuditEntry], total}
//       查询参数：action / user / from(RFC3339) / to(RFC3339) / limit / offset
//
// 响应结构：
//   AuditEntry {id, action, user, resource, tenantID, ip, userAgent, timestamp, details}
import { getJSON } from './request'

// 查询审计事件，返回 {events:[], count:n}
export const getEvents = (params) => getJSON('/audit/events', params)

// 审计检索（P2），返回 {audits: [AuditEntry], total}
export const getAudits = (params) => getJSON('/audits', params)

// 导出审计日志（后端返回纯 JSON 数组），供下载。
// 注意：使用 fetch 走 /api/v1 前缀（与 axios baseURL 保持一致），携带同源 Cookie。
export async function exportEvents(params) {
  const query = new URLSearchParams()
  for (const [k, v] of Object.entries(params || {})) {
    if (v !== '' && v != null) query.set(k, v)
  }
  const qs = query.toString()
  const url = `/api/v1/audit/export${qs ? '?' + qs : ''}`
  const resp = await fetch(url, { credentials: 'same-origin' })
  if (!resp.ok) {
    let msg = `HTTP ${resp.status}`
    try {
      const j = await resp.json()
      if (j && j.error) msg = j.error
    } catch { /* 非 JSON 错误体时保留状态码文案 */ }
    throw new Error(msg)
  }
  return resp.json()
}
