// 设备相关 API
import { getJSON, postEmpty } from './request'

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
