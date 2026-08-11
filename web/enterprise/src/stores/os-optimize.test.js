// os-optimize store 单元测试
// 覆盖：初始状态、fetchTemplates/fetchDetail/fetchDevices/execute/setCategory/clearCurrent 动作。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

// mock @/api/os-optimize 与 @/api/device：避免真实网络请求
vi.mock('@/api/os-optimize', () => ({
  getOSTemplates: vi.fn(),
  getOSTemplate: vi.fn(),
  executeOSTemplate: vi.fn(),
}))

vi.mock('@/api/device', () => ({
  getDevices: vi.fn(),
}))

import { useOSOptimizeStore } from '@/stores/os-optimize'
import { getOSTemplates, getOSTemplate, executeOSTemplate } from '@/api/os-optimize'
import { getDevices } from '@/api/device'

describe('useOSOptimizeStore', () => {
  beforeEach(() => {
    // 每个测试前创建新 pinia 实例，隔离状态
    setActivePinia(createPinia())
    // 重置 mock 调用记录
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('templates 初始为空数组', () => {
      const store = useOSOptimizeStore()
      expect(store.templates).toEqual([])
    })

    it('current 初始为 null', () => {
      const store = useOSOptimizeStore()
      expect(store.current).toBeNull()
    })

    it('devices 初始为空数组', () => {
      const store = useOSOptimizeStore()
      expect(store.devices).toEqual([])
    })

    it('category 初始为空字符串', () => {
      const store = useOSOptimizeStore()
      expect(store.category).toBe('')
    })

    it('loading 初始为 false', () => {
      const store = useOSOptimizeStore()
      expect(store.loading).toBe(false)
    })

    it('error 初始为空字符串', () => {
      const store = useOSOptimizeStore()
      expect(store.error).toBe('')
    })
  })

  describe('fetchTemplates 动作', () => {
    it('成功时设置 templates', async () => {
      const mockTemplates = [{ id: 't1', name: 'kernel-tune' }]
      getOSTemplates.mockResolvedValueOnce(mockTemplates)

      const store = useOSOptimizeStore()
      await store.fetchTemplates()

      expect(getOSTemplates).toHaveBeenCalledWith('')
      expect(store.templates).toEqual(mockTemplates)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('带 category 时传给 API', async () => {
      getOSTemplates.mockResolvedValueOnce([])
      const store = useOSOptimizeStore()
      store.category = 'kernel'

      await store.fetchTemplates()

      expect(getOSTemplates).toHaveBeenCalledWith('kernel')
    })

    it('API 返回 null 时 templates 为空数组', async () => {
      getOSTemplates.mockResolvedValueOnce(null)

      const store = useOSOptimizeStore()
      await store.fetchTemplates()

      expect(store.templates).toEqual([])
    })

    it('失败时设置 error 并结束 loading', async () => {
      getOSTemplates.mockRejectedValueOnce({ j: { error: '权限不足' } })

      const store = useOSOptimizeStore()
      await store.fetchTemplates()

      expect(store.error).toBe('权限不足')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用默认错误码', async () => {
      getOSTemplates.mockRejectedValueOnce({})

      const store = useOSOptimizeStore()
      await store.fetchTemplates()

      expect(store.error).toBe('OS 优化模板列表拉取失败')
    })
  })

  describe('fetchDetail 动作', () => {
    it('成功时设置 current', async () => {
      const mockDetail = { id: 't1', name: 'kernel-tune', steps: [] }
      getOSTemplate.mockResolvedValueOnce(mockDetail)

      const store = useOSOptimizeStore()
      await store.fetchDetail('t1')

      expect(getOSTemplate).toHaveBeenCalledWith('t1')
      expect(store.current).toEqual(mockDetail)
    })

    it('失败时设置 error（优先使用后端文案）', async () => {
      getOSTemplate.mockRejectedValueOnce({ j: { error: '模板不存在' } })

      const store = useOSOptimizeStore()
      await store.fetchDetail('t1')

      expect(store.error).toBe('模板不存在')
    })

    it('失败且无 error 字段时使用默认错误码', async () => {
      getOSTemplate.mockRejectedValueOnce({})

      const store = useOSOptimizeStore()
      await store.fetchDetail('t1')

      expect(store.error).toBe('OS 优化模板详情拉取失败')
    })
  })

  describe('fetchDevices 动作', () => {
    it('返回数组时直接赋值给 devices', async () => {
      const mockDevs = [{ id: 'd1' }, { id: 'd2' }]
      getDevices.mockResolvedValueOnce(mockDevs)

      const store = useOSOptimizeStore()
      await store.fetchDevices()

      expect(store.devices).toEqual(mockDevs)
    })

    it('返回按网段分组的对象时展平为 devices', async () => {
      const mockGrouped = {
        '10.0.0.0/24': [{ id: 'd1' }],
        '192.168.0.0/24': [{ id: 'd2' }, { id: 'd3' }]
      }
      getDevices.mockResolvedValueOnce(mockGrouped)

      const store = useOSOptimizeStore()
      await store.fetchDevices()

      expect(store.devices).toHaveLength(3)
      expect(store.devices.map((d) => d.id).sort()).toEqual(['d1', 'd2', 'd3'])
    })

    it('API 抛错时 devices 回退为空数组', async () => {
      getDevices.mockRejectedValueOnce(new Error('network error'))

      const store = useOSOptimizeStore()
      await store.fetchDevices()

      expect(store.devices).toEqual([])
    })
  })

  describe('execute 动作', () => {
    it('调用 executeOSTemplate 并返回结果', async () => {
      executeOSTemplate.mockResolvedValueOnce({ s: 200, j: { taskID: 'task-1' } })

      const store = useOSOptimizeStore()
      const r = await store.execute('t1', 'agent-1', { param: 'value' })

      expect(executeOSTemplate).toHaveBeenCalledWith('t1', 'agent-1', { param: 'value' })
      expect(r).toEqual({ s: 200, j: { taskID: 'task-1' } })
    })

    it('API 抛错时向上抛出异常', async () => {
      executeOSTemplate.mockRejectedValueOnce(new Error('execute failed'))

      const store = useOSOptimizeStore()
      await expect(store.execute('t1', 'agent-1', {})).rejects.toThrow('execute failed')
    })
  })

  describe('setCategory 动作', () => {
    it('设置 category 并触发 fetchTemplates', async () => {
      getOSTemplates.mockResolvedValueOnce([])

      const store = useOSOptimizeStore()
      await store.setCategory('kernel')

      expect(store.category).toBe('kernel')
      expect(getOSTemplates).toHaveBeenCalledWith('kernel')
    })

    it('传入空值时 category 置空', async () => {
      getOSTemplates.mockResolvedValueOnce([])

      const store = useOSOptimizeStore()
      await store.setCategory()

      expect(store.category).toBe('')
      expect(getOSTemplates).toHaveBeenCalledWith('')
    })
  })

  describe('clearCurrent 动作', () => {
    it('清空 current', () => {
      const store = useOSOptimizeStore()
      store.current = { id: 't1' }

      store.clearCurrent()

      expect(store.current).toBeNull()
    })
  })
})