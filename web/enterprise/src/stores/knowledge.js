// 知识库 store
import { defineStore } from 'pinia'
import { syncKnowledge, searchKnowledge, ingestKnowledge, listKnowledgeDocs, deleteKnowledgeDoc, getKnowledgeStats, getSyncConfigs, updateSyncConfig } from '@/api/knowledge'
import { t } from '@/i18n'

export const useKnowledgeStore = defineStore('knowledge', {
  state: () => ({
    documents: [],
    searchResults: [],
    stats: null,
    syncConfigs: [],
    loading: false,
    searchLoading: false,
    error: '',
    searchQuery: ''
  }),
  actions: {
    async sync(source) {
      this.loading = true
      this.error = ''
      try {
        const result = await syncKnowledge(source ? { source } : {})
        await this.fetchDocs()
        await this.fetchStats()
        return result
      } catch (e) {
        this.error = e.j?.error || t('error.knowledgeSyncFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    async search(query, rerank) {
      this.searchLoading = true
      this.searchQuery = query
      try {
        const result = await searchKnowledge({ query, rerank: rerank || false })
        this.searchResults = result.documents || []
        return result
      } catch (e) {
        this.error = e.j?.error || t('error.knowledgeSearchFailed')
        this.searchResults = []
        throw e
      } finally {
        this.searchLoading = false
      }
    },
    async ingest(docs) {
      this.loading = true
      try {
        const result = await ingestKnowledge({ documents: docs })
        await this.fetchDocs()
        return result
      } catch (e) {
        this.error = e.j?.error || t('error.knowledgeIngestFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    async fetchDocs(source) {
      this.loading = true
      try {
        const result = await listKnowledgeDocs(source)
        this.documents = result.documents || []
      } catch (e) {
        this.error = e.j?.error || t('error.knowledgeListFailed')
      } finally {
        this.loading = false
      }
    },
    async deleteDoc(id) {
      try {
        await deleteKnowledgeDoc(id)
        await this.fetchDocs()
      } catch (e) {
        this.error = e.j?.error || t('error.knowledgeDeleteFailed')
      }
    },
    async fetchStats() {
      try {
        this.stats = await getKnowledgeStats()
      } catch { /* ignore */ }
    },
    async fetchSyncConfigs() {
      try {
        const result = await getSyncConfigs()
        this.syncConfigs = result.configs || []
      } catch { /* ignore */ }
    },
    async updateSyncConfig(source, enabled, interval) {
      try {
        await updateSyncConfig(source, { enabled, interval })
        await this.fetchSyncConfigs()
      } catch (e) {
        this.error = e.j?.error || t('error.knowledgeSyncConfigFailed')
      }
    }
  }
})
