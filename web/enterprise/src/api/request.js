// 通用 HTTP 请求封装 — 基于 axios
// 职责：基础 URL、JSON 解析、{status, data} 归一、错误透传
// X-Tenant-ID / X-User / X-User-Roles 由前置网关注入，前端不主动设置
import axios from 'axios'

export const http = axios.create({
  baseURL: '/api/v1',
  timeout: 15000
})

// 响应归一：成功返回 {s, j}，与个人版 api.js 保持一致
http.interceptors.response.use(
  (resp) => ({ s: resp.status, j: resp.data }),
  (err) => {
    const s = err.response ? err.response.status : -1
    const j = err.response ? err.response.data : { error: err.message }
    return Promise.reject({ s, j })
  }
)

// 便捷方法：返回原始 data（j），失败抛错
export async function getJSON(url, params) {
  const { j } = await http.get(url, { params })
  return j
}
export async function postJSON(url, body) {
  const { s, j } = await http.post(url, body)
  return { s, j }
}
export async function putJSON(url, body) {
  const { s, j } = await http.put(url, body)
  return { s, j }
}
export async function postEmpty(url) {
  const { s, j } = await http.post(url)
  return { s, j }
}