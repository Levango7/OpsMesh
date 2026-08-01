// 日志检索相关 API
import { getJSON } from './request'

export const getLogs = (params) => getJSON('/logs', params)