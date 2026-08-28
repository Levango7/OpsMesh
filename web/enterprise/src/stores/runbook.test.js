// runbook store 单元测试
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

vi.mock('@/api/runbook', () => ({
  getRunbooks: vi.fn(),
  createRunbook: vi.fn(),
  updateRunbook: vi.fn(),
  deleteRunbook: vi.fn(),
  executeRunbook: vi.fn(),
  getRunbookExecutions: vi.fn(),
  getExecutionLogs: vi.fn()
}))

import { useRunbookStore } from '@/stores/runbook'
import {
  getRunbooks,
  createRunbook,
  deleteRunbook,
  executeRunbook,
  getRunbookExecutions,
  getExecutionLogs
} from '@/api/runbook'

describe('useRunbookStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('runbooks 初始为空数组', () => {
      const store = useRunbookStore()
      expect(store.runbooks).toEqual([])
    })

    it('executions 初始为空数组', () => {
      const store = useRunbookStore()
      expect(store.executions).toEqual([])
    })

    it('currentRunbookId 初始为空字符串', () => {
      const store = useRunbookStore()
      expect(store.currentRunbookId).toBe('')
    })
  })

  describe('fetchRunbooks 动作', () => {
    it('成功时设置 runbooks', async () => {
      const mockRunbooks = [{ id: 'r1', name: 'deploy-check', status: 'active' }]
      getRunbooks.mockResolvedValueOnce({ runbooks: mockRunbooks })

      const store = useRunbookStore()
      await store.fetchRunbooks()

      expect(store.runbooks).toEqual(mockRunbooks)
      expect(store.loading).toBe(false)
    })

    it('失败时设置 error', async () => {
      getRunbooks.mockRejectedValueOnce({ j: { error: '获取失败' } })

      const store = useRunbookStore()
      await store.fetchRunbooks()

      expect(store.error).toBe('获取失败')
    })
  })

  describe('addRunbook 动作', () => {
    it('成功时返回结果', async () => {
      createRunbook.mockResolvedValueOnce({ s: 200, j: { id: 'r1' } })

      const store = useRunbookStore()
      const r = await store.addRunbook('test', 'desc', 'content', [])

      expect(createRunbook).toHaveBeenCalledWith('test', 'desc', 'content', [])
      expect(r).toEqual({ s: 200, j: { id: 'r1' } })
    })
  })

  describe('removeRunbook 动作', () => {
    it('成功时返回结果', async () => {
      deleteRunbook.mockResolvedValueOnce({ s: 204, j: null })

      const store = useRunbookStore()
      const r = await store.removeRunbook('r1')

      expect(deleteRunbook).toHaveBeenCalledWith('r1')
      expect(r).toEqual({ s: 204, j: null })
    })
  })

  describe('runRunbook 动作', () => {
    it('成功时返回结果', async () => {
      executeRunbook.mockResolvedValueOnce({ s: 200, j: { executionId: 'e1', status: 'running' } })

      const store = useRunbookStore()
      const r = await store.runRunbook('r1')

      expect(executeRunbook).toHaveBeenCalledWith('r1')
      expect(r).toEqual({ s: 200, j: { executionId: 'e1', status: 'running' } })
    })
  })

  describe('fetchExecutions 动作', () => {
    it('成功时设置 executions', async () => {
      const mockExecs = [{ id: 'e1', status: 'success', startedAt: '2026-01-01' }]
      getRunbookExecutions.mockResolvedValueOnce({ executions: mockExecs })

      const store = useRunbookStore()
      await store.fetchExecutions('r1')

      expect(getRunbookExecutions).toHaveBeenCalledWith('r1')
      expect(store.executions).toEqual(mockExecs)
    })
  })

  describe('fetchLogs 动作', () => {
    it('成功时设置 logsContent', async () => {
      getExecutionLogs.mockResolvedValueOnce({ logs: 'log line 1\nlog line 2' })

      const store = useRunbookStore()
      await store.fetchLogs('r1', 'e1')

      expect(getExecutionLogs).toHaveBeenCalledWith('r1', 'e1')
      expect(store.logsContent).toBe('log line 1\nlog line 2')
    })
  })

  describe('selectRunbook 动作', () => {
    it('设置 currentRunbookId 并拉取执行历史', async () => {
      getRunbookExecutions.mockResolvedValueOnce({ executions: [] })

      const store = useRunbookStore()
      store.selectRunbook('r1')

      expect(store.currentRunbookId).toBe('r1')
    })
  })

  describe('currentRunbook getter', () => {
    it('返回匹配的 runbook', () => {
      const store = useRunbookStore()
      store.runbooks = [{ id: 'r1', name: 'test' }, { id: 'r2', name: 'prod' }]
      store.currentRunbookId = 'r2'

      expect(store.currentRunbook).toEqual({ id: 'r2', name: 'prod' })
    })

    it('未匹配时返回 null', () => {
      const store = useRunbookStore()
      store.runbooks = [{ id: 'r1', name: 'test' }]
      store.currentRunbookId = 'r999'

      expect(store.currentRunbook).toBeNull()
    })
  })
})
