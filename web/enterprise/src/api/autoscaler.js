// Autoscaler 自动扩缩容 API
// 契约：
//   GET    /api/v1/autoscaler/rules                           → 200 {rules: [{id,name,metric,threshold,minReplicas,maxReplicas,cooldown,enabled,createdAt}]}
//   POST   /api/v1/autoscaler/rules    {name,metric,threshold,minReplicas,maxReplicas,cooldown} → 200 Rule
//   PUT    /api/v1/autoscaler/rules/{id}                      → 200 Rule
//   DELETE /api/v1/autoscaler/rules/{id}                      → 204
//   GET    /api/v1/autoscaler/decisions                       → 200 {decisions: [{id,ruleId,action,fromReplicas,toReplicas,timestamp,reason}]}
//   POST   /api/v1/autoscaler/scale    {target,replicas,reason} → 200 {status,message}
//   GET    /api/v1/autoscaler/cooldowns                        → 200 {cooldowns: [{ruleId,ruleName,remaining,expiresAt}]}
import { getJSON, postJSON, putJSON, deleteJSON } from './request'

// ---------- 扩缩容规则 ----------

export const getScalingRules = () => getJSON('/autoscaler/rules')

export const createScalingRule = (name, metric, threshold, minReplicas, maxReplicas, cooldown) =>
  postJSON('/autoscaler/rules', { name, metric, threshold, minReplicas, maxReplicas, cooldown })

export const updateScalingRule = (id, data) =>
  putJSON(`/autoscaler/rules/${encodeURIComponent(id)}`, data)

export const deleteScalingRule = (id) =>
  deleteJSON(`/autoscaler/rules/${encodeURIComponent(id)}`)

// ---------- 决策历史 ----------

export const getScalingDecisions = () => getJSON('/autoscaler/decisions')

// ---------- 手动触发 ----------

export const manualScale = (target, replicas, reason) =>
  postJSON('/autoscaler/scale', { target, replicas, reason })

// ---------- 冷却状态 ----------

export const getCooldowns = () => getJSON('/autoscaler/cooldowns')

// 兼容旧引用：聚合对象形式
export const autoscalerApi = {
  getScalingRules,
  createScalingRule,
  updateScalingRule,
  deleteScalingRule,
  getScalingDecisions,
  manualScale,
  getCooldowns
}
