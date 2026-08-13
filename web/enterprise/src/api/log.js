// 日志检索相关 API
import { getJSON } from './request'

// 简单 keyword 检索（向后兼容）
//   params: { deviceID, agentID, level, source, keyword, from, to, limit, offset }
export const getLogs = (params) => getJSON('/logs', params)

// 结构化查询语法检索（KQL/Lucene 风格）
//   params: { q, limit, offset, from, to }
//   q 示例: 'level=error AND device=dev-1 AND message~"panic"'
// 后端解析失败时返回 400，调用方需捕获 { s: 400, j: { error } }
export const queryLogs = (params) => getJSON('/logs', params)

export default { getLogs, queryLogs }
