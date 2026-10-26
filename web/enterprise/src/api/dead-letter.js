// 死信队列（DLQ）API
// 契约（internal/controlplane/server_tasks.go）：
//   GET    /api/v1/tasks/dead-letter → 死信任务列表（DeadLetter=true 且 status=failed）
//   POST   /api/v1/tasks/{id}/retry → 重新入队（重置 DeadLetter=false, status=pending, retry_count=0）
//   DELETE /api/v1/tasks/{id}/dead-letter → 移除死信任务
import { getJSON, postJSON, deleteJSON } from './request'

export const listDeadLetterTasks = (params) => getJSON('/tasks/dead-letter', params)
export const retryDeadLetterTask = (id) => postJSON(`/tasks/${encodeURIComponent(id)}/retry`)
export const purgeDeadLetterTask = (id) => deleteJSON(`/tasks/${encodeURIComponent(id)}/dead-letter`)
