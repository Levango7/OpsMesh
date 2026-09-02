// approval store 单元测试
// 覆盖：初始状态、fetchFlows/createFlow/removeFlow/fetchRequests/approve/reject/
//      cancel/fetchHistory/fetchPending 动作、loading/error 状态。
// 说明：源码中方法名为 createFlow/removeFlow/approve/reject/cancel（任务描述中的
//      deleteFlow/approveRequest/rejectRequest 为别名语义），本测试按实际导出方法验证。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

// mock @/api/approval：避免真实网络请求，用 vi.fn 控制返回值
vi.mock('@/api/approval', () => ({
  getApprovalFlows: vi.fn(),
  createApprovalFlow: vi.fn(),
  updateApprovalFlow: vi.fn(),
  deleteApprovalFlow: vi.fn(),
  getApprovalRequests: vi.fn(),
  createApprovalRequest: vi.fn(),
  approveApprovalRequest: vi.fn(),
  rejectApprovalRequest: vi.fn(),
  cancelApprovalRequest: vi.fn(),
  getApprovalHistory: vi.fn(),
  getPendingApprovals: vi.fn(),
}))

import { useApprovalStore } from '@/stores/approval'
import {
  getApprovalFlows, createApprovalFlow, deleteApprovalFlow,
  getApprovalRequests, approveApprovalRequest, rejectApprovalRequest,
  getPendingApprovals
} from '@/api/approval'

describe('useApprovalStore', () => {
  beforeEach(() => {
    // 每个测试前创建新 pinia 实例，隔离状态
    setActivePinia(createPinia())
    // 重置 mock 调用记录
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('flows 初始为空数组', () => {
      const store = useApprovalStore()
      expect(store.flows).toEqual([])
    })

    it('requests 初始为空数组', () => {
      const store = useApprovalStore()
      expect(store.requests).toEqual([])
    })

    it('pending 初始为空数组', () => {
      const store = useApprovalStore()
      expect(store.pending).toEqual([])
    })

    it('history 初始为空数组', () => {
      const store = useApprovalStore()
      expect(store.history).toEqual([])
    })

    it('loading 初始为 false', () => {
      const store = useApprovalStore()
      expect(store.loading).toBe(false)
    })

    it('error 初始为空字符串', () => {
      const store = useApprovalStore()
      expect(store.error).toBe('')
    })
  })

  describe('fetchFlows 动作', () => {
    it('成功时设置 flows（r.flows 形态）', async () => {
      const mockFlows = [{ id: 'f1', name: '默认审批流' }]
      getApprovalFlows.mockResolvedValueOnce({ flows: mockFlows, total: 1 })

      const store = useApprovalStore()
      await store.fetchFlows()

      expect(getApprovalFlows).toHaveBeenCalled()
      expect(store.flows).toEqual(mockFlows)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('成功时设置 flows（裸数组形态）', async () => {
      const mockFlows = [{ id: 'f1' }]
      getApprovalFlows.mockResolvedValueOnce(mockFlows)

      const store = useApprovalStore()
      await store.fetchFlows()

      expect(store.flows).toEqual(mockFlows)
    })

    it('API 返回 null 时 flows 为空数组', async () => {
      getApprovalFlows.mockResolvedValueOnce(null)

      const store = useApprovalStore()
      await store.fetchFlows()

      expect(store.flows).toEqual([])
    })

    it('失败时设置 error 并结束 loading', async () => {
      const err = { j: { error: '网络异常' } }
      getApprovalFlows.mockRejectedValueOnce(err)

      const store = useApprovalStore()
      await store.fetchFlows()

      expect(store.error).toBe('网络异常')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getApprovalFlows.mockRejectedValueOnce({})

      const store = useApprovalStore()
      await store.fetchFlows()

      expect(store.error).toBe('审批流列表拉取失败')
    })
  })

  describe('createFlow 动作', () => {
    it('成功时调用 API 并刷新列表', async () => {
      const mockResp = { s: 201, j: { id: 'f1', name: '新流' } }
      createApprovalFlow.mockResolvedValueOnce(mockResp)
      getApprovalFlows.mockResolvedValueOnce({ flows: [{ id: 'f1' }] })

      const store = useApprovalStore()
      const body = { name: '新流', steps: [] }
      const r = await store.createFlow(body)

      expect(createApprovalFlow).toHaveBeenCalledWith(body)
      expect(getApprovalFlows).toHaveBeenCalled()
      expect(store.flows).toEqual([{ id: 'f1' }])
      expect(r).toEqual(mockResp)
    })

    it('API 抛错时向上抛出异常', async () => {
      createApprovalFlow.mockRejectedValueOnce(new Error('create failed'))

      const store = useApprovalStore()
      await expect(store.createFlow({})).rejects.toThrow('create failed')
    })
  })

  describe('removeFlow 动作（任务描述中的 deleteFlow）', () => {
    it('成功时调用 API 并刷新列表', async () => {
      const mockResp = { s: 200, j: { status: 'deleted' } }
      deleteApprovalFlow.mockResolvedValueOnce(mockResp)
      getApprovalFlows.mockResolvedValueOnce({ flows: [] })

      const store = useApprovalStore()
      const r = await store.removeFlow('f1')

      expect(deleteApprovalFlow).toHaveBeenCalledWith('f1')
      expect(getApprovalFlows).toHaveBeenCalled()
      expect(store.flows).toEqual([])
      expect(r).toEqual(mockResp)
    })

    it('API 抛错时向上抛出异常', async () => {
      deleteApprovalFlow.mockRejectedValueOnce(new Error('delete failed'))

      const store = useApprovalStore()
      await expect(store.removeFlow('f1')).rejects.toThrow('delete failed')
    })
  })

  describe('fetchRequests 动作', () => {
    it('成功时设置 requests（r.requests 形态）', async () => {
      const mockRequests = [{ id: 'r1', status: 'pending' }]
      getApprovalRequests.mockResolvedValueOnce({ requests: mockRequests })

      const store = useApprovalStore()
      const params = { status: 'pending' }
      await store.fetchRequests(params)

      expect(getApprovalRequests).toHaveBeenCalledWith(params)
      expect(store.requests).toEqual(mockRequests)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('成功时设置 requests（裸数组形态）', async () => {
      const mockRequests = [{ id: 'r1' }]
      getApprovalRequests.mockResolvedValueOnce(mockRequests)

      const store = useApprovalStore()
      await store.fetchRequests()

      expect(store.requests).toEqual(mockRequests)
    })

    it('API 返回 null 时 requests 为空数组', async () => {
      getApprovalRequests.mockResolvedValueOnce(null)

      const store = useApprovalStore()
      await store.fetchRequests()

      expect(store.requests).toEqual([])
    })

    it('失败时设置 error 并结束 loading', async () => {
      const err = { j: { error: '权限不足' } }
      getApprovalRequests.mockRejectedValueOnce(err)

      const store = useApprovalStore()
      await store.fetchRequests()

      expect(store.error).toBe('权限不足')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getApprovalRequests.mockRejectedValueOnce({})

      const store = useApprovalStore()
      await store.fetchRequests()

      expect(store.error).toBe('审批请求列表拉取失败')
    })
  })

  describe('approve 动作（任务描述中的 approveRequest）', () => {
    it('成功时调用 API 并刷新 requests 与 pending', async () => {
      const mockResp = { s: 200, j: { id: 'r1', status: 'approved' } }
      approveApprovalRequest.mockResolvedValueOnce(mockResp)
      getApprovalRequests.mockResolvedValueOnce({ requests: [{ id: 'r1', status: 'approved' }] })
      getPendingApprovals.mockResolvedValueOnce({ requests: [] })

      const store = useApprovalStore()
      const body = { comment: '同意' }
      const r = await store.approve('r1', body)

      expect(approveApprovalRequest).toHaveBeenCalledWith('r1', body)
      expect(getApprovalRequests).toHaveBeenCalled()
      expect(getPendingApprovals).toHaveBeenCalled()
      expect(store.requests).toEqual([{ id: 'r1', status: 'approved' }])
      expect(store.pending).toEqual([])
      expect(store.loading).toBe(false)
      expect(r).toEqual(mockResp)
    })

    it('失败时设置 error、结束 loading 并向上抛出', async () => {
      const err = { j: { error: '已审批' } }
      approveApprovalRequest.mockRejectedValueOnce(err)

      const store = useApprovalStore()
      await expect(store.approve('r1', {})).rejects.toThrow()

      expect(store.error).toBe('已审批')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      approveApprovalRequest.mockRejectedValueOnce({})

      const store = useApprovalStore()
      await expect(store.approve('r1', {})).rejects.toThrow()

      expect(store.error).toBe('审批通过失败')
    })
  })

  describe('reject 动作（任务描述中的 rejectRequest）', () => {
    it('成功时调用 API 并刷新 requests 与 pending', async () => {
      const mockResp = { s: 200, j: { id: 'r1', status: 'rejected' } }
      rejectApprovalRequest.mockResolvedValueOnce(mockResp)
      getApprovalRequests.mockResolvedValueOnce({ requests: [{ id: 'r1', status: 'rejected' }] })
      getPendingApprovals.mockResolvedValueOnce({ requests: [] })

      const store = useApprovalStore()
      const body = { comment: '不同意' }
      const r = await store.reject('r1', body)

      expect(rejectApprovalRequest).toHaveBeenCalledWith('r1', body)
      expect(getApprovalRequests).toHaveBeenCalled()
      expect(getPendingApprovals).toHaveBeenCalled()
      expect(store.requests).toEqual([{ id: 'r1', status: 'rejected' }])
      expect(r).toEqual(mockResp)
    })

    it('失败时设置 error、结束 loading 并向上抛出', async () => {
      const err = { j: { error: '状态非法' } }
      rejectApprovalRequest.mockRejectedValueOnce(err)

      const store = useApprovalStore()
      await expect(store.reject('r1', {})).rejects.toThrow()

      expect(store.error).toBe('状态非法')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      rejectApprovalRequest.mockRejectedValueOnce({})

      const store = useApprovalStore()
      await expect(store.reject('r1', {})).rejects.toThrow()

      expect(store.error).toBe('审批拒绝失败')
    })
  })

  describe('fetchPending 动作', () => {
    it('成功时设置 pending（r.requests 形态）', async () => {
      const mockPending = [{ id: 'r1', status: 'pending' }]
      getPendingApprovals.mockResolvedValueOnce({ requests: mockPending })

      const store = useApprovalStore()
      await store.fetchPending()

      expect(getPendingApprovals).toHaveBeenCalled()
      expect(store.pending).toEqual(mockPending)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('成功时设置 pending（裸数组形态）', async () => {
      const mockPending = [{ id: 'r1' }]
      getPendingApprovals.mockResolvedValueOnce(mockPending)

      const store = useApprovalStore()
      await store.fetchPending()

      expect(store.pending).toEqual(mockPending)
    })

    it('API 返回 null 时 pending 为空数组', async () => {
      getPendingApprovals.mockResolvedValueOnce(null)

      const store = useApprovalStore()
      await store.fetchPending()

      expect(store.pending).toEqual([])
    })

    it('失败时设置 error 并结束 loading', async () => {
      const err = { j: { error: '网络异常' } }
      getPendingApprovals.mockRejectedValueOnce(err)

      const store = useApprovalStore()
      await store.fetchPending()

      expect(store.error).toBe('网络异常')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getPendingApprovals.mockRejectedValueOnce({})

      const store = useApprovalStore()
      await store.fetchPending()

      expect(store.error).toBe('待审批列表拉取失败')
    })
  })
})