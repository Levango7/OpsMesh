// 告警规则相关 API
//
// Endpoint 契约（后端 internal/controlplane/alert_rule.go，mux 已注册）：
//   - GET    /alert-rules             列表 → {rules}
//   - POST   /alert-rules             创建 {name, metric, op, threshold, ...}
//   - PUT    /alert-rules/{id}        更新
//   - DELETE /alert-rules/{id}        删除（204）
//
// 多条件引擎规则：
//   - GET    /alert-rules-engine     列表 → {rules}
//   - POST   /alert-rules-engine     创建 {name, conditions, action, ...}
//   - PUT    /alert-rules-engine/{id} 更新
//   - DELETE /alert-rules-engine/{id} 删除
//
// 静默规则：
//   - GET    /alert-silences          列表 → {silences}
//   - POST   /alert-silences          创建 {matchers, startsAt, endsAt, comment}
//   - DELETE /alert-silences/{id}     删除
import { getJSON, postJSON, putJSON, deleteJSON } from './request'

// ---------- 告警规则 CRUD ----------

export const getAlertRules = () => getJSON('/alert-rules')
export const createAlertRule = (body) => postJSON('/alert-rules', body)
export const updateAlertRule = (id, body) => putJSON(`/alert-rules/${encodeURIComponent(id)}`, body)
export const deleteAlertRule = (id) => deleteJSON(`/alert-rules/${encodeURIComponent(id)}`)

// ---------- 多条件引擎规则 CRUD ----------

export const getAlertEngineRules = () => getJSON('/alert-rules-engine')
export const createAlertEngineRule = (body) => postJSON('/alert-rules-engine', body)
export const updateAlertEngineRule = (id, body) => putJSON(`/alert-rules-engine/${encodeURIComponent(id)}`, body)
export const deleteAlertEngineRule = (id) => deleteJSON(`/alert-rules-engine/${encodeURIComponent(id)}`)

// ---------- 静默规则 ----------

export const getAlertSilences = () => getJSON('/alert-silences')
export const createAlertSilence = (body) => postJSON('/alert-silences', body)
export const deleteAlertSilence = (id) => deleteJSON(`/alert-silences/${encodeURIComponent(id)}`)