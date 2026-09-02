// 自动纳管 store — 触发自动纳管 + 结果展示
import { defineStore } from 'pinia'
import { autoProvision } from '@/api/provision'
import { t } from '@/i18n'

export const useProvisionStore = defineStore('provision', {
  state: () => ({
    // 最近一次自动纳管结果
    result: null,
    loading: false,
    error: ''
  }),
  actions: {
    // 触发自动纳管
    async autoProvision(body) {
      this.loading = true; this.error = ''
      try {
        const r = await autoProvision(body)
        this.result = r.j || null
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.autoProvisionFailed')
        throw e
      } finally {
        this.loading = false
      }
    }
  }
})