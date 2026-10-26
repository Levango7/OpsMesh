// 任务 store
import { defineStore } from 'pinia'
import { getTasks, createTask, cancelTask, approveTask, rejectTask } from '@/api/task'
import { t } from '@/i18n'

export const useTaskStore = defineStore('task', {
  state: () => ({
    list: [],
    statusFilter: '',
    loading: false,
    error: '',
    pendingApprovals: []
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
    async fetchPendingApprovals() {
      try {
        this.pendingApprovals = await getTasks('pending_approval') || []
      } catch {
        this.pendingApprovals = []
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
    },
    async approve(id) {
      const r = await approveTask(id)
      await this.fetchTasks()
      await this.fetchPendingApprovals()
      return r
    },
    async reject(id) {
      const r = await rejectTask(id)
      await this.fetchTasks()
      await this.fetchPendingApprovals()
      return r
    }
  }
})
