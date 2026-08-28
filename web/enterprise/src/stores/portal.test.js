// portal store 单元测试
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

vi.mock('@/api/portal', () => ({
  createResourceRequest: vi.fn(),
  getMyRequests: vi.fn(),
  getApprovalQueue: vi.fn(),
  approveRequest: vi.fn(),
  rejectRequest: vi.fn(),
  getCostOverview: vi.fn()
}))

import { usePortalStore } from '@/stores/portal'
import {
  createResourceRequest,
  getMyRequests,
  getApprovalQueue,
  approveRequest,
  rejectRequest,
  getCostOverview
} from '@/api/portal'

describe('usePortalStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('myRequests 初始为空数组', () => {
      const store = usePortalStore()
      expect(store.myRequests).toEqual([])
    })

    it('approvalQueue 初始为空数组', () => {
      const store = usePortalStore()
      expect(store.approvalQueue).toEqual([])
    })

    it('costOverview 初始为 null', () => {
      const store = usePortalStore()
      expect(store.costOverview).toBeNull()
    })
  })

  describe('fetchMyRequests 动作', () => {
    it('成功时设置 myRequests', async () => {
      const mockRequests = [{ id: 'req1', type: 'vm', resource: 'my-app', status: 'pending' }]
      getMyRequests.mockResolvedValueOnce({ requests: mockRequests })

      const store = usePortalStore()
      await store.fetchMyRequests()

      expect(store.myRequests).toEqual(mockRequests)
      expect(store.loading).toBe(false)
    })

    it('失败时设置 error', async () => {
      getMyRequests.mockRejectedValueOnce({ j: { error: '获取失败' } })

      const store = usePortalStore()
      await store.fetchMyRequests()

      expect(store.error).toBe('获取失败')
    })
  })

  describe('submitRequest 动作', () => {
    it('成功时返回结果', async () => {
      createResourceRequest.mockResolvedValueOnce({ s: 200, j: { id: 'req1' } })

      const store = usePortalStore()
      const r = await store.submitRequest('vm', 'my-app', '{}', 'need prod')

      expect(createResourceRequest).toHaveBeenCalledWith('vm', 'my-app', '{}', 'need prod')
      expect(r).toEqual({ s: 200, j: { id: 'req1' } })
    })
  })

  describe('fetchApprovalQueue 动作', () => {
    it('成功时设置 approvalQueue', async () => {
      const mockApprovals = [{ id: 'req1', type: 'vm', requester: 'user1' }]
      getApprovalQueue.mockResolvedValueOnce({ requests: mockApprovals })

      const store = usePortalStore()
      await store.fetchApprovalQueue()

      expect(store.approvalQueue).toEqual(mockApprovals)
    })
  })

  describe('approve 动作', () => {
    it('成功时返回结果', async () => {
      approveRequest.mockResolvedValueOnce({ s: 200, j: { status: 'approved' } })

      const store = usePortalStore()
      const r = await store.approve('req1')

      expect(approveRequest).toHaveBeenCalledWith('req1')
      expect(r).toEqual({ s: 200, j: { status: 'approved' } })
    })
  })

  describe('reject 动作', () => {
    it('成功时返回结果', async () => {
      rejectRequest.mockResolvedValueOnce({ s: 200, j: { status: 'rejected' } })

      const store = usePortalStore()
      const r = await store.reject('req1', 'budget exceeded')

      expect(rejectRequest).toHaveBeenCalledWith('req1', 'budget exceeded')
      expect(r).toEqual({ s: 200, j: { status: 'rejected' } })
    })
  })

  describe('fetchCostOverview 动作', () => {
    it('成功时设置 costOverview', async () => {
      const mockCost = { total: 1250.50, trend: [{ date: '2026-01-01', amount: 100 }], byCategory: [] }
      getCostOverview.mockResolvedValueOnce(mockCost)

      const store = usePortalStore()
      await store.fetchCostOverview()

      expect(store.costOverview).toEqual(mockCost)
    })
  })

  describe('getters', () => {
    it('pendingApprovals 返回待审批请求', () => {
      const store = usePortalStore()
      store.approvalQueue = [
        { id: 'req1', status: 'pending' },
        { id: 'req2', status: 'approved' },
        { id: 'req3', status: 'pending' }
      ]

      expect(store.pendingApprovals.length).toBe(2)
    })

    it('totalCost 返回总成本', () => {
      const store = usePortalStore()
      store.costOverview = { total: 1250.50 }

      expect(store.totalCost).toBe(1250.50)
    })

    it('totalCost 为 null 时返回 0', () => {
      const store = usePortalStore()
      store.costOverview = null

      expect(store.totalCost).toBe(0)
    })
  })
})
