// Portal 自助服务门户 store — 请求、审批、成本
import { defineStore } from 'pinia'
import {
  createResourceRequest,
  getMyRequests,
  getApprovalQueue,
  approveRequest,
  rejectRequest,
  getCostOverview
} from '@/api/portal'
import { t } from '@/i18n'

export const usePortalStore = defineStore('portal', {
  state: () => ({
    myRequests: [],
    approvalQueue: [],
    costOverview: null,
    loading: false,
    error: ''
  }),
  getters: {
    pendingApprovals: (s) => s.approvalQueue.filter((r) => r.status === 'pending'),
    totalCost: (s) => s.costOverview?.total || 0
  },
  actions: {
    async fetchMyRequests(status) {
      this.loading = true; this.error = ''
      try {
        const data = await getMyRequests(status)
        this.myRequests = (data && data.requests) || data || []
      } catch (e) {
        this.error = e.j?.error || t('error.requestsFailed')
      } finally {
        this.loading = false
      }
    },
    async submitRequest(type, resource, params, reason) {
      return await createResourceRequest(type, resource, params, reason)
    },
    async fetchApprovalQueue() {
      this.loading = true; this.error = ''
      try {
        const data = await getApprovalQueue()
        this.approvalQueue = (data && data.requests) || data || []
      } catch (e) {
        this.error = e.j?.error || t('error.approvalsFailed')
      } finally {
        this.loading = false
      }
    },
    async approve(id) {
      return await approveRequest(id)
    },
    async reject(id, reason) {
      return await rejectRequest(id, reason)
    },
    async fetchCostOverview() {
      try {
        const data = await getCostOverview()
        this.costOverview = data
      } catch {
        this.costOverview = null
      }
    }
  }
})
