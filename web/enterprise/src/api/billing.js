// 计费相关 API（Phase 6：订阅计划 / 订阅 / 账单 / 用量统计）
import { getJSON, postJSON, putJSON, deleteJSON } from './request'

const enc = encodeURIComponent

// —— 订阅计划 ——
export const listPlans = () => getJSON('/billing/plans')
export const createPlan = (body) => postJSON('/billing/plans', body)
export const getPlan = (id) => getJSON(`/billing/plans/${enc(id)}`)
export const updatePlan = (id, body) => putJSON(`/billing/plans/${enc(id)}`, body)
export const deletePlan = (id) => deleteJSON(`/billing/plans/${enc(id)}`)

// —— 订阅 ——
export const listSubscriptions = () => getJSON('/billing/subscriptions')
export const createSubscription = (body) => postJSON('/billing/subscriptions', body)
export const updateSubscription = (id, body) => putJSON(`/billing/subscriptions/${enc(id)}`, body)
export const deleteSubscription = (id) => deleteJSON(`/billing/subscriptions/${enc(id)}`)

// —— 账单（只读） ——
export const listInvoices = () => getJSON('/billing/invoices')
export const getInvoice = (id) => getJSON(`/billing/invoices/${enc(id)}`)

// —— 用量统计（只读） ——
export const getUsage = () => getJSON('/billing/usage')
