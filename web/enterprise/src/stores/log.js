// 日志检索 store
import { defineStore } from 'pinia'
import { getLogs } from '@/api/log'

export const useLogStore = defineStore('log', {
  state: () => ({
    filters: {
      deviceID: '', agentID: '', level: '', source: '',
      keyword: '', from: '', to: '', limit: 200
    },
    list: [],
    offset: 0,
    pageSize: 0,           // 当前页条数
    loading: false,
    error: ''
  }),
  getters: {
    page: (s) => Math.floor(s.offset / s.filters.limit) + 1
  },
  actions: {
    async search(offset = 0) {
      this.offset = offset
      this.loading = true; this.error = ''
      try {
        const params = { ...this.filters, offset }
        // 清空字符串参数
        Object.keys(params).forEach((k) => { if (params[k] === '' || params[k] == null) delete params[k] })
        this.list = await getLogs(params) || []
        this.pageSize = this.list.length
      } catch (e) {
        this.error = e.j?.error || '日志检索失败'
      } finally {
        this.loading = false
      }
    },
    prev() {
      if (this.offset > 0) this.search(Math.max(0, this.offset - this.filters.limit))
    },
    next() {
      this.search(this.offset + this.filters.limit)
    },
    reset() {
      this.filters = { deviceID: '', agentID: '', level: '', source: '', keyword: '', from: '', to: '', limit: 200 }
      this.list = []; this.offset = 0; this.pageSize = 0
    }
  }
})