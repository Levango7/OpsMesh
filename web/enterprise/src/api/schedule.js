// 定时任务相关 API
//
// Endpoint 契约（后端 internal/controlplane/server_schedules.go，mux 已注册）：
//   - GET    /schedules            列表（?status=active|paused 过滤）→ {schedules, total}
//   - POST   /schedules            创建 {taskID, name, cronExpr} → ScheduleEntry
//   - GET    /schedules/{id}       详情 → ScheduleEntry
//   - PUT    /schedules/{id}        更新 {name, cronExpr, status}
//   - DELETE /schedules/{id}        删除
//   - POST   /schedules/{id}/pause 暂停
//   - POST   /schedules/{id}/resume 恢复
import { getJSON, postJSON, putJSON, deleteJSON, postEmpty } from './request'

export const getSchedules = (params) => getJSON('/schedules', params)
export const getSchedule = (id) => getJSON(`/schedules/${encodeURIComponent(id)}`)
export const createSchedule = (body) => postJSON('/schedules', body)
export const updateSchedule = (id, body) => putJSON(`/schedules/${encodeURIComponent(id)}`, body)
export const deleteSchedule = (id) => deleteJSON(`/schedules/${encodeURIComponent(id)}`)
export const pauseSchedule = (id) => postEmpty(`/schedules/${encodeURIComponent(id)}/pause`)
export const resumeSchedule = (id) => postEmpty(`/schedules/${encodeURIComponent(id)}/resume`)
