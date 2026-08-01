// 告警 store
import { defineStore } from 'pinia'
import { getAlerts, ackAlert, silenceAlert } from '@/api/alert'

export const useAlertStore = defineStore('alert', {
  state: () => ({
    list: [],
    loading: false,
    error: ''
  }),
  getters: {
    critical: (s) => s.list.filter((a) => a.severity === 'critical'),
    warning: (s) => s.list.filter((a) => a.severity === 'warning')
  },
  actions: {
    async fetchAlerts() {
      this.loading = true; this.error = ''
      try {
        this.list = await getAlerts() || []
      } catch (e) {
        this.error = e.j?.error || '告警列表拉取失败'
      } finally {
        this.loading = false
      }
    },
    async ack(id) {
      const r = await ackAlert(id); await this.fetchAlerts(); return r
    },
    async silence(id, body) {
      const r = await silenceAlert(id, body); await this.fetchAlerts(); return r
    }
  }
})