// 审批流相关 API
//
// Endpoint 契约（后端 internal/controlplane/approval.go，mux 已注册）：
//   - GET    /approval/flows                审批流列表 → {flows: [ApprovalFlow], total}
//   - POST   /approval/flows                创建审批流 → ApprovalFlow（201）
//   - GET    /approval/flows/{id}           审批流详情 → ApprovalFlow
//   - PUT    /approval/flows/{id}           更新审批流 → ApprovalFlow
//   - DELETE /approval/flows/{id}           删除审批流 → {status: "deleted"}
//   - GET    /approval/requests             审批请求列表 → {requests: [ApprovalRequest]}（?status=pending）
//   - POST   /approval/requests             创建审批请求 → ApprovalRequest（201）
//   - GET    /approval/requests/{id}        审批请求详情 → ApprovalRequest
//   - POST   /approval/requests/{id}/approve  通过审批 → ApprovalRequest
//   - POST   /approval/requests/{id}/reject   拒绝审批 → ApprovalRequest
//   - POST   /approval/requests/{id}/cancel   取消审批 → ApprovalRequest
//   - GET    /approval/requests/{id}/history  审批历史 → {history: [ApprovalHistory]}
//   - GET    /approval/pending              待我审批 → {requests: [ApprovalRequest]}
//
// 响应结构：
//   ApprovalFlow    {id, name, description, steps: [{name, approvers, order}],
//                    tenantID, createdAt, updatedAt}
//   ApprovalRequest {id, flowID, flowName, requester, resourceType, resourceID,
//                    status: "pending|approved|rejected|cancelled",
//                    currentStep, createdAt, updatedAt}
//   ApprovalHistory {id, requestID, step, approver, action: "approve|reject",
//                    comment, timestamp}
import { getJSON, postJSON, putJSON, deleteJSON } from './request'

// ---------- 审批流 CRUD ----------

export const getApprovalFlows = () => getJSON('/approval/flows')
export const createApprovalFlow = (body) => postJSON('/approval/flows', body)
export const getApprovalFlow = (id) => getJSON(`/approval/flows/${encodeURIComponent(id)}`)
export const updateApprovalFlow = (id, body) => putJSON(`/approval/flows/${encodeURIComponent(id)}`, body)
export const deleteApprovalFlow = (id) => deleteJSON(`/approval/flows/${encodeURIComponent(id)}`)

// ---------- 审批请求 CRUD + 操作 ----------

export const getApprovalRequests = (params) => getJSON('/approval/requests', params)
export const createApprovalRequest = (body) => postJSON('/approval/requests', body)
export const getApprovalRequest = (id) => getJSON(`/approval/requests/${encodeURIComponent(id)}`)
export const approveApprovalRequest = (id, body) => postJSON(`/approval/requests/${encodeURIComponent(id)}/approve`, body)
export const rejectApprovalRequest = (id, body) => postJSON(`/approval/requests/${encodeURIComponent(id)}/reject`, body)
export const cancelApprovalRequest = (id, body) => postJSON(`/approval/requests/${encodeURIComponent(id)}/cancel`, body)

// 审批历史
export const getApprovalHistory = (id) => getJSON(`/approval/requests/${encodeURIComponent(id)}/history`)

// 待我审批
export const getPendingApprovals = () => getJSON('/approval/pending')