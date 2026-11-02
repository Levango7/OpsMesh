// 知识库 API — 对接 /api/v1/knowledge/*
import { getJSON, postJSON, putJSON, deleteJSON } from './request'

export const syncKnowledge = (body) => postJSON('/knowledge/sync', body)
export const searchKnowledge = (body) => postJSON('/knowledge/search', body)
export const ingestKnowledge = (body) => postJSON('/knowledge/ingest', body)
export const listKnowledgeDocs = (source) => getJSON('/knowledge/docs', source ? { source } : undefined)
// 后端 knowledge/handler.go:339 注册的是 DELETE /api/v1/knowledge/docs/（原实现发 POST ⇒ 405）
export const deleteKnowledgeDoc = (id) => deleteJSON(`/knowledge/docs/${encodeURIComponent(id)}`)
export const getKnowledgeStats = () => getJSON('/knowledge/stats')
export const getSyncConfigs = () => getJSON('/knowledge/sync-configs')
// 后端 knowledge/handler.go:345 注册的是 PUT /api/v1/knowledge/sync-configs/（原实现发 POST ⇒ 405）
export const updateSyncConfig = (source, body) => putJSON(`/knowledge/sync-configs/${encodeURIComponent(source)}`, body)

export const knowledgeApi = {
  syncKnowledge, searchKnowledge, ingestKnowledge, listKnowledgeDocs,
  deleteKnowledgeDoc, getKnowledgeStats, getSyncConfigs, updateSyncConfig
}
