// 成本控制 store
import { defineStore } from 'pinia'
import { getBudget, setBudget, getBudgetAlerts, getCostReport, getModelRoutes, setModelRoutes, queryRoute, generateReport } from '@/api/cost'
import { t } from '@/i18n'

export const useCostStore = defineStore('cost', {
  state: () => ({
    budget: null,
    alerts: [],
    report: null,
    routes: [],
    loading: false,
    error: ''
  }),
  actions: {
    async fetchBudget(tenantId) {
      this.loading = true
      try {
        this.budget = await getBudget(tenantId)
      } catch (e) {
        this.error = e.j?.error || t('error.costBudgetFailed')
      } finally {
        this.loading = false
      }
    },
    async updateBudget(tenantId, daily, monthly, threshold) {
      try {
        await setBudget({ tenantId, daily, monthly, threshold })
        await this.fetchBudget(tenantId)
      } catch (e) {
        this.error = e.j?.error || t('error.costBudgetUpdateFailed')
      }
    },
    async fetchAlerts(tenantId) {
      try {
        const result = await getBudgetAlerts(tenantId)
        this.alerts = result.alerts || []
      } catch { /* ignore */ }
    },
    async fetchReport(tenantId, since) {
      this.loading = true
      try {
        this.report = await getCostReport(tenantId, since)
      } catch (e) {
        this.error = e.j?.error || t('error.costReportFailed')
      } finally {
        this.loading = false
      }
    },
    async fetchRoutes() {
      try {
        const result = await getModelRoutes()
        this.routes = result.rules || []
      } catch { /* ignore */ }
    },
    async updateRoutes(rules) {
      try {
        await setModelRoutes({ rules })
        await this.fetchRoutes()
      } catch (e) {
        this.error = e.j?.error || t('error.costRoutesUpdateFailed')
      }
    },
    async queryRoute(taskType) {
      try {
        return await queryRoute({ taskType })
      } catch (e) {
        this.error = e.j?.error || t('error.costRouteQueryFailed')
      }
    },
    async generateMarkdownReport(title, sections) {
      try {
        return await generateReport({ title, sections, format: 'markdown' })
      } catch (e) {
        this.error = e.j?.error || t('error.costReportGenerateFailed')
      }
    }
  }
})
