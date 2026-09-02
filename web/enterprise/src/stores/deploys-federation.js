// 多集群联邦部署 store — 联邦部署列表 / 创建 / 详情
import { defineStore } from 'pinia'
import { getFederationDeploys, createFederationDeploy, getFederationDeploy } from '@/api/deploys-federation'
import { t } from '@/i18n'

export const useFederationDeploysStore = defineStore('federationDeploys', {
  state: () => ({
    deploys: [],
    // 当前查看的部署详情
    current: null,
    loading: false,
    error: ''
  }),
  actions: {
    // 联邦部署列表
    async fetchDeploys() {
      this.loading = true; this.error = ''
      try {
        const r = await getFederationDeploys()
        this.deploys = (r && r.deploys) || r || []
      } catch (e) {
        this.error = e.j?.error || t('error.federationDeploysListFailed')
      } finally {
        this.loading = false
      }
    },
    // 创建联邦部署
    async create(body) {
      this.loading = true; this.error = ''
      try {
        const r = await createFederationDeploy(body)
        await this.fetchDeploys()
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.federationDeployCreateFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    // 联邦部署详情
    async fetchDetail(id) {
      this.loading = true; this.error = ''
      try {
        this.current = await getFederationDeploy(id)
        return this.current
      } catch (e) {
        this.error = e.j?.error || t('error.federationDeployDetailFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    clearCurrent() {
      this.current = null
    }
  }
})