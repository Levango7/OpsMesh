// 自定义脚本相关 API
//
// Endpoint 契约（后端 internal/controlplane/script.go，mux 已注册）：
//   - GET    /scripts               列表 → {scripts}
//   - POST   /scripts               创建 {name, language: shell|python, content, params?, timeoutSec?}
//   - GET    /scripts/{id}          详情
//   - PUT    /scripts/{id}          更新
//   - DELETE /scripts/{id}          删除
//   - POST   /scripts/{id}/execute  执行 {deviceID, params?}
//   - GET    /scripts/{id}/executions 执行记录 → {executions}
import { getJSON, postJSON, putJSON, deleteJSON } from './request'

export const getScripts = () => getJSON('/scripts')
export const getScript = (id) => getJSON(`/scripts/${encodeURIComponent(id)}`)
export const createScript = (body) => postJSON('/scripts', body)
export const updateScript = (id, body) => putJSON(`/scripts/${encodeURIComponent(id)}`, body)
export const deleteScript = (id) => deleteJSON(`/scripts/${encodeURIComponent(id)}`)
export const executeScript = (id, body) => postJSON(`/scripts/${encodeURIComponent(id)}/execute`, body)
export const getScriptExecutions = (id) => getJSON(`/scripts/${encodeURIComponent(id)}/executions`)
