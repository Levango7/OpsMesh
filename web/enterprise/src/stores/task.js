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
    // 审计整改批次29（ADR-3）：服务端分页状态
    page: 1,
    pageSize: 20,
    total: 0,
    hasMore: false,
    _seq: 0, // 审计整改批次22：筛选切换竞态守卫
    pendingApprovals: []
  }),
  actions: {
    async fetchTasks(page = 1) {
      const seq = ++this._seq
      this.loading = true; this.error = ''
      try {
        const params = { page, pageSize: this.pageSize }
        if (this.statusFilter) params.status = this.statusFilter
        const d = await getTasks(params)
        if (seq !== this._seq) return // 筛选已切换/已有更新请求
        // 兼容两种形态：全量数组（无 page 参数时）与 {data,total,hasMore} 信封
        if (Array.isArray(d)) {
          this.list = d
          this.total = d.length
          this.hasMore = false
        } else {
          this.list = (d && d.data) || []
          this.total = (d && d.total) || this.list.length
          this.hasMore = !!(d && d.hasMore)
        }
        this.page = page
      } catch (e) {
        if (seq !== this._seq) return
        this.error = e.j?.error || t('error.taskListFailed')
      } finally {
        if (seq === this._seq) this.loading = false
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
