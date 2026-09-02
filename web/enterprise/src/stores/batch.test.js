// batch store 单元测试
// 覆盖：初始状态、exec/dispatch/fetchStatus/clearCurrent 动作、loading/error 状态、
//      history 累积与上限裁剪、lastBatch/current 状态变化。
// 说明：源码中方法名为 exec/dispatch/fetchStatus（任务描述中的
//      createBatchTask/getBatchStatus 为别名语义），本测试按实际导出方法验证。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

// mock @/api/batch：避免真实网络请求，用 vi.fn 控制返回值
vi.mock('@/api/batch', () => ({
  batchExec: vi.fn(),
  getBatchStatus: vi.fn(),
  batchDispatch: vi.fn()
}))

import { useBatchStore } from '@/stores/batch'
import { batchExec, getBatchStatus, batchDispatch } from '@/api/batch'

describe('useBatchStore', () => {
  beforeEach(() => {
    // 每个测试前创建新 pinia 实例，隔离状态
    setActivePinia(createPinia())
    // 重置 mock 调用记录
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('lastBatch 初始为 null', () => {
      const store = useBatchStore()
      expect(store.lastBatch).toBeNull()
    })

    it('current 初始为 null', () => {
      const store = useBatchStore()
      expect(store.current).toBeNull()
    })

    it('history 初始为空数组', () => {
      const store = useBatchStore()
      expect(store.history).toEqual([])
    })

    it('loading 初始为 false', () => {
      const store = useBatchStore()
      expect(store.loading).toBe(false)
    })

    it('error 初始为空字符串', () => {
      const store = useBatchStore()
      expect(store.error).toBe('')
    })
  })

  describe('exec 动作', () => {
    it('成功时设置 lastBatch 并写入 history', async () => {
      const body = { agentIDs: ['a1'], type: 'shell', command: 'ls' }
      const batch = { batchID: 'b1', status: 'running', total: 1 }
      batchExec.mockResolvedValueOnce({ s: 200, j: batch })

      const store = useBatchStore()
      const r = await store.exec(body)

      expect(batchExec).toHaveBeenCalledWith(body)
      expect(store.lastBatch).toEqual(batch)
      expect(store.history).toHaveLength(1)
      expect(store.history[0]).toEqual(batch)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
      expect(r).toEqual({ s: 200, j: batch })
    })

    it('响应无 batchID 时不写入 history', async () => {
      batchExec.mockResolvedValueOnce({ s: 200, j: { status: 'unknown' } })

      const store = useBatchStore()
      await store.exec({})

      expect(store.lastBatch).toEqual({ status: 'unknown' })
      expect(store.history).toEqual([])
    })

    it('响应 j 为空对象时 lastBatch 为空对象且不写入 history', async () => {
      batchExec.mockResolvedValueOnce({ s: 200 })

      const store = useBatchStore()
      await store.exec({})

      expect(store.lastBatch).toEqual({})
      expect(store.history).toEqual([])
    })

    it('失败时设置 error、结束 loading 并向上抛出异常', async () => {
      batchExec.mockRejectedValueOnce({ j: { error: '执行被拒' } })

      const store = useBatchStore()
      await expect(store.exec({})).rejects.toEqual({ j: { error: '执行被拒' } })

      expect(store.error).toBe('执行被拒')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      batchExec.mockRejectedValueOnce({})

      const store = useBatchStore()
      await expect(store.exec({})).rejects.toEqual({})

      // i18n 默认回退到 zh：error.batchExecFailed → "批量执行失败"
      expect(store.error).toBe('批量执行失败')
    })
  })

  describe('dispatch 动作', () => {
    it('成功时设置 lastBatch 并写入 history', async () => {
      const body = { agentIDs: ['a1', 'a2'], type: 'service', command: 'restart' }
      const batch = { batchID: 'b2', status: 'running', total: 2 }
      batchDispatch.mockResolvedValueOnce({ s: 200, j: batch })

      const store = useBatchStore()
      const r = await store.dispatch(body)

      expect(batchDispatch).toHaveBeenCalledWith(body)
      expect(store.lastBatch).toEqual(batch)
      expect(store.history).toHaveLength(1)
      expect(store.history[0]).toEqual(batch)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
      expect(r).toEqual({ s: 200, j: batch })
    })

    it('响应无 batchID 时不写入 history', async () => {
      batchDispatch.mockResolvedValueOnce({ s: 200, j: { status: 'unknown' } })

      const store = useBatchStore()
      await store.dispatch({})

      expect(store.lastBatch).toEqual({ status: 'unknown' })
      expect(store.history).toEqual([])
    })

    it('失败时设置 error、结束 loading 并向上抛出异常', async () => {
      batchDispatch.mockRejectedValueOnce({ j: { error: '下发失败' } })

      const store = useBatchStore()
      await expect(store.dispatch({})).rejects.toEqual({ j: { error: '下发失败' } })

      expect(store.error).toBe('下发失败')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      batchDispatch.mockRejectedValueOnce({})

      const store = useBatchStore()
      await expect(store.dispatch({})).rejects.toEqual({})

      // i18n 默认回退到 zh：error.batchExecFailed → "批量执行失败"
      expect(store.error).toBe('批量执行失败')
    })
  })

  describe('fetchStatus 动作', () => {
    it('成功时设置 current 并返回 current', async () => {
      const mockStatus = { batchID: 'b1', status: 'succeeded', total: 3, succeeded: 3, failed: 0 }
      getBatchStatus.mockResolvedValueOnce(mockStatus)

      const store = useBatchStore()
      const r = await store.fetchStatus('b1')

      expect(getBatchStatus).toHaveBeenCalledWith('b1')
      expect(store.current).toEqual(mockStatus)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
      expect(r).toEqual(mockStatus)
    })

    it('失败时设置 error、结束 loading 并向上抛出异常', async () => {
      getBatchStatus.mockRejectedValueOnce({ j: { error: '批次不存在' } })

      const store = useBatchStore()
      await expect(store.fetchStatus('b1')).rejects.toEqual({ j: { error: '批次不存在' } })

      expect(store.error).toBe('批次不存在')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getBatchStatus.mockRejectedValueOnce({})

      const store = useBatchStore()
      await expect(store.fetchStatus('b1')).rejects.toEqual({})

      // i18n 默认回退到 zh：error.batchStatusFailed → "批量状态查询失败"
      expect(store.error).toBe('批量状态查询失败')
    })
  })

  describe('clearCurrent 动作', () => {
    it('清空 current', () => {
      const store = useBatchStore()
      store.current = { batchID: 'b1' }

      store.clearCurrent()

      expect(store.current).toBeNull()
    })
  })

  describe('history 累积与上限裁剪', () => {
    it('多次 exec 后 history 按时间倒序累积（最新在前）', async () => {
      const b1 = { batchID: 'b1', status: 'running' }
      const b2 = { batchID: 'b2', status: 'running' }
      batchExec.mockResolvedValueOnce({ s: 200, j: b1 })
      batchExec.mockResolvedValueOnce({ s: 200, j: b2 })

      const store = useBatchStore()
      await store.exec({})
      await store.exec({})

      expect(store.history).toHaveLength(2)
      // unshift：最新在前
      expect(store.history[0]).toEqual(b2)
      expect(store.history[1]).toEqual(b1)
    })

    it('history 超过 50 条时裁剪最旧记录', async () => {
      const store = useBatchStore()
      // 模拟 51 次成功执行
      for (let i = 0; i < 51; i++) {
        batchExec.mockResolvedValueOnce({ s: 200, j: { batchID: `b${i}`, status: 'running' } })
        await store.exec({})
      }

      // 上限 50：最旧的 b0 被裁剪，最新 b50 在最前
      expect(store.history).toHaveLength(50)
      expect(store.history[0].batchID).toBe('b50')
      expect(store.history[49].batchID).toBe('b1')
    })
  })
})