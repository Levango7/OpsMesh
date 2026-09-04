// Autoscaler 自动扩缩容 store — 规则、决策、冷却
import { defineStore } from 'pinia'
import {
  getScalingRules,
  createScalingRule,
  updateScalingRule,
  deleteScalingRule,
  getScalingDecisions,
  manualScale,
  getCooldowns
} from '@/api/autoscaler'
import { t } from '@/i18n'

export const useAutoscalerStore = defineStore('autoscaler', {
  state: () => ({
    rules: [],
    decisions: [],
    cooldowns: [],
    loading: false,
    decisionsLoading: false,
    error: ''
  }),
  getters: {
    enabledRules: (s) => s.rules.filter((r) => r.enabled),
    activeCooldowns: (s) => s.cooldowns.filter((c) => c.remaining > 0)
  },
  actions: {
    async fetchRules() {
      this.loading = true; this.error = ''
      try {
        const data = await getScalingRules()
        this.rules = (data && data.rules) || data || []
      } catch (e) {
        this.error = e.j?.error || t('error.scalingRulesFailed')
      } finally {
        this.loading = false
      }
    },
    async addRule(name, metric, threshold, minReplicas, maxReplicas, cooldown) {
      return await createScalingRule(name, metric, threshold, minReplicas, maxReplicas, cooldown)
    },
    async editRule(id, data) {
      return await updateScalingRule(id, data)
    },
    async removeRule(id) {
      return await deleteScalingRule(id)
    },
    async fetchDecisions() {
      this.decisionsLoading = true; this.error = ''
      try {
        const data = await getScalingDecisions()
        this.decisions = (data && data.decisions) || data || []
      } catch (e) {
        this.error = e.j?.error || t('error.decisionsFailed')
      } finally {
        this.decisionsLoading = false
      }
    },
    async triggerScale(target, replicas, reason) {
      return await manualScale(target, replicas, reason)
    },
    async fetchCooldowns() {
      try {
        const data = await getCooldowns()
        this.cooldowns = (data && data.cooldowns) || data || []
      } catch {
        this.cooldowns = []
      }
    }
  }
})
