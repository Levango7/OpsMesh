// 死信队列（DLQ）store
import { defineStore } from 'pinia'
import { listDeadLetterTasks, retryDeadLetterTask, purgeDeadLetterTask } from '@/api/dead-letter'
import { t } from '@/i18n'

export const useDeadLetterStore = defineStore('dead-letter', {
  state: () => ({
    list: [],
    loading: false,
    error: ''
  }),
  actions: {
    async fetchDeadLetterTasks(params) {
      this.loading = true
      this.error = ''
      try {
        const r = await listDeadLetterTasks(params || {})
        this.list = Array.isArray(r) ? r : (r?.data || r?.items || r?.tasks || [])
      } catch (e) {
        this.error = e?.j?.error || t('error.taskListFailed')
        this.list = []
      } finally {
        this.loading = false
      }
    },
    async retry(id) {
      const r = await retryDeadLetterTask(id)
      await this.fetchDeadLetterTasks()
      return r
    },
    async purge(id) {
      const r = await purgeDeadLetterTask(id)
      await this.fetchDeadLetterTasks()
      return r
    }
  }
})
