// 自动化规则相关 API
//
// Endpoint 契约（后端 internal/controlplane/automation.go，mux 已注册）：
//   - GET    /automation/rules              列表 → {rules}
//   - POST   /automation/rules              创建 AutomationRule
//   - GET    /automation/rules/{id}         详情
//   - PUT    /automation/rules/{id}         更新
//   - DELETE /automation/rules/{id}          删除
//   - POST   /automation/rules/{id}/enable   启用
//   - POST   /automation/rules/{id}/disable  禁用
//   - POST   /automation/rules/{id}/test    测试（试运行一次）
//   - GET    /automation/executions         执行历史 → {executions}
//   - GET    /automation/executions/{id}    执行详情
import { getJSON, postJSON, putJSON, deleteJSON, postEmpty } from './request'

export const getAutomationRules = () => getJSON('/automation/rules')
export const getAutomationRule = (id) => getJSON(`/automation/rules/${encodeURIComponent(id)}`)
export const createAutomationRule = (body) => postJSON('/automation/rules', body)
export const updateAutomationRule = (id, body) => putJSON(`/automation/rules/${encodeURIComponent(id)}`, body)
export const deleteAutomationRule = (id) => deleteJSON(`/automation/rules/${encodeURIComponent(id)}`)
export const enableAutomationRule = (id) => postEmpty(`/automation/rules/${encodeURIComponent(id)}/enable`)
export const disableAutomationRule = (id) => postEmpty(`/automation/rules/${encodeURIComponent(id)}/disable`)
export const testAutomationRule = (id) => postJSON(`/automation/rules/${encodeURIComponent(id)}/test`)
export const getAutomationExecutions = () => getJSON('/automation/executions')
export const getAutomationExecution = (id) => getJSON(`/automation/executions/${encodeURIComponent(id)}`)
