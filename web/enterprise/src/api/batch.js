// 批量运维相关 API
//
// Endpoint 契约（后端 internal/controlplane/batch.go，mux 已注册）：
//   - POST /tasks/batch-exec  批量执行 → {batchID, tasks: [{taskID, agentID, status}]}
//       body: {agentIDs: [], type: 'shell'|'file'|'service', command, path?, content?}
//   - GET  /tasks/batch/{id}  批量状态查询 → {batchID, status, total, succeeded, failed, tasks: [...]}
//
// 2026-10-11 移除 `POST /tasks/batch`（批量下发）死路（TD-60 task 域施工图 §2.2）：
// 该端点在单体侧用 `targets` 字段、逐台建任务且不返回 batchID，而 store 侧
// fetchStatus/dispatch 一律读 batchID——契约本就裂着；且全仓无 view 调用、
// e2e-real 不打，唯一调用方是它自己的单测。留着它只会让下一个人以为
// 「批量下发是一条活路」并照着写新代码。
import { getJSON, postJSON } from './request'

// 批量执行
export const batchExec = (body) => postJSON('/tasks/batch-exec', body)

// 批量状态查询
export const getBatchStatus = (id) => getJSON(`/tasks/batch/${encodeURIComponent(id)}`)