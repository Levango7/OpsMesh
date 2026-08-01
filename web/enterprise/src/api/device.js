// 设备相关 API
import { getJSON, postEmpty } from './request'

export const getDevices = () => getJSON('/devices')
export const getDevice = (id) => getJSON(`/devices/${encodeURIComponent(id)}`)
export const provisionDevice = (id) => postEmpty(`/devices/${encodeURIComponent(id)}/provision`)
export const getAgents = () => getJSON('/agents')