// 批量运维相关 API
//
// Endpoint 契约（后端 internal/controlplane/batch.go，mux 已注册）：
//   - POST /tasks/batch-exec  批量执行 → {batchID, tasks: [{taskID, agentID, status}]}
//       body: {agentIDs: [], type: 'shell'|'file'|'service', command, path?, content?}
//   - GET  /tasks/batch/{id}  批量状态查询 → {batchID, status, total, succeeded, failed, tasks: [...]}
//   - POST /tasks/batch        批量下发（旧版兼容） → 同 batch-exec
import { getJSON, postJSON } from './request'

// 批量执行（推荐）
export const batchExec = (body) => postJSON('/tasks/batch-exec', body)

// 批量状态查询
export const getBatchStatus = (id) => getJSON(`/tasks/batch/${encodeURIComponent(id)}`)

// 批量下发（旧版兼容）
export const batchDispatch = (body) => postJSON('/tasks/batch', body)