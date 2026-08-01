// 任务相关 API
import { getJSON, postJSON } from './request'

export const getTasks = (status) => getJSON('/tasks', status ? { status } : {})
export const createTask = (body) => postJSON('/tasks', body)
export const cancelTask = (id) => postJSON(`/tasks/${encodeURIComponent(id)}/cancel`)