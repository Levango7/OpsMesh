// OS 优化模板 API
// 契约：
//   GET  /api/v1/os-templates            → 200 OSTemplate[]（可选 ?category= 过滤）
//   GET  /api/v1/os-templates/{id}       → 200 OSTemplate
//   POST /api/v1/os-templates/{id}/execute  {agentID, params[]} → 200 task 信息
// OSTemplate 字段：id, name, category, description, commands, risk(low/medium/high), tags[], os, params[]
import { getJSON, postJSON } from './request'

export const getOSTemplates = (category) =>
  getJSON('/os-templates', category ? { category } : undefined)

export const getOSTemplate = (id) =>
  getJSON(`/os-templates/${encodeURIComponent(id)}`)

// 执行模板：在指定 agent 上执行 OS 优化模板，返回创建的 task 信息
export const executeOSTemplate = (id, agentID, params) =>
  postJSON(`/os-templates/${encodeURIComponent(id)}/execute`, {
    agentID,
    params: params || []
  })

// 兼容旧引用：聚合对象形式
export const osOptimizeApi = {
  getOSTemplates,
  getOSTemplate,
  executeOSTemplate
}