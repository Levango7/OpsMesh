// 流量策略相关 API
//
// Endpoint 契约（后端 internal/controlplane/traffic.go，mux 已注册）：
//   - GET    /traffic/policies             列表 → {policies}
//   - POST   /traffic/policies             创建 TrafficPolicy {name, serviceName, type, ...}
//   - GET    /traffic/policies/{id}        详情
//   - PUT    /traffic/policies/{id}        更新
//   - DELETE /traffic/policies/{id}        删除
//   - POST   /traffic/policies/{id}/enable  启用
//   - POST   /traffic/policies/{id}/disable 禁用
import { getJSON, postJSON, putJSON, deleteJSON, postEmpty } from './request'

export const getTrafficPolicies = () => getJSON('/traffic/policies')
export const getTrafficPolicy = (id) => getJSON(`/traffic/policies/${encodeURIComponent(id)}`)
export const createTrafficPolicy = (body) => postJSON('/traffic/policies', body)
export const updateTrafficPolicy = (id, body) => putJSON(`/traffic/policies/${encodeURIComponent(id)}`, body)
export const deleteTrafficPolicy = (id) => deleteJSON(`/traffic/policies/${encodeURIComponent(id)}`)
export const enableTrafficPolicy = (id) => postEmpty(`/traffic/policies/${encodeURIComponent(id)}/enable`)
export const disableTrafficPolicy = (id) => postEmpty(`/traffic/policies/${encodeURIComponent(id)}/disable`)
