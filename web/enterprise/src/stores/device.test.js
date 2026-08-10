// device store 单元测试
// 覆盖：初始状态、fetchDevices/openDevice/provision 动作、fetchMetrics、getters。
// 说明：源码中方法名为 fetchDevices/openDevice/provision（任务描述中的
//      fetchDeviceById/createDevice 为别名语义），本测试按实际导出方法验证。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

// mock @/api/device：避免真实网络请求，用 vi.fn 控制返回值
vi.mock('@/api/device', () => ({
  getDevices: vi.fn(),
  getDevice: vi.fn(),
  provisionDevice: vi.fn(),
  getAgents: vi.fn(),
  getMetrics: vi.fn(),
}))

import { useDeviceStore } from '@/stores/device'
import { getDevices, getDevice, provisionDevice, getMetrics } from '@/api/device'

describe('useDeviceStore', () => {
  beforeEach(() => {
    // 每个测试前创建新 pinia 实例，隔离状态
    setActivePinia(createPinia())
    // 重置 mock 调用记录
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('segments 初始为空对象', () => {
      const store = useDeviceStore()
      expect(store.segments).toEqual({})
    })

    it('current 初始为 null', () => {
      const store = useDeviceStore()
      expect(store.current).toBeNull()
    })

    it('metrics 初始为 null', () => {
      const store = useDeviceStore()
      expect(store.metrics).toBeNull()
    })

    it('loading 初始为 false', () => {
      const store = useDeviceStore()
      expect(store.loading).toBe(false)
    })

    it('error 初始为空字符串', () => {
      const store = useDeviceStore()
      expect(store.error).toBe('')
    })
  })

  describe('fetchDevices 动作', () => {
    it('成功时按网段分组设置 segments', async () => {
      const mockSegments = { '10.0.0.0/24': [{ id: 'd1', state: 'managed' }] }
      getDevices.mockResolvedValueOnce(mockSegments)

      const store = useDeviceStore()
      await store.fetchDevices()

      expect(getDevices).toHaveBeenCalled()
      expect(store.segments).toEqual(mockSegments)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('API 返回 null 时 segments 为空对象', async () => {
      getDevices.mockResolvedValueOnce(null)

      const store = useDeviceStore()
      await store.fetchDevices()

      expect(store.segments).toEqual({})
    })

    it('失败时设置 error 并结束 loading', async () => {
      const err = { j: { error: '网络异常' } }
      getDevices.mockRejectedValueOnce(err)

      const store = useDeviceStore()
      await store.fetchDevices()

      expect(store.error).toBe('网络异常')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getDevices.mockRejectedValueOnce({})

      const store = useDeviceStore()
      await store.fetchDevices()

      // i18n 默认回退到 zh：error.deviceListFailed → "设备列表拉取失败"
      expect(store.error).toBe('设备列表拉取失败')
    })
  })

  describe('openDevice 动作', () => {
    it('成功时设置 current', async () => {
      const mockDevice = { id: 'd1', hostname: 'host-1', state: 'managed' }
      getDevice.mockResolvedValueOnce(mockDevice)

      const store = useDeviceStore()
      await store.openDevice('d1')

      expect(getDevice).toHaveBeenCalledWith('d1')
      expect(store.current).toEqual(mockDevice)
    })

    it('失败时设置 error（i18n 默认错误码）', async () => {
      getDevice.mockRejectedValueOnce({})

      const store = useDeviceStore()
      await store.openDevice('d1')

      expect(store.error).toBe('设备详情拉取失败')
    })

    it('失败时优先使用后端返回的 error 文案', async () => {
      getDevice.mockRejectedValueOnce({ j: { error: '设备不存在' } })

      const store = useDeviceStore()
      await store.openDevice('d1')

      expect(store.error).toBe('设备不存在')
    })
  })

  describe('provision 动作', () => {
    it('成功时返回结果并刷新列表', async () => {
      provisionDevice.mockResolvedValueOnce({ s: 200, j: { ok: true } })
      getDevices.mockResolvedValueOnce({ '10.0.0.0/24': [] })

      const store = useDeviceStore()
      const r = await store.provision('d1')

      expect(provisionDevice).toHaveBeenCalledWith('d1')
      expect(getDevices).toHaveBeenCalled()
      expect(r).toEqual({ s: 200, j: { ok: true } })
    })

    it('API 抛错时向上抛出异常', async () => {
      provisionDevice.mockRejectedValueOnce(new Error('provision failed'))

      const store = useDeviceStore()
      await expect(store.provision('d1')).rejects.toThrow('provision failed')
    })
  })

  describe('fetchMetrics 动作', () => {
    it('成功时设置 metrics', async () => {
      const mockMetrics = { cpu: 0.5, mem: 0.3 }
      getMetrics.mockResolvedValueOnce(mockMetrics)

      const store = useDeviceStore()
      await store.fetchMetrics('d1')

      expect(getMetrics).toHaveBeenCalledWith('d1')
      expect(store.metrics).toEqual(mockMetrics)
      expect(store.metricsLoading).toBe(false)
      expect(store.metricsError).toBe('')
    })

    it('失败时设置 metricsError 并清空 metrics', async () => {
      getMetrics.mockRejectedValueOnce({ j: { error: 'metrics unavailable' } })

      const store = useDeviceStore()
      await store.fetchMetrics('d1')

      expect(store.metricsError).toBe('metrics unavailable')
      expect(store.metrics).toBeNull()
      expect(store.metricsLoading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getMetrics.mockRejectedValueOnce({})

      const store = useDeviceStore()
      await store.fetchMetrics('d1')

      expect(store.metricsError).toBe('监控指标拉取失败')
    })
  })

  describe('clearMetrics 动作', () => {
    it('清空 metrics 与 metricsError', () => {
      const store = useDeviceStore()
      store.metrics = { cpu: 0.5 }
      store.metricsError = 'err'

      store.clearMetrics()

      expect(store.metrics).toBeNull()
      expect(store.metricsError).toBe('')
    })
  })

  describe('closeDrawer 动作', () => {
    it('清空 current', () => {
      const store = useDeviceStore()
      store.current = { id: 'd1' }

      store.closeDrawer()

      expect(store.current).toBeNull()
    })
  })

  describe('getters', () => {
    it('total 返回所有网段设备总数', () => {
      const store = useDeviceStore()
      store.segments = {
        'a': [{ id: 'd1' }, { id: 'd2' }],
        'b': [{ id: 'd3' }]
      }
      expect(store.total).toBe(3)
    })

    it('managed 仅统计 state=managed 或有 agentID 的设备', () => {
      const store = useDeviceStore()
      store.segments = {
        'a': [
          { id: 'd1', state: 'managed' },
          { id: 'd2', state: 'discovered' },
          { id: 'd3', agentID: 'agent-1' }
        ]
      }
      expect(store.managed).toBe(2)
    })

    it('flat 展平所有网段设备并附加 segment 字段', () => {
      const store = useDeviceStore()
      store.segments = {
        'a': [{ id: 'd1' }],
        'b': [{ id: 'd2' }]
      }
      const flat = store.flat
      expect(flat).toHaveLength(2)
      expect(flat[0]).toEqual({ id: 'd1', segment: 'a' })
      expect(flat[1]).toEqual({ id: 'd2', segment: 'b' })
    })
  })
})