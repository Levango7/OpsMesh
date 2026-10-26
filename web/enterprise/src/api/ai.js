// AI 网关 API — 对接 /api/v1/ai/*
import { getJSON, postJSON } from './request'

export const chat = (body) => postJSON('/ai/chat', body)
export const embed = (body) => postJSON('/ai/embed', body)
export const ragIngest = (body) => postJSON('/ai/rag/ingest', body)
export const ragSearch = (body) => postJSON('/ai/rag/search', body)
export const getAIUsage = () => getJSON('/ai/usage')
export const getAIHealth = () => getJSON('/ai/health')
export const listPrompts = (category) => getJSON('/ai/prompts', category ? { category } : undefined)
export const createPrompt = (body) => postJSON('/ai/prompts', body)
export const getPrompt = (id) => getJSON(`/ai/prompts/${encodeURIComponent(id)}`)
export const updatePrompt = (id, body) => postJSON(`/ai/prompts/${encodeURIComponent(id)}`, body)
export const deletePrompt = (id) => postJSON(`/ai/prompts/${encodeURIComponent(id)}`, {})

export const aiApi = {
  chat, embed, ragIngest, ragSearch, getAIUsage, getAIHealth,
  listPrompts, createPrompt, getPrompt, updatePrompt, deletePrompt
}
