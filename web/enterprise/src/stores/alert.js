// 告警 store
import { defineStore } from 'pinia'
import { getAlerts, ackAlert, silenceAlert } from '@/api/alert'
import { t } from '@/i18n'

export const useAlertStore = defineStore('alert', {
  state: () => ({
    list: [],
    loading: false,
    error: '',
    rootCauses: {} // alertID -> {category, component, description, confidence, evidence}
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
        this.error = e.j?.error || t('error.alertListFailed')
      } finally {
        this.loading = false
      }
    },
    setRootCause(alertID, rootCause) {
      this.rootCauses[alertID] = rootCause
    },
    async ack(id) {
      try {
        const r = await ackAlert(id); await this.fetchAlerts(); return r
      } catch (e) {
        this.error = e.j?.error || t('error.alertAckFailed')
        throw e
      }
    },
    async silence(id, body) {
      try {
        const r = await silenceAlert(id, body); await this.fetchAlerts(); return r
      } catch (e) {
        this.error = e.j?.error || t('error.alertSilenceFailed')
        throw e
      }
    }
  }
})