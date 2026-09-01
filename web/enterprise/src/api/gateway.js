// API 网关相关 API（Phase 5：路由规则 CRUD + 启停 + 统计）
import { getJSON, postJSON, putJSON, deleteJSON, postEmpty } from './request'

const enc = encodeURIComponent

// —— 路由规则 ——
export const listRoutes = () => getJSON('/gateway/routes')
export const createRoute = (body) => postJSON('/gateway/routes', body)
export const getRoute = (id) => getJSON(`/gateway/routes/${enc(id)}`)
export const updateRoute = (id, body) => putJSON(`/gateway/routes/${enc(id)}`, body)
export const deleteRoute = (id) => deleteJSON(`/gateway/routes/${enc(id)}`)
export const enableRoute = (id) => postEmpty(`/gateway/routes/${enc(id)}/enable`)
export const disableRoute = (id) => postEmpty(`/gateway/routes/${enc(id)}/disable`)

// —— 网关统计（只读） ——
export const getStats = () => getJSON('/gateway/stats')
