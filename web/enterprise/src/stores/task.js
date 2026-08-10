// 任务 store
import { defineStore } from 'pinia'
import { getTasks, createTask, cancelTask } from '@/api/task'
import { t } from '@/i18n'

export const useTaskStore = defineStore('task', {
  state: () => ({
    list: [],
    statusFilter: '',
    loading: false,
    error: ''
  }),
  actions: {
    async fetchTasks() {
      this.loading = true; this.error = ''
      try {
        this.list = await getTasks(this.statusFilter) || []
      } catch (e) {
        this.error = e.j?.error || t('error.taskListFailed')
      } finally {
        this.loading = false
      }
    },
    async create(body) {
      const r = await createTask(body)
      await this.fetchTasks()
      return r
    },
    async cancel(id) {
      const r = await cancelTask(id)
      await this.fetchTasks()
      return r
    }
  }
})