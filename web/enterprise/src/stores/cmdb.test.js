// cmdb store 单元测试
// 覆盖：初始状态、fetchTypes/fetchInstances/fetchTemplates/openGraph/create 动作。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

// mock @/api/cmdb：避免真实网络请求，用 vi.fn 控制返回值
vi.mock('@/api/cmdb', () => ({
  getCMDBTypes: vi.fn(),
  getCIs: vi.fn(),
  createCI: vi.fn(),
  getCIGraph: vi.fn(),
  getAttrTemplates: vi.fn(),
}))

import { useCmdbStore } from '@/stores/cmdb'
import { getCMDBTypes, getCIs, createCI, getCIGraph, getAttrTemplates } from '@/api/cmdb'

describe('useCmdbStore', () => {
  beforeEach(() => {
    // 每个测试前创建新 pinia 实例，隔离状态
    setActivePinia(createPinia())
    // 重置 mock 调用记录
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('types 初始为空数组', () => {
      const store = useCmdbStore()
      expect(store.types).toEqual([])
    })

    it('currentType 初始为空字符串', () => {
      const store = useCmdbStore()
      expect(store.currentType).toBe('')
    })

    it('instances 初始为空数组', () => {
      const store = useCmdbStore()
      expect(store.instances).toEqual([])
    })

    it('templates 初始为空数组', () => {
      const store = useCmdbStore()
      expect(store.templates).toEqual([])
    })

    it('graph 初始为 null', () => {
      const store = useCmdbStore()
      expect(store.graph).toBeNull()
    })

    it('loading 初始为 false', () => {
      const store = useCmdbStore()
      expect(store.loading).toBe(false)
    })

    it('error 初始为空字符串', () => {
      const store = useCmdbStore()
      expect(store.error).toBe('')
    })
  })

  describe('fetchTypes 动作', () => {
    it('成功时设置 types', async () => {
      const mockTypes = [{ name: 'host' }, { name: 'app' }]
      getCMDBTypes.mockResolvedValueOnce(mockTypes)

      const store = useCmdbStore()
      await store.fetchTypes()

      expect(getCMDBTypes).toHaveBeenCalled()
      expect(store.types).toEqual(mockTypes)
    })

    it('API 返回 null 时 types 为空数组', async () => {
      getCMDBTypes.mockResolvedValueOnce(null)

      const store = useCmdbStore()
      await store.fetchTypes()

      expect(store.types).toEqual([])
    })

    it('失败时设置 error（优先使用后端文案）', async () => {
      getCMDBTypes.mockRejectedValueOnce({ j: { error: '权限不足' } })

      const store = useCmdbStore()
      await store.fetchTypes()

      expect(store.error).toBe('权限不足')
    })

    it('失败且无 error 字段时使用默认错误码', async () => {
      getCMDBTypes.mockRejectedValueOnce({})

      const store = useCmdbStore()
      await store.fetchTypes()

      expect(store.error).toBe('CMDB 类型拉取失败')
    })
  })

  describe('fetchInstances 动作', () => {
    it('成功时设置 instances 并更新 currentType', async () => {
      const mockInstances = [{ id: 'ci1', name: 'web-1' }]
      getCIs.mockResolvedValueOnce(mockInstances)

      const store = useCmdbStore()
      await store.fetchInstances('host')

      expect(store.currentType).toBe('host')
      expect(getCIs).toHaveBeenCalledWith('host')
      expect(store.instances).toEqual(mockInstances)
      expect(store.loading).toBe(false)
    })

    it('未传 type 且 currentType 为空时清空 instances 不调用 API', async () => {
      const store = useCmdbStore()
      await store.fetchInstances()

      expect(store.instances).toEqual([])
      expect(getCIs).not.toHaveBeenCalled()
    })

    it('未传 type 时复用 currentType', async () => {
      getCIs.mockResolvedValueOnce([])
      const store = useCmdbStore()
      store.currentType = 'app'

      await store.fetchInstances()

      expect(getCIs).toHaveBeenCalledWith('app')
    })

    it('API 返回 null 时 instances 为空数组', async () => {
      getCIs.mockResolvedValueOnce(null)

      const store = useCmdbStore()
      await store.fetchInstances('host')

      expect(store.instances).toEqual([])
    })

    it('失败时设置 error 并结束 loading', async () => {
      getCIs.mockRejectedValueOnce({ j: { error: '类型不存在' } })

      const store = useCmdbStore()
      await store.fetchInstances('host')

      expect(store.error).toBe('类型不存在')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用默认错误码', async () => {
      getCIs.mockRejectedValueOnce({})

      const store = useCmdbStore()
      await store.fetchInstances('host')

      expect(store.error).toBe('配置项拉取失败')
    })
  })

  describe('fetchTemplates 动作', () => {
    it('成功时设置 templates', async () => {
      const mockTemplates = [{ name: 'cpu', type: 'int' }]
      getAttrTemplates.mockResolvedValueOnce(mockTemplates)

      const store = useCmdbStore()
      await store.fetchTemplates('host')

      expect(getAttrTemplates).toHaveBeenCalledWith('host')
      expect(store.templates).toEqual(mockTemplates)
    })

    it('API 返回 null 时 templates 为空数组', async () => {
      getAttrTemplates.mockResolvedValueOnce(null)

      const store = useCmdbStore()
      await store.fetchTemplates('host')

      expect(store.templates).toEqual([])
    })

    it('失败时设置 error（优先使用后端文案）', async () => {
      getAttrTemplates.mockRejectedValueOnce({ j: { error: '模板缺失' } })

      const store = useCmdbStore()
      await store.fetchTemplates('host')

      expect(store.error).toBe('模板缺失')
    })

    it('失败且无 error 字段时使用默认错误码', async () => {
      getAttrTemplates.mockRejectedValueOnce({})

      const store = useCmdbStore()
      await store.fetchTemplates('host')

      expect(store.error).toBe('属性模板拉取失败')
    })
  })

  describe('openGraph 动作', () => {
    it('成功时设置 graph', async () => {
      const mockGraph = { centerCI: { id: 'ci1' }, relations: [] }
      getCIGraph.mockResolvedValueOnce(mockGraph)

      const store = useCmdbStore()
      await store.openGraph('ci1')

      expect(getCIGraph).toHaveBeenCalledWith('ci1')
      expect(store.graph).toEqual(mockGraph)
    })

    it('失败时设置 error（优先使用后端文案）', async () => {
      getCIGraph.mockRejectedValueOnce({ j: { error: '关系图不存在' } })

      const store = useCmdbStore()
      await store.openGraph('ci1')

      expect(store.error).toBe('关系图不存在')
    })

    it('失败且无 error 字段时使用默认错误码', async () => {
      getCIGraph.mockRejectedValueOnce({})

      const store = useCmdbStore()
      await store.openGraph('ci1')

      expect(store.error).toBe('关系图拉取失败')
    })
  })

  describe('create 动作', () => {
    it('成功时返回结果并刷新实例列表', async () => {
      const body = { type: 'host', name: 'web-1' }
      createCI.mockResolvedValueOnce({ s: 200, j: { id: 'ci1' } })
      getCIs.mockResolvedValueOnce([{ id: 'ci1', name: 'web-1' }])

      const store = useCmdbStore()
      store.currentType = 'host'
      const r = await store.create(body)

      expect(createCI).toHaveBeenCalledWith(body)
      expect(getCIs).toHaveBeenCalled()
      expect(r).toEqual({ s: 200, j: { id: 'ci1' } })
    })

    it('API 抛错时向上抛出异常', async () => {
      createCI.mockRejectedValueOnce(new Error('create failed'))

      const store = useCmdbStore()
      await expect(store.create({})).rejects.toThrow('create failed')
    })
  })
})