// Portal 自助服务门户 API
// 契约：
//   POST   /api/v1/portal/requests     {type,resource,params,reason} → 200 Request
//   GET    /api/v1/portal/requests     ?status={s}                    → 200 {requests: [{id,type,resource,status,requester,createdAt,approvedAt,cost}]}
//   GET    /api/v1/portal/approvals                                   → 200 {requests: [{id,type,resource,requester,reason,createdAt}]}
//   POST   /api/v1/portal/approvals/{id}/approve                      → 200 {status}
//   POST   /api/v1/portal/approvals/{id}/reject  {reason}             → 200 {status}
//   GET    /api/v1/portal/cost                                       → 200 {total,trend: [{date,amount}],byCategory: [{category,amount}]}
import { getJSON, postJSON } from './request'

// ---------- 资源请求 ----------

export const createResourceRequest = (type, resource, params, reason) =>
  postJSON('/portal/requests', { type, resource, params, reason })

export const getMyRequests = (status) =>
  getJSON('/portal/requests', status ? { status } : undefined)

// ---------- 审批 ----------

export const getApprovalQueue = () => getJSON('/portal/approvals')

export const approveRequest = (id) =>
  postJSON(`/portal/approvals/${encodeURIComponent(id)}/approve`)

export const rejectRequest = (id, reason) =>
  postJSON(`/portal/approvals/${encodeURIComponent(id)}/reject`, { reason })

// ---------- 成本 ----------

export const getCostOverview = () => getJSON('/portal/cost')

// 兼容旧引用：聚合对象形式
export const portalApi = {
  createResourceRequest,
  getMyRequests,
  getApprovalQueue,
  approveRequest,
  rejectRequest,
  getCostOverview
}
