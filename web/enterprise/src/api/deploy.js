// 部署相关 API
import { getJSON, postJSON, postEmpty } from './request'

export const getDeploys = (status) => getJSON('/deploys', status ? { status } : {})
export const createDeploy = (body) => postJSON('/deploys', body)
export const executeDeploy = (id) => postEmpty(`/deploys/${encodeURIComponent(id)}/execute`)
export const rollbackDeploy = (id) => postEmpty(`/deploys/${encodeURIComponent(id)}/rollback`)
export const getDeploy = (id) => getJSON(`/deploys/${encodeURIComponent(id)}`)