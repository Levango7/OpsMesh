// 部署相关 API
import { getJSON, postJSON, postEmpty } from './request'

export const getDeploys = (status) => getJSON('/deploys', status ? { status } : {})
export const createDeploy = (body) => postJSON('/deploys', body)
export const executeDeploy = (id) => postEmpty(`/deploys/${id}/execute`)
export const rollbackDeploy = (id) => postEmpty(`/deploys/${id}/rollback`)
export const getDeploy = (id) => getJSON(`/deploys/${id}`)