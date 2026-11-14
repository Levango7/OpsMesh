// Admin / diagnostics API
import { getJSON, postJSON, refreshToken } from './request'

// 生效配置（脱敏快照）
export const getAdminConfig = () => getJSON('/admin/config')

// 运行时日志级别切换
export const setLogLevel = (body) => postJSON('/admin/loglevel', body)

// 诊断包下载 URL（绝对路径：raw fetch 不走 axios baseURL）
export const getDiagnosticsUrl = () => '/api/v1/admin/diagnostics'

// store 写入失败列表
export const getStoreFailures = (params) => getJSON('/admin/store-failures', params)

// TD-60 各域路由表
export const getServiceRouting = () => getJSON('/admin/service-routing')

// TD-60 各域流量计数器
export const getServiceTraffic = () => getJSON('/admin/service-traffic')

export const adminApi = {
  getAdminConfig,
  setLogLevel,
  getDiagnosticsUrl,
  getStoreFailures,
  getServiceRouting,
  getServiceTraffic,
  downloadDiagnostics
}

// 诊断包下载
//
// 审计整改批次20：原实现请求 `POST /admin/diagnostics/download`，而后端只有
// `GET /api/v1/admin/diagnostics`（application/zip）⇒ 管理台点击必然 404/405。
// 现在：GET + 同源 Cookie；401 先 refresh 再重放一次（与主链路会话续期一致）；
// 失败时尽量读取 JSON error 文案。
export const downloadDiagnostics = async () => {
  const url = getDiagnosticsUrl()
  let res = await fetch(url, { method: 'GET', credentials: 'same-origin' })
  if (res.status === 401) {
    await refreshToken()
    res = await fetch(url, { method: 'GET', credentials: 'same-origin' })
  }
  if (!res.ok) {
    let msg = `HTTP ${res.status}`
    try {
      const j = await res.json()
      if (j && j.error) msg = j.error
    } catch {
      // 非 JSON 错误体：保留状态码文案
    }
    throw new Error(msg)
  }
  const blob = await res.blob()
  const objUrl = window.URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = objUrl
  a.download = 'opsmesh-diagnostics.zip'
  document.body.appendChild(a)
  a.click()
  window.URL.revokeObjectURL(objUrl)
  document.body.removeChild(a)
}
