// 审批流 store — 审批流 CRUD / 审批请求 CRUD / approve/reject/cancel / history / pending
import { defineStore } from 'pinia'
import {
  getApprovalFlows, createApprovalFlow, updateApprovalFlow, deleteApprovalFlow,
  getApprovalRequests, createApprovalRequest,
  approveApprovalRequest, rejectApprovalRequest, cancelApprovalRequest,
  getApprovalHistory, getPendingApprovals
} from '@/api/approval'
import { t } from '@/i18n'

export const useApprovalStore = defineStore('approval', {
  state: () => ({
    flows: [],
    requests: [],
    pending: [],
    // 当前请求的审批历史
    history: [],
    loading: false,
    error: ''
  }),
  actions: {
    // ---------- 审批流 ----------
    async fetchFlows() {
      this.loading = true; this.error = ''
      try {
        const r = await getApprovalFlows()
        this.flows = (r && r.flows) || r || []
      } catch (e) {
        this.error = e.j?.error || t('error.approvalFlowsFailed')
      } finally {
        this.loading = false
      }
    },
    async createFlow(body) {
      const r = await createApprovalFlow(body)
      await this.fetchFlows()
      return r
    },
    async updateFlow(id, body) {
      const r = await updateApprovalFlow(id, body)
      await this.fetchFlows()
      return r
    },
    async removeFlow(id) {
      const r = await deleteApprovalFlow(id)
      await this.fetchFlows()
      return r
    },

    // ---------- 审批请求 ----------
    async fetchRequests(params) {
      this.loading = true; this.error = ''
      try {
        const r = await getApprovalRequests(params)
        this.requests = (r && r.requests) || r || []
      } catch (e) {
        this.error = e.j?.error || t('error.approvalRequestsFailed')
      } finally {
        this.loading = false
      }
    },
    async createRequest(body) {
      const r = await createApprovalRequest(body)
      await this.fetchRequests()
      return r
    },
    async approve(id, body) {
      this.loading = true; this.error = ''
      try {
        const r = await approveApprovalRequest(id, body)
        await this.fetchRequests()
        await this.fetchPending()
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.approvalApproveFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    async reject(id, body) {
      this.loading = true; this.error = ''
      try {
        const r = await rejectApprovalRequest(id, body)
        await this.fetchRequests()
        await this.fetchPending()
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.approvalRejectFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    async cancel(id, body) {
      this.loading = true; this.error = ''
      try {
        const r = await cancelApprovalRequest(id, body)
        await this.fetchRequests()
        await this.fetchPending()
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.approvalCancelFailed')
        throw e
      } finally {
        this.loading = false
      }
    },

    // ---------- 审批历史 ----------
    async fetchHistory(id) {
      this.loading = true; this.error = ''
      try {
        const r = await getApprovalHistory(id)
        this.history = (r && r.history) || r || []
      } catch (e) {
        this.error = e.j?.error || t('error.approvalHistoryFailed')
      } finally {
        this.loading = false
      }
    },

    // ---------- 待我审批 ----------
    async fetchPending() {
      this.loading = true; this.error = ''
      try {
        const r = await getPendingApprovals()
        this.pending = (r && r.requests) || r || []
      } catch (e) {
        this.error = e.j?.error || t('error.approvalPendingFailed')
      } finally {
        this.loading = false
      }
    }
  }
})