// Helm 应用商店 store — 仓库 CRUD / Chart 搜索 / Release CRUD / rollback / history / catalog
import { defineStore } from 'pinia'
import {
  getHelmRepos, createHelmRepo, deleteHelmRepo, getRepoCharts,
  searchCharts,
  getHelmReleases, createHelmRelease, upgradeHelmRelease, deleteHelmRelease,
  rollbackHelmRelease, getReleaseHistory,
  getHelmCatalog
} from '@/api/helm'
import { t } from '@/i18n'

export const useHelmStore = defineStore('helm', {
  state: () => ({
    repos: [],
    charts: [],
    releases: [],
    history: [],
    catalog: [],
    loading: false,
    error: ''
  }),
  actions: {
    // ---------- 仓库 ----------
    async fetchRepos() {
      this.loading = true; this.error = ''
      try {
        const r = await getHelmRepos()
        this.repos = (r && r.repos) || r || []
      } catch (e) {
        this.error = e.j?.error || t('error.helmReposFailed')
      } finally {
        this.loading = false
      }
    },
    async createRepo(body) {
      const r = await createHelmRepo(body)
      await this.fetchRepos()
      return r
    },
    async removeRepo(name) {
      const r = await deleteHelmRepo(name)
      await this.fetchRepos()
      return r
    },

    // ---------- 仓库内 Chart 列表 ----------
    async fetchRepoCharts(name) {
      this.loading = true; this.error = ''
      try {
        const r = await getRepoCharts(name)
        this.charts = (r && r.charts) || r || []
      } catch (e) {
        this.error = e.j?.error || t('error.helmRepoChartsFailed')
      } finally {
        this.loading = false
      }
    },

    // ---------- Chart 搜索 ----------
    async searchCharts(q) {
      this.loading = true; this.error = ''
      try {
        const r = await searchCharts(q)
        this.charts = (r && r.charts) || r || []
      } catch (e) {
        this.error = e.j?.error || t('error.helmSearchFailed')
      } finally {
        this.loading = false
      }
    },

    // ---------- Release ----------
    async fetchReleases() {
      this.loading = true; this.error = ''
      try {
        const r = await getHelmReleases()
        this.releases = (r && r.releases) || r || []
      } catch (e) {
        this.error = e.j?.error || t('error.helmReleasesFailed')
      } finally {
        this.loading = false
      }
    },
    async createRelease(body) {
      const r = await createHelmRelease(body)
      await this.fetchReleases()
      return r
    },
    async upgradeRelease(name, body) {
      this.loading = true; this.error = ''
      try {
        const r = await upgradeHelmRelease(name, body)
        await this.fetchReleases()
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.helmUpgradeFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    async removeRelease(name) {
      this.loading = true; this.error = ''
      try {
        const r = await deleteHelmRelease(name)
        await this.fetchReleases()
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.helmUninstallFailed')
        throw e
      } finally {
        this.loading = false
      }
    },

    // ---------- Release 回滚 ----------
    async rollbackRelease(name, body) {
      this.loading = true; this.error = ''
      try {
        const r = await rollbackHelmRelease(name, body)
        await this.fetchReleases()
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.helmRollbackFailed')
        throw e
      } finally {
        this.loading = false
      }
    },

    // ---------- Release 历史 ----------
    async fetchHistory(name) {
      this.loading = true; this.error = ''
      try {
        const r = await getReleaseHistory(name)
        this.history = (r && r.history) || r || []
      } catch (e) {
        this.error = e.j?.error || t('error.helmHistoryFailed')
      } finally {
        this.loading = false
      }
    },

    // ---------- 预置目录 ----------
    async fetchCatalog() {
      this.loading = true; this.error = ''
      try {
        const r = await getHelmCatalog()
        this.catalog = (r && r.categories) || r || []
      } catch (e) {
        this.error = e.j?.error || t('error.helmCatalogFailed')
      } finally {
        this.loading = false
      }
    }
  }
})