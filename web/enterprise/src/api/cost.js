// 成本控制 API — 对接 /api/v1/cost/*
import { getJSON, postJSON } from './request'

export const getBudget = (tenantId) => getJSON('/cost/budget', tenantId ? { tenantId } : undefined)
export const setBudget = (body) => postJSON('/cost/budget', body)
export const getBudgetAlerts = (tenantId) => getJSON('/cost/alerts', tenantId ? { tenantId } : undefined)
export const getCostReport = (tenantId, since, format) => {
  const params = {}
  if (tenantId) params.tenantId = tenantId
  if (since) params.since = since
  if (format) params.format = format
  return getJSON('/cost/report', params)
}
export const getModelRoutes = () => getJSON('/cost/routes')
export const setModelRoutes = (body) => postJSON('/cost/routes', body)
export const queryRoute = (body) => postJSON('/cost/route', body)
export const generateReport = (body) => postJSON('/cost/report/generate', body)

export const costApi = {
  getBudget, setBudget, getBudgetAlerts, getCostReport,
  getModelRoutes, setModelRoutes, queryRoute, generateReport
}
