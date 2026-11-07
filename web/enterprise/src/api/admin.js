// Admin / diagnostics API
import { getJSON, postJSON } from './request'

// 生效配置（脱敏快照）
export const getAdminConfig = () => getJSON('/admin/config')

// 运行时日志级别切换
export const setLogLevel = (body) => postJSON('/admin/loglevel', body)

// 诊断包下载 URL
export const getDiagnosticsUrl = () => '/admin/diagnostics'

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
export const downloadDiagnostics = () => {
  return fetch(`${getDiagnosticsUrl()}/download`, {
    method: 'POST'
  }).then(res => {
    if (!res.ok) throw new Error(res.statusText)
    return res.blob()
  }).then(blob => {
    const url = window.URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'opsmesh-diagnostics.tar.gz'
    document.body.appendChild(a)
    a.click()
    window.URL.revokeObjectURL(url)
    document.body.removeChild(a)
  })
}
