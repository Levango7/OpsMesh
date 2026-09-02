// canary store 单元测试
// 覆盖：初始状态、create/fetchStatus/advance 动作、流量分割（fetchTrafficSplit/
//      updateTrafficSplit）、指标（fetchMetrics）、clearCurrent、loading/error 状态、
//      list 累积与上限裁剪。
// 说明：源码中方法名为 create/fetchStatus/advance（任务描述中的
//      createCanaryRelease/promoteCanary/abortCanary 为别名语义），本测试按实际导出方法验证。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

// mock @/api/canary：避免真实网络请求，用 vi.fn 控制返回值
vi.mock('@/api/canary', () => ({
  createCanary: vi.fn(),
  getCanaryStatus: vi.fn(),
  advanceCanary: vi.fn(),
  getTrafficSplit: vi.fn(),
  setTrafficSplit: vi.fn(),
  getCanaryMetrics: vi.fn()
}))

import { useCanaryStore } from '@/stores/canary'
import {
  createCanary, getCanaryStatus, advanceCanary,
  getTrafficSplit, setTrafficSplit, getCanaryMetrics
} from '@/api/canary'

describe('useCanaryStore', () => {
  beforeEach(() => {
    // 每个测试前创建新 pinia 实例，隔离状态
    setActivePinia(createPinia())
    // 重置 mock 调用记录
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('current 初始为 null', () => {
      const store = useCanaryStore()
      expect(store.current).toBeNull()
    })

    it('trafficSplit 初始为 null', () => {
      const store = useCanaryStore()
      expect(store.trafficSplit).toBeNull()
    })

    it('metrics 初始为 null', () => {
      const store = useCanaryStore()
      expect(store.metrics).toBeNull()
    })

    it('list 初始为空数组', () => {
      const store = useCanaryStore()
      expect(store.list).toEqual([])
    })

    it('loading 初始为 false', () => {
      const store = useCanaryStore()
      expect(store.loading).toBe(false)
    })

    it('error 初始为空字符串', () => {
      const store = useCanaryStore()
      expect(store.error).toBe('')
    })
  })

  describe('create 动作', () => {
    it('成功时写入 list 并返回结果', async () => {
      const body = { serviceName: 'svc-a', targetVersion: 'v2' }
      const canary = { canaryID: 'c1', status: 'running', currentStep: 0 }
      createCanary.mockResolvedValueOnce({ s: 200, j: canary })

      const store = useCanaryStore()
      const r = await store.create(body)

      expect(createCanary).toHaveBeenCalledWith(body)
      expect(store.list).toHaveLength(1)
      expect(store.list[0]).toEqual(canary)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
      expect(r).toEqual({ s: 200, j: canary })
    })

    it('响应无 canaryID 时不写入 list', async () => {
      createCanary.mockResolvedValueOnce({ s: 200, j: { status: 'unknown' } })

      const store = useCanaryStore()
      await store.create({})

      expect(store.list).toEqual([])
    })

    it('响应 j 为空对象时不写入 list', async () => {
      createCanary.mockResolvedValueOnce({ s: 200 })

      const store = useCanaryStore()
      await store.create({})

      expect(store.list).toEqual([])
    })

    it('失败时设置 error、结束 loading 并向上抛出异常', async () => {
      createCanary.mockRejectedValueOnce({ j: { error: '灰度已存在' } })

      const store = useCanaryStore()
      await expect(store.create({})).rejects.toEqual({ j: { error: '灰度已存在' } })

      expect(store.error).toBe('灰度已存在')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      createCanary.mockRejectedValueOnce({})

      const store = useCanaryStore()
      await expect(store.create({})).rejects.toEqual({})

      // i18n 默认回退到 zh：error.canaryCreateFailed → "灰度发布创建失败"
      expect(store.error).toBe('灰度发布创建失败')
    })
  })

  describe('fetchStatus 动作', () => {
    it('成功时设置 current 并返回 current', async () => {
      const mockStatus = { canaryID: 'c1', status: 'running', currentStep: 1 }
      getCanaryStatus.mockResolvedValueOnce(mockStatus)

      const store = useCanaryStore()
      const r = await store.fetchStatus('c1')

      expect(getCanaryStatus).toHaveBeenCalledWith('c1')
      expect(store.current).toEqual(mockStatus)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
      expect(r).toEqual(mockStatus)
    })

    it('失败时设置 error、结束 loading 并向上抛出异常', async () => {
      getCanaryStatus.mockRejectedValueOnce({ j: { error: '灰度不存在' } })

      const store = useCanaryStore()
      await expect(store.fetchStatus('c1')).rejects.toEqual({ j: { error: '灰度不存在' } })

      expect(store.error).toBe('灰度不存在')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getCanaryStatus.mockRejectedValueOnce({})

      const store = useCanaryStore()
      await expect(store.fetchStatus('c1')).rejects.toEqual({})

      // i18n 默认回退到 zh：error.canaryStatusFailed → "灰度状态查询失败"
      expect(store.error).toBe('灰度状态查询失败')
    })
  })

  describe('advance 动作', () => {
    it('成功时推进并刷新 current', async () => {
      advanceCanary.mockResolvedValueOnce({ s: 200, j: { canaryID: 'c1', currentStep: 2 } })
      const refreshed = { canaryID: 'c1', status: 'running', currentStep: 2 }
      getCanaryStatus.mockResolvedValueOnce(refreshed)

      const store = useCanaryStore()
      const r = await store.advance('c1')

      expect(advanceCanary).toHaveBeenCalledWith('c1')
      expect(getCanaryStatus).toHaveBeenCalledWith('c1')
      expect(store.current).toEqual(refreshed)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
      expect(r).toEqual({ s: 200, j: { canaryID: 'c1', currentStep: 2 } })
    })

    it('推进成功但刷新状态失败时仍返回推进结果', async () => {
      advanceCanary.mockResolvedValueOnce({ s: 200, j: { canaryID: 'c1', currentStep: 2 } })
      // 刷新状态抛错，advance 内部 try/catch 忽略
      getCanaryStatus.mockRejectedValueOnce(new Error('refresh failed'))

      const store = useCanaryStore()
      const r = await store.advance('c1')

      expect(r).toEqual({ s: 200, j: { canaryID: 'c1', currentStep: 2 } })
      expect(store.error).toBe('')
      expect(store.loading).toBe(false)
    })

    it('推进失败时设置 error、结束 loading 并向上抛出异常', async () => {
      advanceCanary.mockRejectedValueOnce({ j: { error: '推进被拒' } })

      const store = useCanaryStore()
      await expect(store.advance('c1')).rejects.toEqual({ j: { error: '推进被拒' } })

      expect(store.error).toBe('推进被拒')
      expect(store.loading).toBe(false)
    })

    it('推进失败且无 error 字段时使用 i18n 默认错误码', async () => {
      advanceCanary.mockRejectedValueOnce({})

      const store = useCanaryStore()
      await expect(store.advance('c1')).rejects.toEqual({})

      // i18n 默认回退到 zh：error.canaryAdvanceFailed → "灰度推进失败"
      expect(store.error).toBe('灰度推进失败')
    })
  })

  describe('fetchTrafficSplit 动作', () => {
    it('成功时设置 trafficSplit 并返回', async () => {
      const mockSplit = { canaryPercent: 30, baselinePercent: 70 }
      getTrafficSplit.mockResolvedValueOnce(mockSplit)

      const store = useCanaryStore()
      const r = await store.fetchTrafficSplit('c1')

      expect(getTrafficSplit).toHaveBeenCalledWith('c1')
      expect(store.trafficSplit).toEqual(mockSplit)
      expect(r).toEqual(mockSplit)
    })

    it('失败时设置 error 并向上抛出异常', async () => {
      getTrafficSplit.mockRejectedValueOnce({ j: { error: '流量查询失败' } })

      const store = useCanaryStore()
      await expect(store.fetchTrafficSplit('c1')).rejects.toEqual({ j: { error: '流量查询失败' } })

      expect(store.error).toBe('流量查询失败')
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getTrafficSplit.mockRejectedValueOnce({})

      const store = useCanaryStore()
      await expect(store.fetchTrafficSplit('c1')).rejects.toEqual({})

      // i18n 默认回退到 zh：error.canaryTrafficFailed → "流量分割操作失败"
      expect(store.error).toBe('流量分割操作失败')
    })
  })

  describe('updateTrafficSplit 动作', () => {
    it('成功时设置流量并刷新 trafficSplit', async () => {
      const body = { canaryPercent: 50 }
      setTrafficSplit.mockResolvedValueOnce({ s: 200, j: { ok: true } })
      const refreshed = { canaryPercent: 50, baselinePercent: 50 }
      getTrafficSplit.mockResolvedValueOnce(refreshed)

      const store = useCanaryStore()
      const r = await store.updateTrafficSplit('c1', body)

      expect(setTrafficSplit).toHaveBeenCalledWith('c1', body)
      expect(getTrafficSplit).toHaveBeenCalledWith('c1')
      expect(store.trafficSplit).toEqual(refreshed)
      expect(r).toEqual({ s: 200, j: { ok: true } })
    })

    it('设置失败时设置 error 并向上抛出异常', async () => {
      setTrafficSplit.mockRejectedValueOnce({ j: { error: '流量设置失败' } })

      const store = useCanaryStore()
      await expect(store.updateTrafficSplit('c1', {})).rejects.toEqual({ j: { error: '流量设置失败' } })

      expect(store.error).toBe('流量设置失败')
    })

    it('设置失败且无 error 字段时使用 i18n 默认错误码', async () => {
      setTrafficSplit.mockRejectedValueOnce({})

      const store = useCanaryStore()
      await expect(store.updateTrafficSplit('c1', {})).rejects.toEqual({})

      // i18n 默认回退到 zh：error.canaryTrafficFailed → "流量分割操作失败"
      expect(store.error).toBe('流量分割操作失败')
    })
  })

  describe('fetchMetrics 动作', () => {
    it('成功时设置 metrics 并返回', async () => {
      const mockMetrics = { canary: { errorRate: 0.01 }, baseline: { errorRate: 0.005 } }
      getCanaryMetrics.mockResolvedValueOnce(mockMetrics)

      const store = useCanaryStore()
      const r = await store.fetchMetrics('c1')

      expect(getCanaryMetrics).toHaveBeenCalledWith('c1')
      expect(store.metrics).toEqual(mockMetrics)
      expect(r).toEqual(mockMetrics)
    })

    it('失败时设置 error 并向上抛出异常', async () => {
      getCanaryMetrics.mockRejectedValueOnce({ j: { error: '指标不可用' } })

      const store = useCanaryStore()
      await expect(store.fetchMetrics('c1')).rejects.toEqual({ j: { error: '指标不可用' } })

      expect(store.error).toBe('指标不可用')
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getCanaryMetrics.mockRejectedValueOnce({})

      const store = useCanaryStore()
      await expect(store.fetchMetrics('c1')).rejects.toEqual({})

      // i18n 默认回退到 zh：error.canaryMetricsFailed → "灰度指标拉取失败"
      expect(store.error).toBe('灰度指标拉取失败')
    })
  })

  describe('clearCurrent 动作', () => {
    it('清空 current、trafficSplit、metrics', () => {
      const store = useCanaryStore()
      store.current = { canaryID: 'c1' }
      store.trafficSplit = { canaryPercent: 30 }
      store.metrics = { canary: {} }

      store.clearCurrent()

      expect(store.current).toBeNull()
      expect(store.trafficSplit).toBeNull()
      expect(store.metrics).toBeNull()
    })
  })

  describe('list 累积与上限裁剪', () => {
    it('多次 create 后 list 按时间倒序累积（最新在前）', async () => {
      const c1 = { canaryID: 'c1', status: 'running' }
      const c2 = { canaryID: 'c2', status: 'running' }
      createCanary.mockResolvedValueOnce({ s: 200, j: c1 })
      createCanary.mockResolvedValueOnce({ s: 200, j: c2 })

      const store = useCanaryStore()
      await store.create({})
      await store.create({})

      expect(store.list).toHaveLength(2)
      // unshift：最新在前
      expect(store.list[0]).toEqual(c2)
      expect(store.list[1]).toEqual(c1)
    })

    it('list 超过 50 条时裁剪最旧记录', async () => {
      const store = useCanaryStore()
      // 模拟 51 次成功创建
      for (let i = 0; i < 51; i++) {
        createCanary.mockResolvedValueOnce({ s: 200, j: { canaryID: `c${i}`, status: 'running' } })
        await store.create({})
      }

      // 上限 50：最旧的 c0 被裁剪，最新 c50 在最前
      expect(store.list).toHaveLength(50)
      expect(store.list[0].canaryID).toBe('c50')
      expect(store.list[49].canaryID).toBe('c1')
    })
  })
})