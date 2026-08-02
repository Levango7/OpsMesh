// 通用 HTTP 请求封装 — 基于 axios
// 职责：基础 URL、JSON 解析、{status, data} 归一、错误透传
// 鉴权：自动附加 Authorization: Bearer <token>；401 时清除 token 并跳转登录
// X-Tenant-ID / X-User / X-User-Roles 由前置网关注入，前端不主动设置
import axios from 'axios'

export const TOKEN_KEY = 'opsmesh-token'

export const http = axios.create({
  baseURL: '/api/v1',
  timeout: 15000
})

// 请求拦截器：自动附加 JWT
http.interceptors.request.use((config) => {
  const token = localStorage.getItem(TOKEN_KEY)
  if (token) {
    config.headers = config.headers || {}
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// 响应归一：成功返回 {s, j}，与个人版 api.js 保持一致
http.interceptors.response.use(
  (resp) => ({ s: resp.status, j: resp.data }),
  (err) => {
    const s = err.response ? err.response.status : -1
    const j = err.response ? err.response.data : { error: err.message }
    // 401：token 失效，清除并跳转登录（避免在登录页本身跳转造成死循环）
    if (s === 401) {
      localStorage.removeItem(TOKEN_KEY)
      const path = window.location.pathname
      // 仅当当前不在登录/注册页时才跳转
      if (!/\/(login|register)(\/|$|\?)/.test(path)) {
        // 保留 hash 路由前缀，跳转到 /enterprise/login
        const base = import.meta.env.BASE_URL || '/'
        window.location.href = base + 'login'
      }
    }
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
// DELETE 便捷方法
export async function deleteJSON(url) {
  const { s, j } = await http.delete(url)
  return { s, j }
}
