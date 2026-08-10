// k8s store 单元测试
// 覆盖：初始状态、fetchClusters/createCluster/removeCluster 动作、currentCluster getter。
// 说明：源码中方法名为 createCluster/removeCluster（任务描述中的 addCluster/deleteCluster 为别名语义），
//      本测试按实际导出方法验证。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

// mock @/api/k8s：避免真实网络请求，用 vi.fn 控制返回值
vi.mock('@/api/k8s', () => ({
  getK8sClusters: vi.fn(),
  createK8sCluster: vi.fn(),
  deleteK8sCluster: vi.fn(),
  testK8sCluster: vi.fn(),
  getK8sNamespaces: vi.fn(),
  getK8sPods: vi.fn(),
  getK8sPodLogs: vi.fn(),
  deleteK8sPod: vi.fn(),
  getK8sDeployments: vi.fn(),
  scaleK8sDeployment: vi.fn(),
  restartK8sDeployment: vi.fn(),
  getK8sServices: vi.fn(),
  getK8sConfigMaps: vi.fn(),
  getK8sSecrets: vi.fn(),
  getK8sNodes: vi.fn(),
}))

import { useK8sStore } from '@/stores/k8s'
import {
  getK8sClusters,
  createK8sCluster,
  deleteK8sCluster,
} from '@/api/k8s'

describe('useK8sStore', () => {
  beforeEach(() => {
    // 每个测试前创建新 pinia 实例，隔离状态
    setActivePinia(createPinia())
    // 重置 mock 调用记录
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('clusters 初始为空数组', () => {
      const store = useK8sStore()
      expect(store.clusters).toEqual([])
    })

    it('currentClusterID 初始为空字符串', () => {
      const store = useK8sStore()
      expect(store.currentClusterID).toBe('')
    })

    it('resourceType 初始为 pods', () => {
      const store = useK8sStore()
      expect(store.resourceType).toBe('pods')
    })

    it('loading 初始为 false', () => {
      const store = useK8sStore()
      expect(store.loading).toBe(false)
    })

    it('error 初始为空字符串', () => {
      const store = useK8sStore()
      expect(store.error).toBe('')
    })
  })

  describe('fetchClusters 动作', () => {
    it('成功时设置 clusters（数组形态）', async () => {
      const mockClusters = [{ id: 'c1', name: 'prod', status: 'online' }]
      getK8sClusters.mockResolvedValueOnce(mockClusters)

      const store = useK8sStore()
      await store.fetchClusters()

      expect(getK8sClusters).toHaveBeenCalled()
      expect(store.clusters).toEqual(mockClusters)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('成功时设置 clusters（{clusters: []} 形态）', async () => {
      const mockClusters = [{ id: 'c1', name: 'prod' }]
      getK8sClusters.mockResolvedValueOnce({ clusters: mockClusters })

      const store = useK8sStore()
      await store.fetchClusters()

      expect(store.clusters).toEqual(mockClusters)
    })

    it('API 返回非预期形态时 clusters 为空数组', async () => {
      getK8sClusters.mockResolvedValueOnce({ unexpected: true })

      const store = useK8sStore()
      await store.fetchClusters()

      expect(store.clusters).toEqual([])
    })

    it('失败时设置 error 并结束 loading', async () => {
      getK8sClusters.mockRejectedValueOnce({ j: { error: 'kubeconfig 无效' } })

      const store = useK8sStore()
      await store.fetchClusters()

      expect(store.error).toBe('kubeconfig 无效')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getK8sClusters.mockRejectedValueOnce({})

      const store = useK8sStore()
      await store.fetchClusters()

      expect(store.error).toBe('K8s 集群列表拉取失败')
    })
  })

  describe('createCluster 动作', () => {
    it('成功时返回结果', async () => {
      createK8sCluster.mockResolvedValueOnce({ s: 200, j: { id: 'c1', name: 'prod' } })

      const store = useK8sStore()
      const r = await store.createCluster('prod', 'https://k8s:6443', 'kubeconfig-content')

      expect(createK8sCluster).toHaveBeenCalledWith('prod', 'https://k8s:6443', 'kubeconfig-content')
      expect(r).toEqual({ s: 200, j: { id: 'c1', name: 'prod' } })
    })

    it('API 抛错时向上抛出异常', async () => {
      createK8sCluster.mockRejectedValueOnce(new Error('create cluster failed'))

      const store = useK8sStore()
      await expect(store.createCluster('p', 's', 'k')).rejects.toThrow('create cluster failed')
    })
  })

  describe('removeCluster 动作', () => {
    it('成功时返回结果', async () => {
      deleteK8sCluster.mockResolvedValueOnce({ s: 204, j: null })

      const store = useK8sStore()
      const r = await store.removeCluster('c1')

      expect(deleteK8sCluster).toHaveBeenCalledWith('c1')
      expect(r).toEqual({ s: 204, j: null })
    })

    it('API 抛错时向上抛出异常', async () => {
      deleteK8sCluster.mockRejectedValueOnce(new Error('delete cluster failed'))

      const store = useK8sStore()
      await expect(store.removeCluster('c1')).rejects.toThrow('delete cluster failed')
    })
  })

  describe('currentCluster getter', () => {
    it('按 currentClusterID 查找匹配的集群', () => {
      const store = useK8sStore()
      store.clusters = [
        { id: 'c1', name: 'prod' },
        { id: 'c2', name: 'staging' }
      ]
      store.currentClusterID = 'c2'

      expect(store.currentCluster).toEqual({ id: 'c2', name: 'staging' })
    })

    it('未匹配时返回 null', () => {
      const store = useK8sStore()
      store.clusters = [{ id: 'c1', name: 'prod' }]
      store.currentClusterID = 'c999'

      expect(store.currentCluster).toBeNull()
    })

    it('currentClusterID 为空时返回 null', () => {
      const store = useK8sStore()
      store.clusters = [{ id: 'c1', name: 'prod' }]
      store.currentClusterID = ''

      expect(store.currentCluster).toBeNull()
    })
  })
})