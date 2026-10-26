// 设备相关 API
import { getJSON, postJSON, postEmpty } from './request'

export const getDevices = () => getJSON('/devices')
export const getDevice = (id) => getJSON(`/devices/${encodeURIComponent(id)}`)
export const provisionDevice = (id) => postEmpty(`/devices/${encodeURIComponent(id)}/provision`)
export const getAgents = () => getJSON('/agents')

// 设备监控指标聚合 API
export const getMetrics = (id) => getJSON(`/devices/${encodeURIComponent(id)}/metrics`)

// 兼容旧引用：聚合对象形式
export const deviceApi = {
  getDevices,
  getDevice,
  provisionDevice,
  getAgents,
  getMetrics
}

// ============ 设备发现 API (device-svc HTTP 网关，经 service_proxy 转发) ============

// 开始发现任务：POST /device-svc/discovery/jobs
export const startDiscovery = (tenantID, cidr) =>
  postJSON('/device-svc/discovery/jobs', { tenantID, cidr })

// 获取发现任务状态：GET /device-svc/discovery/jobs/{jobID}
export const getDiscoveryJob = (jobID) =>
  getJSON(`/device-svc/discovery/jobs/${encodeURIComponent(jobID)}`)

// 列出已发现设备：GET /device-svc/discovery/devices?tenantID={tenantID}
export const getDiscoveredDevices = (tenantID) =>
  getJSON('/device-svc/discovery/devices', tenantID ? { tenantID } : undefined)

// ============ 自动纳管 API ============

// 自动纳管：POST /device-svc/provision/auto
export const autoProvision = (tenantID, cidrs) =>
  postJSON('/device-svc/provision/auto', { tenantID, cidrs })

