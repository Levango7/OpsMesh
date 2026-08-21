// 作业编排相关 API
import { getJSON, postJSON, putJSON, postEmpty } from './request'

export const getWorkflows = () => getJSON('/workflows')
export const getWorkflow = (id) => getJSON(`/workflows/${encodeURIComponent(id)}`)
export const createWorkflow = (body) => postJSON('/workflows', body)
export const updateWorkflow = (id, body) => putJSON(`/workflows/${encodeURIComponent(id)}`, body)
export const runWorkflow = (id) => postEmpty(`/workflows/${encodeURIComponent(id)}/run`)
export const getWorkflowStatus = (id) => getJSON(`/workflows/${encodeURIComponent(id)}/status`)
export const scheduleWorkflow = (id, cron) => postJSON(`/workflows/${encodeURIComponent(id)}/schedule`, { cron })