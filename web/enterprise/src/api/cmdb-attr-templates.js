// CMDB 属性模板相关 API
//
// Endpoint 契约（后端 internal/controlplane/cmdb_attr_templates.go，mux 已注册）：
//   - GET    /cmdb/attr-templates        属性模板列表 → {templates: [AttrTemplate]}
//   - POST   /cmdb/attr-templates        创建属性模板 → AttrTemplate（201）
//   - GET    /cmdb/attr-templates/{id}   属性模板详情 → AttrTemplate
//   - PUT    /cmdb/attr-templates/{id}   更新属性模板 → AttrTemplate
//   - DELETE /cmdb/attr-templates/{id}   删除属性模板 → {status: "deleted"}
//
// 响应结构：
//   AttrTemplate {id, name, type, category, required, defaultValue, options, createdAt}
import { getJSON, postJSON, putJSON, deleteJSON } from './request'

// 属性模板列表
export const getAttrTemplates = () => getJSON('/cmdb/attr-templates')

// 创建属性模板
export const createAttrTemplate = (body) => postJSON('/cmdb/attr-templates', body)

// 属性模板详情
export const getAttrTemplate = (id) => getJSON(`/cmdb/attr-templates/${encodeURIComponent(id)}`)

// 更新属性模板
export const updateAttrTemplate = (id, body) => putJSON(`/cmdb/attr-templates/${encodeURIComponent(id)}`, body)

// 删除属性模板
export const deleteAttrTemplate = (id) => deleteJSON(`/cmdb/attr-templates/${encodeURIComponent(id)}`)