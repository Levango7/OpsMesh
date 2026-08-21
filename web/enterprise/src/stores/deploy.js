// 部署 store
import { defineStore } from 'pinia'
import { getDeploys, createDeploy, executeDeploy, rollbackDeploy, getDeploy } from '@/api/deploy'
import { t } from '@/i18n'

export const useDeployStore = defineStore('deploy', {
  state: () => ({
    list: [],
    statusFilter: '',
    current: null,
    loading: false,
    error: '',
    msg: ''
  }),
  actions: {
    async fetchList() {
      this.loading = true; this.error = ''
      try { this.list = await getDeploys(this.statusFilter) || [] }
      catch (e) { this.error = e.j?.error || t('error.deployListFailed') }
      finally { this.loading = false }
    },
    async create(body) {
      const r = await createDeploy(body); await this.fetchList(); return r
    },
    async execute(id) {
      const r = await executeDeploy(id); await this.fetchList(); return r
    },
    async rollback(id) {
      const r = await rollbackDeploy(id); await this.fetchList(); return r
    },
    async open(id) {
      try { this.current = await getDeploy(id) }
      catch (e) { this.error = e.j?.error || t('error.deployDetailFailed') }
    }
  }
})