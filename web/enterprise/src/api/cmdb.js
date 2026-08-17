// CMDB 相关 API
// 对接后端 internal/cmdb/handler.go：
//   GET /api/v1/cmdb/types                        → getCMDBTypes
//   GET /api/v1/cmdb/ci?type=xxx                  → getCIs
//   POST /api/v1/cmdb/ci                          → createCI
//   GET /api/v1/cmdb/ci/{id}/graph                → getCIRelationGraph（后端 handleCIRelationGraph → GetCIRelationGraph）
//   GET /api/v1/cmdb/attr-templates?type=xxx      → getAttrTemplates
import { getJSON, postJSON } from './request'

export const getCMDBTypes = () => getJSON('/cmdb/types')
export const getCIs = (type) => getJSON('/cmdb/ci', { type })
export const createCI = (body) => postJSON('/cmdb/ci', body)

// 获取 CI 关系图谱：返回 { centerCI, relations: [{ sourceCIID, targetCIID, relationType, sourceName, targetName, targetType, ... }] }
// 对接后端 Handler.handleCIRelationGraph → store.GetCIRelationGraph
export const getCIRelationGraph = (id) => getJSON(`/cmdb/ci/${encodeURIComponent(id)}/graph`)
// 向后兼容别名：旧调用方（如 stores/cmdb.js）使用 getCIGraph
export const getCIGraph = getCIRelationGraph

export const getAttrTemplates = (type) => getJSON('/cmdb/attr-templates', { type })