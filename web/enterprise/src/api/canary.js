// 灰度发布相关 API
//
// Endpoint 契约（后端 internal/controlplane/canary.go，mux 已注册）：
//   - POST /tasks/canary                  创建灰度发布 → {canaryID, status}
//       body: {serviceName, targetVersion, steps?, baselineVersion?}
//   - GET  /tasks/canary/{id}             灰度状态 → {canaryID, status, currentStep, ...}
//   - POST /tasks/canary/{id}/advance     灰度推进 → {canaryID, status, currentStep}
//   - GET  /canary/{id}/traffic-split     流量分割查询 → {canaryPercent, baselinePercent}
//   - POST /canary/{id}/traffic-split     流量分割设置 body: {canaryPercent}
//   - GET  /canary/{id}/metrics           灰度指标 → {canary: {...}, baseline: {...}}
import { getJSON, postJSON } from './request'

// 创建灰度发布
export const createCanary = (body) => postJSON('/tasks/canary', body)

// 灰度状态查询
export const getCanaryStatus = (id) => getJSON(`/tasks/canary/${encodeURIComponent(id)}`)

// 灰度推进
export const advanceCanary = (id) => postJSON(`/tasks/canary/${encodeURIComponent(id)}/advance`)

// 流量分割查询
export const getTrafficSplit = (id) => getJSON(`/canary/${encodeURIComponent(id)}/traffic-split`)

// 流量分割设置
export const setTrafficSplit = (id, body) => postJSON(`/canary/${encodeURIComponent(id)}/traffic-split`, body)

// 灰度指标查询
export const getCanaryMetrics = (id) => getJSON(`/canary/${encodeURIComponent(id)}/metrics`)