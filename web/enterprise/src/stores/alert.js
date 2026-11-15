// 告警 store
import { defineStore } from 'pinia'
import { getAlerts, ackAlert, silenceAlert } from '@/api/alert'
import { t } from '@/i18n'

export const useAlertStore = defineStore('alert', {
  state: () => ({
    list: [],
    loading: false,
    error: '',
    // ADR-2026-11-15 §ADR-3：服务端分页
    page: 1,
    pageSize: 20,
    total: 0,
    hasMore: false,
    rootCauses: {}, // alertID -> {category, component, description, confidence, evidence}
    traceIDs: {}    // alertID -> traceID (from SSE AIOps root cause analysis)
  }),
  getters: {
    critical: (s) => s.list.filter((a) => a.severity === 'critical'),
    warning: (s) => s.list.filter((a) => a.severity === 'warning')
  },
  actions: {
    async fetchAlerts(page = 1) {
      this.loading = true; this.error = ''
      try {
        const d = await getAlerts({ page, pageSize: this.pageSize })
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
        this.error = e.j?.error || t('error.alertListFailed')
      } finally {
        this.loading = false
      }
    },
    setRootCause(alertID, rootCause) {
      this.rootCauses[alertID] = rootCause
    },
    setTraceID(alertID, traceID) {
      this.traceIDs[alertID] = traceID
    },
    async ack(id) {
      try {
        // 保持当前页码：ack 后重新拉取同一页，避免翻页位置被重置到第 1 页
        const r = await ackAlert(id); await this.fetchAlerts(this.page); return r
      } catch (e) {
        this.error = e.j?.error || t('error.alertAckFailed')
        throw e
      }
    },
    async silence(id, body) {
      try {
        const r = await silenceAlert(id, body); await this.fetchAlerts(this.page); return r
      } catch (e) {
        this.error = e.j?.error || t('error.alertSilenceFailed')
        throw e
      }
    }
  }
})