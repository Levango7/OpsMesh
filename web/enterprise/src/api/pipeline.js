// CI/CD 流水线相关 API
//
// 后端契约（internal/controlplane/pipeline.go，server_lifecycle.go 注册）：
//   - GET    /api/v1/pipeline/templates           → {templates: [PipelineTemplate]}
//   - POST   /api/v1/pipeline/templates           → PipelineTemplate（201）
//   - GET    /api/v1/pipeline/templates/{id}      → PipelineTemplate
//   - PUT    /api/v1/pipeline/templates/{id}      → PipelineTemplate
//   - DELETE /api/v1/pipeline/templates/{id}      → {status: "deleted"}
//   - POST   /api/v1/pipeline/templates/{id}/run  → PipelineRun（201，body 可选 {parameters}）
//   - GET    /api/v1/pipeline/runs                → {runs: [PipelineRun]}
//   - GET    /api/v1/pipeline/runs/{id}           → PipelineRun
// PipelineTemplate 字段：id/name/description/type("tekton"|"jenkins")/yaml/agentID/
//   parameters[{name,description,default,required}]/createdAt/updatedAt
// PipelineRun 字段：id/templateID/templateName/status("pending"|"running"|"succeeded"|
//   "failed"|"cancelled")/parameters/logs/startedAt/finishedAt/createdAt
import { getJSON, postJSON, putJSON, deleteJSON } from './request'

export const listTemplates = () => getJSON('/pipeline/templates')
export const createTemplate = (body) => postJSON('/pipeline/templates', body)
export const getTemplate = (id) => getJSON(`/pipeline/templates/${encodeURIComponent(id)}`)
export const updateTemplate = (id, body) => putJSON(`/pipeline/templates/${encodeURIComponent(id)}`, body)
export const deleteTemplate = (id) => deleteJSON(`/pipeline/templates/${encodeURIComponent(id)}`)
export const runTemplate = (id, body) => postJSON(`/pipeline/templates/${encodeURIComponent(id)}/run`, body)
export const listRuns = () => getJSON('/pipeline/runs')
export const getRun = (id) => getJSON(`/pipeline/runs/${encodeURIComponent(id)}`)
