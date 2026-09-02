// 灰度发布 store — 创建 / 状态 / 推进 / 流量分割 / 指标
import { defineStore } from 'pinia'
import {
  createCanary, getCanaryStatus, advanceCanary,
  getTrafficSplit, setTrafficSplit, getCanaryMetrics
} from '@/api/canary'
import { t } from '@/i18n'

export const useCanaryStore = defineStore('canary', {
  state: () => ({
    // 当前选中的灰度发布
    current: null,
    // 流量分割
    trafficSplit: null,
    // 灰度指标
    metrics: null,
    // 历史列表（本地维护）
    list: [],
    loading: false,
    error: ''
  }),
  actions: {
    async create(body) {
      this.loading = true; this.error = ''
      try {
        const r = await createCanary(body)
        const c = r.j || {}
        if (c.canaryID) {
          this.list.unshift(c)
          if (this.list.length > 50) this.list.pop()
        }
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.canaryCreateFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    async fetchStatus(id) {
      this.loading = true; this.error = ''
      try {
        this.current = await getCanaryStatus(id)
        return this.current
      } catch (e) {
        this.error = e.j?.error || t('error.canaryStatusFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    async advance(id) {
      this.loading = true; this.error = ''
      try {
        const r = await advanceCanary(id)
        // 推进后刷新状态
        try { this.current = await getCanaryStatus(id) } catch { /* 忽略刷新失败 */ }
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.canaryAdvanceFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    async fetchTrafficSplit(id) {
      try {
        this.trafficSplit = await getTrafficSplit(id)
        return this.trafficSplit
      } catch (e) {
        this.error = e.j?.error || t('error.canaryTrafficFailed')
        throw e
      }
    },
    async updateTrafficSplit(id, body) {
      try {
        const r = await setTrafficSplit(id, body)
        await this.fetchTrafficSplit(id)
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.canaryTrafficFailed')
        throw e
      }
    },
    async fetchMetrics(id) {
      try {
        this.metrics = await getCanaryMetrics(id)
        return this.metrics
      } catch (e) {
        this.error = e.j?.error || t('error.canaryMetricsFailed')
        throw e
      }
    },
    clearCurrent() {
      this.current = null
      this.trafficSplit = null
      this.metrics = null
    }
  }
})