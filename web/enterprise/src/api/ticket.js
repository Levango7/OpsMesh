// 工单相关 API
//
// Endpoint 契约（后端 internal/controlplane/ticket.go，mux 已注册）：
//   - GET    /tickets            列表（?status=&priority=&category=&assigneeID= 过滤）→ {tickets}
//   - POST    /tickets           创建 {title, description, priority, category, assigneeID, ...}
//   - GET    /tickets/{id}       详情
//   - PUT    /tickets/{id}       更新 {title, description, status, priority, category, assigneeID, ...}
//   - POST   /tickets/{id}/close 关闭工单
import { getJSON, postJSON, putJSON, postEmpty } from './request'

export const getTickets = (params) => getJSON('/tickets', params)
export const getTicket = (id) => getJSON(`/tickets/${encodeURIComponent(id)}`)
export const createTicket = (body) => postJSON('/tickets', body)
export const updateTicket = (id, body) => putJSON(`/tickets/${encodeURIComponent(id)}`, body)
export const closeTicket = (id) => postEmpty(`/tickets/${encodeURIComponent(id)}/close`)
