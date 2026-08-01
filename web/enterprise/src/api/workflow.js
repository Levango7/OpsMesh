// 作业编排相关 API
import { getJSON, postJSON, putJSON, postEmpty } from './request'

export const getWorkflows = () => getJSON('/workflows')
export const getWorkflow = (id) => getJSON(`/workflows/${encodeURIComponent(id)}`)
export const createWorkflow = (body) => postJSON('/workflows', body)
export const updateWorkflow = (id, body) => putJSON(`/workflows/${id}`, body)
export const runWorkflow = (id) => postEmpty(`/workflows/${id}/run`)
export const getWorkflowStatus = (id) => getJSON(`/workflows/${id}/status`)
export const scheduleWorkflow = (id, cron) => postJSON(`/workflows/${id}/schedule`, { cron })