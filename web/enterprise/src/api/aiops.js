// AIOps 相关 API — 故障根因分析 + 智能修复生成 + 自然语言运维 + 自动执行
//
// Endpoint 契约（后端 internal/aiops/handler.go，mux 已在 controlplane server_lifecycle.go 注册）：
//   - POST /api/v1/aiops/analyze      告警根因分析 → {category, component, description, confidence, evidence}
//   - POST /api/v1/aiops/remediate    生成修复方案 → {title, steps[], commands[], dagNodes[], safety}
//   - POST /api/v1/aiops/remediate-nl 自然语言转结构化任务 → {title, steps[], commands[]}
//   - POST /api/v1/aiops/execute      自动执行修复（下发任务到 agent） → {results[], dispatched, total}
//   - GET  /api/v1/aiops/status        引擎状态 → {enabled, provider, model, stats, lastErr}
import { getJSON, postJSON, LONG_TASK_TIMEOUT } from './request'

export const getAIOpsStatus = () => getJSON('/aiops/status')

// 审计整改批次22：LLM 分析走长超时（120s）
export const analyzeAIOps = (body) => postJSON('/aiops/analyze', body, undefined, { timeout: LONG_TASK_TIMEOUT })

export const generateRemediation = (body) => postJSON('/aiops/remediate', body, undefined, { timeout: LONG_TASK_TIMEOUT })

export const generateRemediationNL = (body) => postJSON('/aiops/remediate-nl', body, undefined, { timeout: LONG_TASK_TIMEOUT })

export const executeRemediation = (body) => postJSON('/aiops/execute', body, undefined, { timeout: LONG_TASK_TIMEOUT })

export const getAIOpsExecutions = () => getJSON('/aiops/executions')
