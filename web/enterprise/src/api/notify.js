// 通知渠道/模板相关 API（M2：渠道 CRUD + 测试发送；模板 CRUD）
import { getJSON, postJSON, putJSON, deleteJSON, postEmpty } from './request'

const enc = encodeURIComponent

// —— 通知渠道 ——
export const listChannels = () => getJSON('/notify-channels')
export const createChannel = (body) => postJSON('/notify-channels', body)
export const updateChannel = (id, body) => putJSON(`/notify-channels/${enc(id)}`, body)
export const deleteChannel = (id) => deleteJSON(`/notify-channels/${enc(id)}`)
// 测试发送：body 可选 {title, body}，缺省时后端用内置测试消息
export const testChannel = (id, body) => postEmpty(`/notify-channels/${enc(id)}/test`, body)

// —— 通知模板 ——
export const listTemplates = () => getJSON('/notify-templates')
export const createTemplate = (body) => postJSON('/notify-templates', body)
export const updateTemplate = (id, body) => putJSON(`/notify-templates/${enc(id)}`, body)
export const deleteTemplate = (id) => deleteJSON(`/notify-templates/${enc(id)}`)
