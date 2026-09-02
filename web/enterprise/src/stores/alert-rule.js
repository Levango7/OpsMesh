// 告警规则 store — 规则 / 引擎规则 / 静默 三态合一
import { defineStore } from 'pinia'
import {
  getAlertRules, createAlertRule, updateAlertRule, deleteAlertRule,
  getAlertEngineRules, createAlertEngineRule, updateAlertEngineRule, deleteAlertEngineRule,
  getAlertSilences, createAlertSilence, deleteAlertSilence
} from '@/api/alert-rule'
import { t } from '@/i18n'

export const useAlertRuleStore = defineStore('alertRule', {
  state: () => ({
    rules: [],
    engineRules: [],
    silences: [],
    loading: false,
    error: ''
  }),
  actions: {
    // ---------- 告警规则 ----------
    async fetchRules() {
      this.loading = true; this.error = ''
      try {
        const r = await getAlertRules()
        this.rules = (r && r.rules) || r || []
      } catch (e) {
        this.error = e.j?.error || t('error.alertRuleListFailed')
      } finally {
        this.loading = false
      }
    },
    async createRule(body) {
      const r = await createAlertRule(body)
      await this.fetchRules()
      return r
    },
    async updateRule(id, body) {
      const r = await updateAlertRule(id, body)
      await this.fetchRules()
      return r
    },
    async removeRule(id) {
      const r = await deleteAlertRule(id)
      await this.fetchRules()
      return r
    },

    // ---------- 多条件引擎规则 ----------
    async fetchEngineRules() {
      this.loading = true; this.error = ''
      try {
        const r = await getAlertEngineRules()
        this.engineRules = (r && r.rules) || r || []
      } catch (e) {
        this.error = e.j?.error || t('error.alertEngineListFailed')
      } finally {
        this.loading = false
      }
    },
    async createEngineRule(body) {
      const r = await createAlertEngineRule(body)
      await this.fetchEngineRules()
      return r
    },
    async updateEngineRule(id, body) {
      const r = await updateAlertEngineRule(id, body)
      await this.fetchEngineRules()
      return r
    },
    async removeEngineRule(id) {
      const r = await deleteAlertEngineRule(id)
      await this.fetchEngineRules()
      return r
    },

    // ---------- 静默规则 ----------
    async fetchSilences() {
      this.loading = true; this.error = ''
      try {
        const r = await getAlertSilences()
        this.silences = (r && r.silences) || r || []
      } catch (e) {
        this.error = e.j?.error || t('error.alertSilenceListFailed')
      } finally {
        this.loading = false
      }
    },
    async createSilence(body) {
      const r = await createAlertSilence(body)
      await this.fetchSilences()
      return r
    },
    async removeSilence(id) {
      const r = await deleteAlertSilence(id)
      await this.fetchSilences()
      return r
    }
  }
})