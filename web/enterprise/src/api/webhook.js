// Webhook 管理相关 API
//
// Endpoint 契约（后端 internal/controlplane/webhook.go，mux 已注册）：
//   - GET    /webhooks                    列表 → {webhooks}
//   - POST   /webhooks                    创建 {name, url, events, headers?, bodyTemplate?, enabled, ...}
//   - GET    /webhooks/{id}               详情
//   - PUT    /webhooks/{id}               更新
//   - DELETE /webhooks/{id}               删除
//   - POST   /webhooks/{id}/test          测试投递（真实 HTTP POST，返回投递结果）
//   - GET    /webhooks/{id}/deliveries    投递记录 → {deliveries}
import { getJSON, postJSON, putJSON, deleteJSON, postEmpty } from './request'

export const getWebhooks = () => getJSON('/webhooks')
export const getWebhook = (id) => getJSON(`/webhooks/${encodeURIComponent(id)}`)
export const createWebhook = (body) => postJSON('/webhooks', body)
export const updateWebhook = (id, body) => putJSON(`/webhooks/${encodeURIComponent(id)}`, body)
export const deleteWebhook = (id) => deleteJSON(`/webhooks/${encodeURIComponent(id)}`)
export const testWebhook = (id) => postEmpty(`/webhooks/${encodeURIComponent(id)}/test`)
export const getWebhookDeliveries = (id) => getJSON(`/webhooks/${encodeURIComponent(id)}/deliveries`)
