// deploy store 单元测试
// 覆盖：初始状态、fetchList/create/execute/rollback/open 动作。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

// mock @/api/deploy：避免真实网络请求，用 vi.fn 控制返回值
vi.mock('@/api/deploy', () => ({
  getDeploys: vi.fn(),
  createDeploy: vi.fn(),
  executeDeploy: vi.fn(),
  rollbackDeploy: vi.fn(),
  getDeploy: vi.fn(),
}))

import { useDeployStore } from '@/stores/deploy'
import { getDeploys, createDeploy, executeDeploy, rollbackDeploy, getDeploy } from '@/api/deploy'

describe('useDeployStore', () => {
  beforeEach(() => {
    // 每个测试前创建新 pinia 实例，隔离状态
    setActivePinia(createPinia())
    // 重置 mock 调用记录
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('list 初始为空数组', () => {
      const store = useDeployStore()
      expect(store.list).toEqual([])
    })

    it('statusFilter 初始为空字符串', () => {
      const store = useDeployStore()
      expect(store.statusFilter).toBe('')
    })

    it('current 初始为 null', () => {
      const store = useDeployStore()
      expect(store.current).toBeNull()
    })

    it('loading 初始为 false', () => {
      const store = useDeployStore()
      expect(store.loading).toBe(false)
    })

    it('error 初始为空字符串', () => {
      const store = useDeployStore()
      expect(store.error).toBe('')
    })
  })

  describe('fetchList 动作', () => {
    it('成功时设置 list', async () => {
      const mockList = [{ id: 'd1', status: 'success' }, { id: 'd2', status: 'running' }]
      getDeploys.mockResolvedValueOnce(mockList)

      const store = useDeployStore()
      await store.fetchList()

      expect(getDeploys).toHaveBeenCalledWith('')
      expect(store.list).toEqual(mockList)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('带 statusFilter 时传给 API', async () => {
      getDeploys.mockResolvedValueOnce([])
      const store = useDeployStore()
      store.statusFilter = 'running'

      await store.fetchList()

      expect(getDeploys).toHaveBeenCalledWith('running')
    })

    it('API 返回 null 时 list 为空数组', async () => {
      getDeploys.mockResolvedValueOnce(null)

      const store = useDeployStore()
      await store.fetchList()

      expect(store.list).toEqual([])
    })

    it('失败时设置 error 并结束 loading', async () => {
      getDeploys.mockRejectedValueOnce({ j: { error: '权限不足' } })

      const store = useDeployStore()
      await store.fetchList()

      expect(store.error).toBe('权限不足')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用默认错误码', async () => {
      getDeploys.mockRejectedValueOnce({})

      const store = useDeployStore()
      await store.fetchList()

      expect(store.error).toBe('部署列表拉取失败')
    })
  })

  describe('create 动作', () => {
    it('成功时返回结果并刷新列表', async () => {
      const body = { app: 'web', version: '1.0.0' }
      createDeploy.mockResolvedValueOnce({ s: 200, j: { id: 'd1' } })
      getDeploys.mockResolvedValueOnce([{ id: 'd1' }])

      const store = useDeployStore()
      const r = await store.create(body)

      expect(createDeploy).toHaveBeenCalledWith(body)
      expect(getDeploys).toHaveBeenCalled()
      expect(r).toEqual({ s: 200, j: { id: 'd1' } })
    })

    it('API 抛错时向上抛出异常', async () => {
      createDeploy.mockRejectedValueOnce(new Error('create failed'))

      const store = useDeployStore()
      await expect(store.create({})).rejects.toThrow('create failed')
    })
  })

  describe('execute 动作', () => {
    it('成功时返回结果并刷新列表', async () => {
      executeDeploy.mockResolvedValueOnce({ s: 200, j: { ok: true } })
      getDeploys.mockResolvedValueOnce([])

      const store = useDeployStore()
      const r = await store.execute('d1')

      expect(executeDeploy).toHaveBeenCalledWith('d1')
      expect(getDeploys).toHaveBeenCalled()
      expect(r).toEqual({ s: 200, j: { ok: true } })
    })

    it('API 抛错时向上抛出异常', async () => {
      executeDeploy.mockRejectedValueOnce(new Error('execute failed'))

      const store = useDeployStore()
      await expect(store.execute('d1')).rejects.toThrow('execute failed')
    })
  })

  describe('rollback 动作', () => {
    it('成功时返回结果并刷新列表', async () => {
      rollbackDeploy.mockResolvedValueOnce({ s: 200, j: { ok: true } })
      getDeploys.mockResolvedValueOnce([])

      const store = useDeployStore()
      const r = await store.rollback('d1')

      expect(rollbackDeploy).toHaveBeenCalledWith('d1')
      expect(getDeploys).toHaveBeenCalled()
      expect(r).toEqual({ s: 200, j: { ok: true } })
    })

    it('API 抛错时向上抛出异常', async () => {
      rollbackDeploy.mockRejectedValueOnce(new Error('rollback failed'))

      const store = useDeployStore()
      await expect(store.rollback('d1')).rejects.toThrow('rollback failed')
    })
  })

  describe('open 动作', () => {
    it('成功时设置 current', async () => {
      const mockDeploy = { id: 'd1', status: 'success', steps: [] }
      getDeploy.mockResolvedValueOnce(mockDeploy)

      const store = useDeployStore()
      await store.open('d1')

      expect(getDeploy).toHaveBeenCalledWith('d1')
      expect(store.current).toEqual(mockDeploy)
    })

    it('失败时设置 error（优先使用后端文案）', async () => {
      getDeploy.mockRejectedValueOnce({ j: { error: '部署不存在' } })

      const store = useDeployStore()
      await store.open('d1')

      expect(store.error).toBe('部署不存在')
    })

    it('失败且无 error 字段时使用默认错误码', async () => {
      getDeploy.mockRejectedValueOnce({})

      const store = useDeployStore()
      await store.open('d1')

      expect(store.error).toBe('部署详情拉取失败')
    })
  })
})