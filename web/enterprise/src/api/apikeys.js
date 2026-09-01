// API Key 管理相关 API（Phase 6：CRUD + 启停）
// 注意：POST 创建响应 {apiKey, plainKey}，plainKey 明文仅返回一次。
import { getJSON, postJSON, putJSON, deleteJSON, postEmpty } from './request'

const enc = encodeURIComponent

export const listAPIKeys = () => getJSON('/apikeys')
export const createAPIKey = (body) => postJSON('/apikeys', body)
export const getAPIKey = (id) => getJSON(`/apikeys/${enc(id)}`)
export const updateAPIKey = (id, body) => putJSON(`/apikeys/${enc(id)}`, body)
export const deleteAPIKey = (id) => deleteJSON(`/apikeys/${enc(id)}`)
export const enableAPIKey = (id) => postEmpty(`/apikeys/${enc(id)}/enable`)
export const disableAPIKey = (id) => postEmpty(`/apikeys/${enc(id)}/disable`)
