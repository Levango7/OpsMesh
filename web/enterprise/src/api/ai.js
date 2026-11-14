// AI 网关 API — 对接 /api/v1/ai/*
import { getJSON, postJSON, putJSON, deleteJSON, LONG_TASK_TIMEOUT } from './request'

// 审计整改批次22：LLM 推理常超 15s 默认超时，长任务单独放宽。
export const chat = (body) => postJSON('/ai/chat', body, undefined, { timeout: LONG_TASK_TIMEOUT })
export const embed = (body) => postJSON('/ai/embed', body)
export const ragIngest = (body) => postJSON('/ai/rag/ingest', body)
export const ragSearch = (body) => postJSON('/ai/rag/search', body)
// scope='all' 请求平台全量口径（TD-97 ③④）：仅 default 租户持 platform:readall
// 时后端返回全量，否则 403（平台租户）或降级为本租户（业务租户不会被问）。
export const getAIUsage = (scope) => getJSON(scope === 'all' ? '/ai/usage?scope=all' : '/ai/usage')
export const getAIHealth = () => getJSON('/ai/health')
export const listPrompts = (category) => getJSON('/ai/prompts', category ? { category } : undefined)
export const createPrompt = (body) => postJSON('/ai/prompts', body)
export const getPrompt = (id) => getJSON(`/ai/prompts/${encodeURIComponent(id)}`)
// 后端 gateway/handler.go:374 注册的是 PUT /api/v1/ai/prompts/{id}（原实现发 POST ⇒ 405）
export const updatePrompt = (id, body) => putJSON(`/ai/prompts/${encodeURIComponent(id)}`, body)
// 后端 gateway/handler.go:378 注册的是 DELETE /api/v1/ai/prompts/{id}（原实现发 POST ⇒ 405）
export const deletePrompt = (id) => deleteJSON(`/ai/prompts/${encodeURIComponent(id)}`)

export const aiApi = {
  chat, embed, ragIngest, ragSearch, getAIUsage, getAIHealth,
  listPrompts, createPrompt, getPrompt, updatePrompt, deletePrompt
}
