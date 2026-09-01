// SLO 目标相关 API
//
// Endpoint 契约（后端 internal/controlplane/slo.go，mux 已注册）：
//   - GET    /slos             列表 → {slos}
//   - POST   /slos             创建 {name, description, serviceName, target, window, slis?}
//   - GET    /slos/{id}        详情
//   - PUT    /slos/{id}        更新
//   - DELETE /slos/{id}        删除（204）
//   - GET    /slos/{id}/status SLI 当前状态
import { getJSON, postJSON, putJSON, deleteJSON } from './request'

export const getSLOs = () => getJSON('/slos')
export const getSLO = (id) => getJSON(`/slos/${encodeURIComponent(id)}`)
export const createSLO = (body) => postJSON('/slos', body)
export const updateSLO = (id, body) => putJSON(`/slos/${encodeURIComponent(id)}`, body)
export const deleteSLO = (id) => deleteJSON(`/slos/${encodeURIComponent(id)}`)
export const getSLOStatus = (id) => getJSON(`/slos/${encodeURIComponent(id)}/status`)
