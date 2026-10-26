// 任务相关 API
import { getJSON, postJSON } from './request'

export const getTasks = (status) => getJSON('/tasks', status ? { status } : {})
export const createTask = (body) => postJSON('/tasks', body)
export const cancelTask = (id) => postJSON(`/tasks/${encodeURIComponent(id)}/cancel`)
export const approveTask = (id) => postJSON(`/tasks/${encodeURIComponent(id)}/approve`)
export const rejectTask = (id) => postJSON(`/tasks/${encodeURIComponent(id)}/reject`)

// 任务详情查询（用于执行/部署/卸载日志轮询）
// 契约：GET /api/v1/tasks/{taskID} → 200 {taskID, status, output, ...}
//   status: pending / running / completed / failed
//   output: 任务执行 stdout/stderr 拼接文本
export const getTaskDetail = (taskID) => getJSON(`/tasks/${encodeURIComponent(taskID)}`)