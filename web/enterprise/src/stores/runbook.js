// Runbook 自动化 store — Runbook CRUD + 执行历史
import { defineStore } from 'pinia'
import {
  getRunbooks,
  createRunbook,
  updateRunbook,
  deleteRunbook,
  executeRunbook,
  getRunbookExecutions,
  getExecutionLogs
} from '@/api/runbook'
import { t } from '@/i18n'

export const useRunbookStore = defineStore('runbook', {
  state: () => ({
    runbooks: [],
    executions: [],
    currentRunbookId: '',
    logsContent: '',
    loading: false,
    executionsLoading: false,
    logsLoading: false,
    error: ''
  }),
  getters: {
    currentRunbook: (s) => s.runbooks.find((r) => r.id === s.currentRunbookId) || null
  },
  actions: {
    async fetchRunbooks() {
      this.loading = true; this.error = ''
      try {
        const data = await getRunbooks()
        this.runbooks = (data && data.runbooks) || data || []
      } catch (e) {
        this.error = e.j?.error || t('error.runbooksFailed')
      } finally {
        this.loading = false
      }
    },
    async addRunbook(name, description, content, triggers) {
      return await createRunbook(name, description, content, triggers)
    },
    async editRunbook(id, name, description, content, triggers) {
      return await updateRunbook(id, name, description, content, triggers)
    },
    async removeRunbook(id) {
      return await deleteRunbook(id)
    },
    async runRunbook(id) {
      return await executeRunbook(id)
    },
    async fetchExecutions(id) {
      this.executionsLoading = true; this.error = ''
      try {
        const data = await getRunbookExecutions(id)
        this.executions = (data && data.executions) || data || []
      } catch (e) {
        this.error = e.j?.error || t('error.executionsFailed')
      } finally {
        this.executionsLoading = false
      }
    },
    async fetchLogs(id, executionId) {
      this.logsLoading = true; this.error = ''
      try {
        const data = await getExecutionLogs(id, executionId)
        this.logsContent = (data && data.logs) || data || ''
      } catch (e) {
        this.error = e.j?.error || t('error.logsFailed')
      } finally {
        this.logsLoading = false
      }
    },
    selectRunbook(id) {
      this.currentRunbookId = id || ''
      if (id) {
        this.fetchExecutions(id)
      }
    }
  }
})
