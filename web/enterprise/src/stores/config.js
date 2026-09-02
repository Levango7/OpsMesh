// 配置热推送 store — 热推送 / 灰度发布 / 版本历史
import { defineStore } from 'pinia'
import { hotpushConfig, canaryConfig, getConfigVersions } from '@/api/config'
import { t } from '@/i18n'

export const useConfigStore = defineStore('configHotpush', {
  state: () => ({
    versions: [],
    // 最近一次热推送返回
    lastHotpush: null,
    // 最近一次灰度发布返回
    lastCanary: null,
    loading: false,
    error: ''
  }),
  actions: {
    // 配置热推送
    async hotpush(body) {
      this.loading = true; this.error = ''
      try {
        const r = await hotpushConfig(body)
        this.lastHotpush = r.j || null
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.configHotpushFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    // 配置灰度发布
    async canary(body) {
      this.loading = true; this.error = ''
      try {
        const r = await canaryConfig(body)
        this.lastCanary = r.j || null
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.configCanaryFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    // 配置版本历史（支持按 key / agentID 过滤与 limit 限制）
    async fetchVersions(params) {
      this.loading = true; this.error = ''
      try {
        const r = await getConfigVersions(params) || {}
        this.versions = r.versions || []
      } catch (e) {
        this.error = e.j?.error || t('error.configVersionsFailed')
      } finally {
        this.loading = false
      }
    }
  }
})