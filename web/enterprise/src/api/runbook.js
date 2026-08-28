// Runbook 自动化 API
// 契约：
//   GET    /api/v1/runbooks                                  → 200 {runbooks: [{id,name,description,status,createdAt,updatedAt}]}
//   POST   /api/v1/runbooks           {name,description,content,triggers} → 200 Runbook
//   PUT    /api/v1/runbooks/{id}      {name,description,content,triggers} → 200 Runbook
//   DELETE /api/v1/runbooks/{id}                             → 204
//   POST   /api/v1/runbooks/{id}/execute                     → 200 {executionId,status}
//   GET    /api/v1/runbooks/{id}/executions                  → 200 {executions: [{id,status,startedAt,endedAt,logs}]}
//   GET    /api/v1/runbooks/{id}/executions/{eid}/logs       → 200 {logs: "..."}
import { getJSON, postJSON, putJSON, deleteJSON } from './request'

// ---------- Runbook CRUD ----------

export const getRunbooks = () => getJSON('/runbooks')

export const createRunbook = (name, description, content, triggers) =>
  postJSON('/runbooks', { name, description, content, triggers })

export const updateRunbook = (id, name, description, content, triggers) =>
  putJSON(`/runbooks/${encodeURIComponent(id)}`, { name, description, content, triggers })

export const deleteRunbook = (id) =>
  deleteJSON(`/runbooks/${encodeURIComponent(id)}`)

// ---------- 执行 ----------

export const executeRunbook = (id) =>
  postJSON(`/runbooks/${encodeURIComponent(id)}/execute`)

export const getRunbookExecutions = (id) =>
  getJSON(`/runbooks/${encodeURIComponent(id)}/executions`)

export const getExecutionLogs = (id, executionId) =>
  getJSON(`/runbooks/${encodeURIComponent(id)}/executions/${encodeURIComponent(executionId)}/logs`)

// 兼容旧引用：聚合对象形式
export const runbookApi = {
  getRunbooks,
  createRunbook,
  updateRunbook,
  deleteRunbook,
  executeRunbook,
  getRunbookExecutions,
  getExecutionLogs
}
