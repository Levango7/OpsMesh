// 告警相关 API
import { getJSON, postJSON, postEmpty } from './request'

export const getAlerts = (params) => getJSON('/alerts', params)
export const ackAlert = (id) => postEmpty(`/alerts/${encodeURIComponent(id)}/ack`)
export const silenceAlert = (id, body) => postJSON(`/alerts/${encodeURIComponent(id)}/silence`, body)