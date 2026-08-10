// middleware store 单元测试
// 覆盖：初始状态、fetchTemplates/fetchDetail/deploy/fetchInstances/uninstall 动作及错误处理。
// 说明：源码中方法名为 fetchTemplates/fetchDetail/deploy/fetchInstances/uninstall
//      （任务描述中的 fetchTemplates/fetchTemplateDetail/deployMiddleware/
//        fetchInstances/uninstallInstance 为别名语义），
//      本测试按实际导出方法验证。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

// mock @/api/middleware：避免真实网络请求，用 vi.fn 控制返回值
vi.mock('@/api/middleware', () => ({
  getMiddlewareTemplates: vi.fn(),
  getMiddlewareTemplate: vi.fn(),
  deployMiddleware: vi.fn(),
  getMiddlewareInstances: vi.fn(),
  uninstallMiddleware: vi.fn(),
}))

// mock @/api/device：fetchDevices 依赖 getDevices
vi.mock('@/api/device', () => ({
  getDevices: vi.fn(),
}))

import { useMiddlewareStore } from '@/stores/middleware'
import {
  getMiddlewareTemplates,
  getMiddlewareTemplate,
  deployMiddleware,
  getMiddlewareInstances,
  uninstallMiddleware
} from '@/api/middleware'
import { getDevices } from '@/api/device'

describe('useMiddlewareStore', () => {
  beforeEach(() => {
    // 每个测试前创建新 pinia 实例，隔离状态
    setActivePinia(createPinia())
    // 重置 mock 调用记录
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('templates 初始为空数组', () => {
      const store = useMiddlewareStore()
      expect(store.templates).toEqual([])
    })

    it('instances 初始为空数组', () => {
      const store = useMiddlewareStore()
      expect(store.instances).toEqual([])
    })

    it('current 初始为 null', () => {
      const store = useMiddlewareStore()
      expect(store.current).toBeNull()
    })

    it('devices 初始为空数组', () => {
      const store = useMiddlewareStore()
      expect(store.devices).toEqual([])
    })

    it('category 初始为空字符串', () => {
      const store = useMiddlewareStore()
      expect(store.category).toBe('')
    })

    it('loading 初始为 false', () => {
      const store = useMiddlewareStore()
      expect(store.loading).toBe(false)
    })

    it('instancesLoading 初始为 false', () => {
      const store = useMiddlewareStore()
      expect(store.instancesLoading).toBe(false)
    })

    it('error 初始为空字符串', () => {
      const store = useMiddlewareStore()
      expect(store.error).toBe('')
    })
  })

  describe('fetchTemplates 动作', () => {
    it('成功时设置 templates', async () => {
      const mockTemplates = [
        { id: 't1', name: 'nginx', category: 'web' },
        { id: 't2', name: 'mysql', category: 'db' }
      ]
      getMiddlewareTemplates.mockResolvedValueOnce(mockTemplates)

      const store = useMiddlewareStore()
      await store.fetchTemplates()

      expect(getMiddlewareTemplates).toHaveBeenCalledWith('')
      expect(store.templates).toEqual(mockTemplates)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('带 category 时传给 API 进行过滤', async () => {
      getMiddlewareTemplates.mockResolvedValueOnce([])

      const store = useMiddlewareStore()
      store.category = 'web'

      await store.fetchTemplates()

      expect(getMiddlewareTemplates).toHaveBeenCalledWith('web')
    })

    it('API 返回 null 时 templates 为空数组', async () => {
      getMiddlewareTemplates.mockResolvedValueOnce(null)

      const store = useMiddlewareStore()
      await store.fetchTemplates()

      expect(store.templates).toEqual([])
    })

    it('失败时设置 error 并结束 loading', async () => {
      getMiddlewareTemplates.mockRejectedValueOnce({ j: { error: '服务不可用' } })

      const store = useMiddlewareStore()
      await store.fetchTemplates()

      expect(store.error).toBe('服务不可用')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getMiddlewareTemplates.mockRejectedValueOnce({})

      const store = useMiddlewareStore()
      await store.fetchTemplates()

      expect(store.error).toBe('中间件模板列表拉取失败')
    })
  })

  describe('fetchDetail 动作', () => {
    it('成功时设置 current', async () => {
      const mockDetail = {
        id: 't1', name: 'nginx', category: 'web', version: '1.25',
        deployTypes: ['docker', 'systemd'], risk: 'low'
      }
      getMiddlewareTemplate.mockResolvedValueOnce(mockDetail)

      const store = useMiddlewareStore()
      await store.fetchDetail('t1')

      expect(getMiddlewareTemplate).toHaveBeenCalledWith('t1')
      expect(store.current).toEqual(mockDetail)
    })

    it('失败时设置 error', async () => {
      getMiddlewareTemplate.mockRejectedValueOnce({ j: { error: '模板不存在' } })

      const store = useMiddlewareStore()
      await store.fetchDetail('missing')

      expect(store.error).toBe('模板不存在')
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getMiddlewareTemplate.mockRejectedValueOnce({})

      const store = useMiddlewareStore()
      await store.fetchDetail('missing')

      expect(store.error).toBe('中间件模板详情拉取失败')
    })
  })

  describe('deploy 动作', () => {
    it('成功时返回结果并透传参数', async () => {
      deployMiddleware.mockResolvedValueOnce({ s: 200, j: { taskID: 'task-1' } })

      const store = useMiddlewareStore()
      const params = { port: 80 }
      const r = await store.deploy('t1', 'agent-1', 'docker', params)

      expect(deployMiddleware).toHaveBeenCalledWith('t1', 'agent-1', 'docker', params)
      expect(r).toEqual({ s: 200, j: { taskID: 'task-1' } })
    })

    it('API 抛错时向上抛出异常', async () => {
      deployMiddleware.mockRejectedValueOnce(new Error('deploy failed'))

      const store = useMiddlewareStore()
      await expect(store.deploy('t1', 'agent-1', 'docker', {})).rejects.toThrow('deploy failed')
    })
  })

  describe('fetchInstances 动作', () => {
    it('成功时设置 instances', async () => {
      const mockInstances = [
        { id: 'i1', templateID: 't1', agentID: 'agent-1', status: 'running' },
        { id: 'i2', templateID: 't2', agentID: 'agent-2', status: 'stopped' }
      ]
      getMiddlewareInstances.mockResolvedValueOnce(mockInstances)

      const store = useMiddlewareStore()
      await store.fetchInstances()

      expect(getMiddlewareInstances).toHaveBeenCalledWith()
      expect(store.instances).toEqual(mockInstances)
      expect(store.instancesLoading).toBe(false)
    })

    it('API 返回 null 时 instances 为空数组', async () => {
      getMiddlewareInstances.mockResolvedValueOnce(null)

      const store = useMiddlewareStore()
      await store.fetchInstances()

      expect(store.instances).toEqual([])
    })

    it('失败时不覆盖主 error 但结束 instancesLoading', async () => {
      getMiddlewareInstances.mockRejectedValueOnce(new Error('boom'))

      const store = useMiddlewareStore()
      store.error = '前置错误'

      await store.fetchInstances()

      // 实例列表失败不覆盖主错误
      expect(store.error).toBe('前置错误')
      expect(store.instancesLoading).toBe(false)
    })
  })

  describe('uninstall 动作', () => {
    it('成功时返回结果并透传参数', async () => {
      uninstallMiddleware.mockResolvedValueOnce({ s: 200, j: { taskID: 'task-2' } })

      const store = useMiddlewareStore()
      const r = await store.uninstall('i1', 'agent-1', 'docker')

      expect(uninstallMiddleware).toHaveBeenCalledWith('i1', 'agent-1', 'docker')
      expect(r).toEqual({ s: 200, j: { taskID: 'task-2' } })
    })

    it('API 抛错时向上抛出异常', async () => {
      uninstallMiddleware.mockRejectedValueOnce(new Error('uninstall failed'))

      const store = useMiddlewareStore()
      await expect(store.uninstall('i1', 'agent-1', 'docker')).rejects.toThrow('uninstall failed')
    })
  })

  describe('fetchDevices 动作', () => {
    it('返回数组时直接赋值', async () => {
      const mockDevs = [{ id: 'd1' }, { id: 'd2' }]
      getDevices.mockResolvedValueOnce(mockDevs)

      const store = useMiddlewareStore()
      await store.fetchDevices()

      expect(store.devices).toEqual(mockDevs)
    })

    it('返回对象时按值展平', async () => {
      getDevices.mockResolvedValueOnce({
        seg1: [{ id: 'd1' }],
        seg2: [{ id: 'd2' }, { id: 'd3' }]
      })

      const store = useMiddlewareStore()
      await store.fetchDevices()

      expect(store.devices).toEqual([{ id: 'd1' }, { id: 'd2' }, { id: 'd3' }])
    })

    it('失败时 devices 为空数组', async () => {
      getDevices.mockRejectedValueOnce(new Error('boom'))

      const store = useMiddlewareStore()
      await store.fetchDevices()

      expect(store.devices).toEqual([])
    })
  })

  describe('setCategory 动作', () => {
    it('设置 category 并触发 fetchTemplates', async () => {
      getMiddlewareTemplates.mockResolvedValueOnce([])

      const store = useMiddlewareStore()
      await store.setCategory('db')

      expect(store.category).toBe('db')
      expect(getMiddlewareTemplates).toHaveBeenCalledWith('db')
    })

    it('传入空值时 category 置空', async () => {
      getMiddlewareTemplates.mockResolvedValueOnce([])

      const store = useMiddlewareStore()
      store.category = 'web'
      await store.setCategory('')

      expect(store.category).toBe('')
      expect(getMiddlewareTemplates).toHaveBeenCalledWith('')
    })
  })

  describe('clearCurrent 动作', () => {
    it('将 current 置为 null', () => {
      const store = useMiddlewareStore()
      store.current = { id: 't1', name: 'nginx' }

      store.clearCurrent()

      expect(store.current).toBeNull()
    })
  })
})