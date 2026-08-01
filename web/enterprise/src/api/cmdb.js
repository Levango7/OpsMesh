// CMDB 相关 API
import { getJSON, postJSON } from './request'

export const getCMDBTypes = () => getJSON('/cmdb/types')
export const getCIs = (type) => getJSON('/cmdb/ci', { type })
export const createCI = (body) => postJSON('/cmdb/ci', body)
export const getCIGraph = (id) => getJSON(`/cmdb/ci/${encodeURIComponent(id)}/graph`)
export const getAttrTemplates = (type) => getJSON('/cmdb/attr-templates', { type })