// 平台配置 store — 配置 / 健康检查 / 指标汇总
import { defineStore } from 'pinia'
import { getPlatformConfig, updatePlatformConfig, getPlatformHealth, getPlatformMetrics } from '@/api/platform'
import { t } from '@/i18n'

export const usePlatformStore = defineStore('platform', {
  state: () => ({
    config: null,
    health: null,
    metrics: null,
    loading: false,
    error: ''
  }),
  actions: {
    // 读取平台配置
    async fetchConfig() {
      this.loading = true; this.error = ''
      try {
        this.config = await getPlatformConfig()
      } catch (e) {
        this.error = e.j?.error || t('error.platformConfigFailed')
      } finally {
        this.loading = false
      }
    },
    // 更新平台配置
    async updateConfig(body) {
      this.loading = true; this.error = ''
      try {
        const r = await updatePlatformConfig(body)
        this.config = r.j || this.config
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.platformConfigUpdateFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    // 平台健康检查
    async fetchHealth() {
      this.loading = true; this.error = ''
      try {
        this.health = await getPlatformHealth()
      } catch (e) {
        this.error = e.j?.error || t('error.platformHealthFailed')
      } finally {
        this.loading = false
      }
    },
    // 平台指标汇总
    async fetchMetrics() {
      this.loading = true; this.error = ''
      try {
        this.metrics = await getPlatformMetrics()
      } catch (e) {
        this.error = e.j?.error || t('error.platformMetricsFailed')
      } finally {
        this.loading = false
      }
    }
  }
})