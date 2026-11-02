// AI 网关 API — 对接 /api/v1/ai/*
import { getJSON, postJSON, putJSON, deleteJSON } from './request'

export const chat = (body) => postJSON('/ai/chat', body)
export const embed = (body) => postJSON('/ai/embed', body)
export const ragIngest = (body) => postJSON('/ai/rag/ingest', body)
export const ragSearch = (body) => postJSON('/ai/rag/search', body)
export const getAIUsage = () => getJSON('/ai/usage')
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
