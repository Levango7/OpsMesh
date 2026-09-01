// 安全合规相关 API
//
// 后端契约（internal/controlplane/compliance.go，server_lifecycle.go 注册）：
//   - GET /api/v1/compliance/rules       → {rules: [ComplianceRule]}
//   - GET /api/v1/compliance/rules/{id}  → ComplianceRule
//   - POST /api/v1/compliance/scan       → ComplianceReport（201）
//       请求体：{deviceID: "...", results?: [{ruleId, passed, output}]}
//       results 为空时后端用引擎规则生成占位结果（全 failed，供测试/演示）
//   - GET /api/v1/compliance/reports     → {reports: [ComplianceReport]}
//   - GET /api/v1/compliance/reports/{id} → ComplianceReport
// ComplianceRule 字段：id/name/category("cis"|"pci_dss"|"hipaa"|"custom")/
//   severity("high"|"medium"|"low")/description/checkScript/remediation
// ComplianceReport 字段：id/deviceID/results[{ruleId,passed,output,checkedAt}]/
//   score(0-100)/createdAt
import { getJSON, postJSON } from './request'

export const listRules = () => getJSON('/compliance/rules')
export const getRule = (id) => getJSON(`/compliance/rules/${encodeURIComponent(id)}`)
export const scanDevice = (body) => postJSON('/compliance/scan', body)
export const listReports = () => getJSON('/compliance/reports')
export const getReport = (id) => getJSON(`/compliance/reports/${encodeURIComponent(id)}`)
