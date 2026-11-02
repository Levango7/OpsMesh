// RAG 检索/入库 store（2026-11-02 补「后端有、前端无界面」缺口）
// 后端契约（internal/ai/gateway/handler.go）：
//   POST /api/v1/ai/rag/search → 请求 {query, topK?, filter?}，响应 {documents: [VectorDocument], count}
//     VectorDocument = {id, content, embedding, metadata, score?}（rag.go:13-19）；
//     embedding 是向量本身，界面**不展示**（无意义且可能很大）。
//   POST /api/v1/ai/rag/ingest → 请求 {documents: [{id, content, metadata}]}，响应 {ingested: N}；
//     入参不带 embedding 时后端自动生成（handler.go:176-186），故 UI 只收 content/metadata。
//   GET  /api/v1/ai/health → {providers: [...]}——是 LLM 提供商可达性，不是进程存活探针。
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { t } from '@/i18n'
import { ragSearch, ragIngest, getAIHealth } from '@/api/ai'

export const useAIRagStore = defineStore('ai-rag', () => {
  const results = ref([])
  const searched = ref(false)
  const health = ref([])
  const loading = ref(false)
  const healthError = ref('')
  const error = ref('')

  async function search(query) {
    if (!query || !query.trim()) return
    loading.value = true
    error.value = ''
    try {
      const resp = await ragSearch({ query: query.trim() })
      results.value = resp.documents || []
      searched.value = true
    } catch (e) {
      results.value = []
      searched.value = false
      error.value = e.j?.error || t('rag.searchFailed')
    } finally {
      loading.value = false
    }
  }

  // UI 多行文本逐条入库；embedding 由后端生成，故只组装 id/content/metadata。
  async function ingest(text) {
    const lines = (text || '').split('\n').map((l) => l.trim()).filter(Boolean)
    if (!lines.length) return 0
    loading.value = true
    error.value = ''
    try {
      const documents = lines.map((content, i) => ({ id: `ui-${Date.now()}-${i}`, content, metadata: {} }))
      const resp = await ragIngest({ documents })
      return resp.ingested ?? documents.length
    } catch (e) {
      error.value = e.j?.error || t('rag.ingestFailed')
      return 0
    } finally {
      loading.value = false
    }
  }

  async function fetchHealth() {
    healthError.value = ''
    try {
      const resp = await getAIHealth()
      health.value = resp.providers || []
    } catch (e) {
      health.value = []
      healthError.value = e.j?.error || t('rag.health_failed')
    }
  }

  return { results, searched, health, loading, healthError, error, search, ingest, fetchHealth }
})
