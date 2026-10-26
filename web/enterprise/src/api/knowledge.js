// 知识库 API — 对接 /api/v1/knowledge/*
import { getJSON, postJSON } from './request'

export const syncKnowledge = (body) => postJSON('/knowledge/sync', body)
export const searchKnowledge = (body) => postJSON('/knowledge/search', body)
export const ingestKnowledge = (body) => postJSON('/knowledge/ingest', body)
export const listKnowledgeDocs = (source) => getJSON('/knowledge/docs', source ? { source } : undefined)
export const deleteKnowledgeDoc = (id) => postJSON(`/knowledge/docs/${encodeURIComponent(id)}`, {})
export const getKnowledgeStats = () => getJSON('/knowledge/stats')
export const getSyncConfigs = () => getJSON('/knowledge/sync-configs')
export const updateSyncConfig = (source, body) => postJSON(`/knowledge/sync-configs/${encodeURIComponent(source)}`, body)

export const knowledgeApi = {
  syncKnowledge, searchKnowledge, ingestKnowledge, listKnowledgeDocs,
  deleteKnowledgeDoc, getKnowledgeStats, getSyncConfigs, updateSyncConfig
}
