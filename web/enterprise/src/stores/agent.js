// AI Agent store
import { defineStore } from 'pinia'
import { executeAgent, classifyIntent, decomposeTask, getAgentMemory, clearAgentMemory, getAgentTools } from '@/api/agent'
import { t } from '@/i18n'

export const useAgentStore = defineStore('agent', {
  state: () => ({
    results: [],
    currentResult: null,
    loading: false,
    error: '',
    tools: [],
    memory: [],
    intentType: '',
    intentConfidence: 0,
    decomposedTasks: []
  }),
  actions: {
    async execute(input, userId, tenantId, sessionId, useDAG) {
      this.loading = true
      this.error = ''
      try {
        const result = await executeAgent({
          input,
          userId,
          tenantId,
          sessionId,
          useDag: useDAG
        })
        this.currentResult = result
        this.results.unshift(result)
        return result
      } catch (e) {
        this.error = e.j?.error || t('error.agentExecuteFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    async classify(input) {
      try {
        const result = await classifyIntent({ input })
        this.intentType = result.agentType
        this.intentConfidence = result.confidence
        return result
      } catch (e) {
        this.error = e.j?.error || t('error.agentClassifyFailed')
        throw e
      }
    },
    async decompose(input) {
      try {
        const result = await decomposeTask({ input })
        this.decomposedTasks = result.subTasks || []
        return result
      } catch (e) {
        this.error = e.j?.error || t('error.agentDecomposeFailed')
        throw e
      }
    },
    async fetchTools() {
      try {
        const result = await getAgentTools()
        this.tools = result.tools || []
      } catch { /* ignore */ }
    },
    async fetchMemory(sessionId) {
      try {
        const result = await getAgentMemory(sessionId)
        this.memory = result.entries || []
      } catch { /* ignore */ }
    },
    async clearMemory(sessionId) {
      try {
        await clearAgentMemory(sessionId)
        this.memory = []
      } catch { /* ignore */ }
    }
  }
})
