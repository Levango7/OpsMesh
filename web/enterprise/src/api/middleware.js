// 中间件部署模板 API
// 契约：
//   GET  /api/v1/middleware-templates            → 200 MiddlewareTemplate[]（可选 ?category= 过滤）
//   GET  /api/v1/middleware-templates/{id}       → 200 MiddlewareTemplate
//   POST /api/v1/middleware-templates/{id}/deploy  {agentID, deployType, params} → 200 {taskID}
//   GET  /api/v1/middleware-instances            → 200 MiddlewareInstance[]
//   POST /api/v1/middleware-instances/{instanceID}/uninstall  {agentID, deployType} → 200 {taskID}
// MiddlewareTemplate 字段：
//   id, name, category, version, description, deployTypes[], params[], scripts{docker,systemd}, risk, tags[]
// MiddlewareInstance 字段：{id, templateID, agentID, deployType, status, createdAt, ...}
import { getJSON, postJSON } from './request'

export const getMiddlewareTemplates = (category) =>
  getJSON('/middleware-templates', category ? { category } : undefined)

export const getMiddlewareTemplate = (id) =>
  getJSON(`/middleware-templates/${encodeURIComponent(id)}`)

// 部署中间件：在指定 agent 上以指定部署方式（docker/systemd）部署中间件模板
export const deployMiddleware = (id, agentID, deployType, params) =>
  postJSON(`/middleware-templates/${encodeURIComponent(id)}/deploy`, {
    agentID,
    deployType,
    params: params || {}
  })

// 查询已部署的中间件实例列表
export const getMiddlewareInstances = () =>
  getJSON('/middleware-instances')

// 卸载中间件实例
export const uninstallMiddleware = (instanceID, agentID, deployType) =>
  postJSON(`/middleware-instances/${encodeURIComponent(instanceID)}/uninstall`, {
    agentID,
    deployType
  })

// 兼容旧引用：聚合对象形式
export const middlewareApi = {
  getMiddlewareTemplates,
  getMiddlewareTemplate,
  deployMiddleware,
  getMiddlewareInstances,
  uninstallMiddleware
}