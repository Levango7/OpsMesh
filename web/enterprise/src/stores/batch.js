// 批量运维 store — 批量下发 + 状态查询
import { defineStore } from 'pinia'
import { batchExec, getBatchStatus, batchDispatch } from '@/api/batch'
import { t } from '@/i18n'

export const useBatchStore = defineStore('batch', {
  state: () => ({
    // 最近一次批量执行返回
    lastBatch: null,
    // 当前查看的批量详情
    current: null,
    // 历史批量列表（本地维护，便于多次执行后查看）
    history: [],
    loading: false,
    error: ''
  }),
  actions: {
    async exec(body) {
      this.loading = true; this.error = ''
      try {
        const r = await batchExec(body)
        const batch = r.j || {}
        this.lastBatch = batch
        if (batch.batchID) {
          this.history.unshift(batch)
          if (this.history.length > 50) this.history.pop()
        }
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.batchExecFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    async dispatch(body) {
      this.loading = true; this.error = ''
      try {
        const r = await batchDispatch(body)
        const batch = r.j || {}
        this.lastBatch = batch
        if (batch.batchID) {
          this.history.unshift(batch)
          if (this.history.length > 50) this.history.pop()
        }
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.batchExecFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    async fetchStatus(id) {
      this.loading = true; this.error = ''
      try {
        this.current = await getBatchStatus(id)
        return this.current
      } catch (e) {
        this.error = e.j?.error || t('error.batchStatusFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    clearCurrent() {
      this.current = null
    }
  }
})