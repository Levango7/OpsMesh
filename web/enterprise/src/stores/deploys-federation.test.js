// deploys-federation store 单元测试
// 覆盖：初始状态、fetchDeploys/create/fetchDetail/clearCurrent 动作、
//       loading/error 状态、成功/失败分支、i18n 默认错误码回退。
// 说明：源码中方法名为 fetchDeploys/create/fetchDetail（任务描述中的
//      createDeploy/getDeploy 为别名语义），本测试按实际导出方法验证。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

// mock @/api/deploys-federation：避免真实网络请求，用 vi.fn 控制返回值
vi.mock('@/api/deploys-federation', () => ({
  getFederationDeploys: vi.fn(),
  createFederationDeploy: vi.fn(),
  getFederationDeploy: vi.fn(),
}))

import { useFederationDeploysStore } from '@/stores/deploys-federation'
import {
  getFederationDeploys,
  createFederationDeploy,
  getFederationDeploy,
} from '@/api/deploys-federation'

describe('useFederationDeploysStore', () => {
  beforeEach(() => {
    // 每个测试前创建新 pinia 实例，隔离状态
    setActivePinia(createPinia())
    // 重置 mock 调用记录
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('deploys 初始为空数组', () => {
      const store = useFederationDeploysStore()
      expect(store.deploys).toEqual([])
    })

    it('current 初始为 null', () => {
      const store = useFederationDeploysStore()
      expect(store.current).toBeNull()
    })

    it('loading 初始为 false', () => {
      const store = useFederationDeploysStore()
      expect(store.loading).toBe(false)
    })

    it('error 初始为空字符串', () => {
      const store = useFederationDeploysStore()
      expect(store.error).toBe('')
    })
  })

  describe('fetchDeploys 动作', () => {
    it('成功时从 r.deploys 设置 deploys', async () => {
      const mockResp = { deploys: [{ id: 'dep-1', status: 'running' }] }
      getFederationDeploys.mockResolvedValueOnce(mockResp)

      const store = useFederationDeploysStore()
      await store.fetchDeploys()

      expect(getFederationDeploys).toHaveBeenCalled()
      expect(store.deploys).toEqual([{ id: 'dep-1', status: 'running' }])
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('响应无 deploys 字段但本身是数组时直接使用数组', async () => {
      const mockArr = [{ id: 'dep-1' }, { id: 'dep-2' }]
      getFederationDeploys.mockResolvedValueOnce(mockArr)

      const store = useFederationDeploysStore()
      await store.fetchDeploys()

      expect(store.deploys).toEqual(mockArr)
      expect(store.loading).toBe(false)
    })

    it('API 返回 null 时 deploys 为空数组', async () => {
      getFederationDeploys.mockResolvedValueOnce(null)

      const store = useFederationDeploysStore()
      await store.fetchDeploys()

      expect(store.deploys).toEqual([])
      expect(store.loading).toBe(false)
    })

    it('失败时优先使用后端返回的 error 文案', async () => {
      getFederationDeploys.mockRejectedValueOnce({ j: { error: '联邦部署列表不可达' } })

      const store = useFederationDeploysStore()
      await store.fetchDeploys()

      expect(store.error).toBe('联邦部署列表不可达')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getFederationDeploys.mockRejectedValueOnce({})

      const store = useFederationDeploysStore()
      await store.fetchDeploys()

      // i18n 默认回退到 zh：error.federationDeploysListFailed → "联邦部署列表拉取失败"
      expect(store.error).toBe('联邦部署列表拉取失败')
      expect(store.loading).toBe(false)
    })
  })

  describe('create 动作', () => {
    it('成功时调用 API 并刷新列表，返回结果', async () => {
      const mockResp = { s: 200, j: { id: 'dep-new', status: 'created' } }
      createFederationDeploy.mockResolvedValueOnce(mockResp)
      getFederationDeploys.mockResolvedValueOnce({ deploys: [{ id: 'dep-new' }] })

      const store = useFederationDeploysStore()
      const r = await store.create({ name: 'deploy-1' })

      expect(createFederationDeploy).toHaveBeenCalledWith({ name: 'deploy-1' })
      expect(getFederationDeploys).toHaveBeenCalled()
      expect(store.deploys).toEqual([{ id: 'dep-new' }])
      expect(r).toEqual(mockResp)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('API 抛错时设置 error 并向上抛出异常', async () => {
      const err = { j: { error: '创建参数非法' } }
      createFederationDeploy.mockRejectedValueOnce(err)

      const store = useFederationDeploysStore()
      await expect(store.create({})).rejects.toEqual(err)

      expect(store.error).toBe('创建参数非法')
      expect(store.loading).toBe(false)
      // 失败时不应触发列表刷新
      expect(getFederationDeploys).not.toHaveBeenCalled()
    })

    it('失败且无 error 字段时使用 i18n 默认错误码并抛出', async () => {
      createFederationDeploy.mockRejectedValueOnce(new Error('network'))

      const store = useFederationDeploysStore()
      await expect(store.create({})).rejects.toThrow('network')

      expect(store.error).toBe('联邦部署创建失败')
      expect(store.loading).toBe(false)
    })
  })

  describe('fetchDetail 动作', () => {
    it('成功时设置 current 并返回详情', async () => {
      const mockDetail = { id: 'dep-1', status: 'running', clusters: [] }
      getFederationDeploy.mockResolvedValueOnce(mockDetail)

      const store = useFederationDeploysStore()
      const r = await store.fetchDetail('dep-1')

      expect(getFederationDeploy).toHaveBeenCalledWith('dep-1')
      expect(store.current).toEqual(mockDetail)
      expect(r).toEqual(mockDetail)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('API 返回 null 时 current 为 null', async () => {
      getFederationDeploy.mockResolvedValueOnce(null)

      const store = useFederationDeploysStore()
      const r = await store.fetchDetail('dep-1')

      expect(store.current).toBeNull()
      expect(r).toBeNull()
      expect(store.loading).toBe(false)
    })

    it('失败时设置 error 并向上抛出异常', async () => {
      const err = { j: { error: '部署不存在' } }
      getFederationDeploy.mockRejectedValueOnce(err)

      const store = useFederationDeploysStore()
      await expect(store.fetchDetail('dep-x')).rejects.toEqual(err)

      expect(store.error).toBe('部署不存在')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码并抛出', async () => {
      getFederationDeploy.mockRejectedValueOnce(new Error('network'))

      const store = useFederationDeploysStore()
      await expect(store.fetchDetail('dep-x')).rejects.toThrow('network')

      expect(store.error).toBe('联邦部署详情拉取失败')
      expect(store.loading).toBe(false)
    })
  })

  describe('clearCurrent 动作', () => {
    it('清空 current', () => {
      const store = useFederationDeploysStore()
      store.current = { id: 'dep-1' }

      store.clearCurrent()

      expect(store.current).toBeNull()
    })
  })
})