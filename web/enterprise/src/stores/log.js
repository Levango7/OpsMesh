// 日志检索 store
// 支持两种检索模式：
//   - simple  : 传统字段过滤 + keyword 模糊匹配（向后兼容）
//   - advanced : KQL/Lucene 风格结构化查询语法（q 参数）
// 切换模式时不清空已选 filters / q，便于用户在两种模式间来回切换。
// 查询失败（含语法错误 400）时保留上次的 list，仅设置 error，避免结果突兀清空。
import { defineStore } from 'pinia'
import { getLogs, queryLogs } from '@/api/log'
import { t } from '@/i18n'

const DEFAULT_FILTERS = {
  deviceID: '', agentID: '', level: '', source: '',
  keyword: '', from: '', to: '', limit: 200
}

export const useLogStore = defineStore('log', {
  state: () => ({
    // 检索模式：'simple' | 'advanced'
    mode: 'simple',
    // 简单搜索过滤条件
    filters: { ...DEFAULT_FILTERS },
    // 高级查询语法字符串（KQL/Lucene 风格）
    q: '',
    // 结果列表
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
    // 统一搜索入口：根据 mode 选择 API
    async search(offset = 0) {
      this.offset = offset
      this.loading = true
      // 注意：不清空 error 在 try 内重置；失败时保留旧 list 仅更新 error
      const previousList = this.list
      try {
        let params
        if (this.mode === 'advanced') {
          params = { q: this.q, from: this.filters.from, to: this.filters.to, limit: this.filters.limit, offset }
        } else {
          params = { ...this.filters, offset }
        }
        // 清空字符串与 null 参数（保留数字 0）
        Object.keys(params).forEach((k) => {
          if (params[k] === '' || params[k] == null) delete params[k]
        })
        const apiFn = this.mode === 'advanced' ? queryLogs : getLogs
        const data = await apiFn(params)
        this.list = data || []
        this.pageSize = this.list.length
        this.error = ''
      } catch (e) {
        // 语法错误（400）或其他错误：保留上次结果，仅显示错误提示
        this.list = previousList
        this.pageSize = previousList.length
        // 优先取后端返回的 error 字段；语法错误时附加状态码便于前端识别
        const msg = e?.j?.error || t('error.logsFailed')
        this.error = e?.s === 400 ? t('logs.querySyntaxError') + msg : msg
      } finally {
        this.loading = false
      }
    },
    // 切换模式
    setMode(mode) {
      if (mode !== 'simple' && mode !== 'advanced') return
      this.mode = mode
      this.error = ''
    },
    prev() {
      if (this.offset > 0) this.search(Math.max(0, this.offset - this.filters.limit))
    },
    next() {
      this.search(this.offset + this.filters.limit)
    },
    reset() {
      this.filters = { ...DEFAULT_FILTERS }
      this.q = ''
      this.list = []
      this.offset = 0
      this.pageSize = 0
      this.error = ''
    }
  }
})
