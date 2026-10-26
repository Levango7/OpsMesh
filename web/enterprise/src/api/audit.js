// 审计相关 API（Phase 3：事件查询 + 导出，均只读；P2 扩展审计检索）
// 查询参数：action / user / from(RFC3339) / to(RFC3339) / limit
//
// P2 新增：
//   - GET /audits  审计检索 → {audits: [AuditEntry], total}
//       查询参数：action / user / from(RFC3339) / to(RFC3339) / limit / offset
//
// 响应结构（逐条核对过后端 handler，不是约定）：
//   - GET /audit/events（internal/controlplane/audit_query.go handleAuditEvents，
//     111-114 行）写的是 map[string]interface{}{"events":…,"count":…}，即 {events, count}。
//     audit_query_test.go 的 TestHandleAuditEvents_* 也按 {events, count} 断言。
//   - GET /audits（internal/controlplane/server_audits.go handleAudits，108 行）
//     写的是 events 切片本身——**裸 JSON 数组**，没有 {audits, total} 包装。
//     AuditsView.vue 读 r.audits，拿到的是 undefined ⇒ 页面恒空。
//     本模块的 getAudits 因此在下面把裸数组补齐为 {audits, total}（调用方零改动）。
//
// AuditEntry 字段（proto.AuditEvent，JSON camelCase）：tenantID/userID/action/target/detail/createdAt/highRisk/traceID
import { getJSON } from './request'

// 查询审计事件，返回 {events:[], count:n}（后端 handleAuditEvents 原样包装）
export const getEvents = (params) => getJSON('/audit/events', params)

// 审计检索（P2）。
// 后端 handleAudits 直写 events 切片（裸数组），与文件头记录的 {audits, total} 契约不符。
// 这里在 API 层补齐包装：调用方（AuditsView.vue）读 r.audits，不需要改动即可拿到列表。
export const getAudits = async (params) => {
  const j = await getJSON('/audits', params)
  if (Array.isArray(j)) return { audits: j, total: j.length }
  if (j && Array.isArray(j.audits)) {
    return { audits: j.audits, total: j.total != null ? j.total : j.audits.length }
  }
  return { audits: [], total: 0 }
}

// 高危操作审计查询（P1-5C）
// 契约：GET /api/v1/audit/high-risk?tenant=&from=&to=&limit=
//   → 200 {"events":[<AuditEvent>], "count":N}
export const getHighRiskAudits = (params) => getJSON('/audit/high-risk', params)

// 哈希链校验（P1-5C）
// 契约：GET /api/v1/audit/verify?limit=
//   → 200 {supported, scope, ok, checked, fromID, toID, firstBadID, reason, ...}
//   → 409 ok=false（链校验失败）；501 backend 不支持链校验
export const verifyAuditChain = (params) => getJSON('/audit/verify', params)

// 审计导出（P2）
export const exportEvents = async (params) => {
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
