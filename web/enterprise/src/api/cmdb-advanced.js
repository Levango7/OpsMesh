// CMDB 高级相关 API（变更审批 / 关系 / CI 采集与审批）
//
// Endpoint 契约（后端 internal/controlplane/cmdb_advanced.go，mux 已注册）：
//   - POST   /cmdb/collect              手动触发采集 → {collected, failed}（201）
//   - GET    /cmdb/changes              变更列表 → {changes: [CMDBChange]}
//   - POST   /cmdb/changes              创建变更 → CMDBChange（201）
//   - GET    /cmdb/changes/{id}         变更详情 → CMDBChange
//   - POST   /cmdb/changes/{id}/approve 审批通过 → CMDBChange
//   - POST   /cmdb/changes/{id}/reject  审批拒绝 → CMDBChange
//   - GET    /cmdb/relations            关系列表 → {relations: [Relation]}
//   - POST   /cmdb/relations            创建关系 → Relation（201）
//   - GET    /cmdb/ci/export            导出 CI → [CiItem]
//   - POST   /cmdb/ci/import            导入 CI → {imported, failed}
//   - GET    /cmdb/ci/pending           待审批 CI → {items: [CiItem]}
//   - GET    /cmdb/ci/{id}/relations    CI 关系列表 → {relations: [Relation]}
//   - PUT    /cmdb/ci/{id}              更新 CI → CiItem
//   - DELETE /cmdb/ci/{id}              删除 CI → {status: "deleted"}
//   - POST   /cmdb/ci/{id}/approve      审批通过 CI → CiItem
//   - POST   /cmdb/ci/{id}/reject       审批拒绝 CI → CiItem
//
// 响应结构：
//   CMDBChange {id, ciID, changeType: "create|update|delete", before, after,
//               status: "pending|approved|rejected", requester, approver,
//               createdAt, approvedAt}
//   Relation   {id, sourceCIID, targetCIID, relationType, sourceName, targetName, targetType}
import { getJSON, postJSON, putJSON, deleteJSON } from './request'

// ---------- 采集 ----------

// 手动触发 CMDB 采集
export const collectCMDB = () => postJSON('/cmdb/collect')

// ---------- 变更审批 ----------

// 变更列表
export const getCMDBChanges = (params) => getJSON('/cmdb/changes', params)

// 创建变更
export const createCMDBChange = (body) => postJSON('/cmdb/changes', body)

// 变更详情
export const getCMDBChange = (id) => getJSON(`/cmdb/changes/${encodeURIComponent(id)}`)

// 审批通过
export const approveCMDBChange = (id) => postJSON(`/cmdb/changes/${encodeURIComponent(id)}/approve`)

// 审批拒绝
export const rejectCMDBChange = (id) => postJSON(`/cmdb/changes/${encodeURIComponent(id)}/reject`)

// ---------- 关系 ----------

// 关系列表
export const getRelations = (params) => getJSON('/cmdb/relations', params)

// 创建关系
export const createRelation = (body) => postJSON('/cmdb/relations', body)

// ---------- CI 导入导出与审批 ----------

// 导出 CI
export const exportCIs = (params) => getJSON('/cmdb/ci/export', params)

// 导入 CI
export const importCIs = (body) => postJSON('/cmdb/ci/import', body)

// 待审批 CI 列表
export const getPendingCIs = () => getJSON('/cmdb/ci/pending')

// CI 的关系列表
export const getCIRelations = (id) => getJSON(`/cmdb/ci/${encodeURIComponent(id)}/relations`)

// 更新 CI
export const updateCI = (id, body) => putJSON(`/cmdb/ci/${encodeURIComponent(id)}`, body)

// 删除 CI
export const deleteCI = (id) => deleteJSON(`/cmdb/ci/${encodeURIComponent(id)}`)

// 审批通过 CI
export const approveCI = (id) => postJSON(`/cmdb/ci/${encodeURIComponent(id)}/approve`)

// 审批拒绝 CI
export const rejectCI = (id) => postJSON(`/cmdb/ci/${encodeURIComponent(id)}/reject`)