// 通用 HTTP 请求封装 — 基于 axios
// 职责：基础 URL、JSON 解析、{status, data} 归一、错误透传
// 鉴权：采用双 HttpOnly Cookie（at+rt）同源自动携带，前端不持有令牌（防 XSS 窃取）；
// at 过期（401）时静默调用 /auth/refresh 换新 at+rt 并重试一次；刷新失败则清会话跳登录。
// X-Tenant-ID / X-User / X-User-Roles 由前置网关注入，前端不主动设置。
import axios from 'axios'

export const http = axios.create({
  baseURL: '/api/v1',
  timeout: 15000
})

// 未授权回调（由 auth store 注册）：刷新也失败时清会话并跳转登录。
let unauthorizedHandler = null
export function setUnauthorizedHandler(fn) {
  unauthorizedHandler = fn
}

// 跳登录（避免在登录/注册页本身跳转造成死循环）
function redirectToLogin() {
  const base = import.meta.env.BASE_URL || '/'
  if (!/\/(login|register)(\/|$|\?)/.test(window.location.pathname)) {
    window.location.href = base + 'login'
  }
}

let refreshing = null

// 请求拦截器：双 Cookie 由浏览器自动携带，无需手动附加 Authorization。
http.interceptors.request.use((config) => config)

// 响应归一：成功返回 {s, j}，与个人版 api.js 保持一致
http.interceptors.response.use(
  (resp) => ({ s: resp.status, j: resp.data }),
  async (err) => {
    const s = err.response ? err.response.status : -1
    const j = err.response ? err.response.data : { error: err.message }
    const original = err.config
    // at 过期（401）时静默刷新后重试一次；刷新失败则清会话并跳登录。
    if (s === 401 && original && !original._retry) {
      original._retry = true
      try {
        if (!refreshing) refreshing = postEmpty('/auth/refresh')
        await refreshing
        refreshing = null
        return await http(original) // 重试原请求（自动携带新 Cookie）
      } catch (e2) {
        refreshing = null
        if (unauthorizedHandler) unauthorizedHandler()
        else redirectToLogin()
        return Promise.reject({ s: 401, j })
      }
    }
    if (s === 401) {
      if (unauthorizedHandler) unauthorizedHandler()
      else redirectToLogin()
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
