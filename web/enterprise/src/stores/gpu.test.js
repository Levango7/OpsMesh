// gpu store 单元测试
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

vi.mock('@/api/gpu', () => ({
  getGpuNodes: vi.fn(),
  getGpuWorkloads: vi.fn(),
  createGpuWorkload: vi.fn(),
  deleteGpuWorkload: vi.fn(),
  getGpuModels: vi.fn(),
  pullGpuModel: vi.fn(),
  deleteGpuModel: vi.fn(),
  getGpuQuotas: vi.fn(),
  getGpuMetrics: vi.fn()
}))

import { useGpuStore } from '@/stores/gpu'
import {
  getGpuNodes,
  getGpuWorkloads,
  createGpuWorkload,
  deleteGpuWorkload,
  getGpuModels,
  getGpuQuotas,
  getGpuMetrics
} from '@/api/gpu'

describe('useGpuStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('nodes 初始为空数组', () => {
      const store = useGpuStore()
      expect(store.nodes).toEqual([])
    })

    it('workloads 初始为空数组', () => {
      const store = useGpuStore()
      expect(store.workloads).toEqual([])
    })

    it('models 初始为空数组', () => {
      const store = useGpuStore()
      expect(store.models).toEqual([])
    })

    it('selectedNodeId 初始为空字符串', () => {
      const store = useGpuStore()
      expect(store.selectedNodeId).toBe('')
    })

    it('loading 初始为 false', () => {
      const store = useGpuStore()
      expect(store.loading).toBe(false)
    })
  })

  describe('fetchNodes 动作', () => {
    it('成功时设置 nodes（{nodes: []} 形态）', async () => {
      const mockNodes = [{ id: 'n1', name: 'gpu-node-1', health: 'healthy' }]
      getGpuNodes.mockResolvedValueOnce({ nodes: mockNodes })

      const store = useGpuStore()
      await store.fetchNodes()

      expect(getGpuNodes).toHaveBeenCalled()
      expect(store.nodes).toEqual(mockNodes)
      expect(store.loading).toBe(false)
    })

    it('成功时设置 nodes（数组形态）', async () => {
      const mockNodes = [{ id: 'n1', name: 'gpu-node-1' }]
      getGpuNodes.mockResolvedValueOnce(mockNodes)

      const store = useGpuStore()
      await store.fetchNodes()

      expect(store.nodes).toEqual(mockNodes)
    })

    it('失败时设置 error', async () => {
      getGpuNodes.mockRejectedValueOnce({ j: { error: 'GPU 服务不可用' } })

      const store = useGpuStore()
      await store.fetchNodes()

      expect(store.error).toBe('GPU 服务不可用')
      expect(store.loading).toBe(false)
    })
  })

  describe('fetchWorkloads 动作', () => {
    it('成功时设置 workloads', async () => {
      const mockWorkloads = [{ id: 'w1', name: 'llama-inference', status: 'running' }]
      getGpuWorkloads.mockResolvedValueOnce({ workloads: mockWorkloads })

      const store = useGpuStore()
      await store.fetchWorkloads()

      expect(store.workloads).toEqual(mockWorkloads)
      expect(store.workloadsLoading).toBe(false)
    })

    it('失败时设置 error', async () => {
      getGpuWorkloads.mockRejectedValueOnce({ j: { error: '获取失败' } })

      const store = useGpuStore()
      await store.fetchWorkloads()

      expect(store.error).toBe('获取失败')
    })
  })

  describe('addWorkload 动作', () => {
    it('成功时返回结果', async () => {
      createGpuWorkload.mockResolvedValueOnce({ s: 200, j: { id: 'w1' } })

      const store = useGpuStore()
      const r = await store.addWorkload('test', 'inference', 'llama3', 2, 'n1')

      expect(createGpuWorkload).toHaveBeenCalledWith('test', 'inference', 'llama3', 2, 'n1')
      expect(r).toEqual({ s: 200, j: { id: 'w1' } })
    })
  })

  describe('removeWorkload 动作', () => {
    it('成功时返回结果', async () => {
      deleteGpuWorkload.mockResolvedValueOnce({ s: 204, j: null })

      const store = useGpuStore()
      const r = await store.removeWorkload('w1')

      expect(deleteGpuWorkload).toHaveBeenCalledWith('w1')
      expect(r).toEqual({ s: 204, j: null })
    })
  })

  describe('fetchModels 动作', () => {
    it('成功时设置 models', async () => {
      const mockModels = [{ name: 'llama3.1:8b', size: '4.7GB', status: 'ready' }]
      getGpuModels.mockResolvedValueOnce({ models: mockModels })

      const store = useGpuStore()
      await store.fetchModels()

      expect(store.models).toEqual(mockModels)
    })
  })

  describe('fetchQuotas 动作', () => {
    it('成功时设置 quotas', async () => {
      const mockQuotas = [{ tenantId: 't1', totalGpu: 8, usedGpu: 4 }]
      getGpuQuotas.mockResolvedValueOnce({ quotas: mockQuotas })

      const store = useGpuStore()
      await store.fetchQuotas()

      expect(store.quotas).toEqual(mockQuotas)
    })
  })

  describe('fetchMetrics 动作', () => {
    it('成功时设置 metrics', async () => {
      const mockMetrics = [{ timestamp: '2026-01-01T00:00:00Z', utilization: 75, memory: 65 }]
      getGpuMetrics.mockResolvedValueOnce({ metrics: mockMetrics })

      const store = useGpuStore()
      await store.fetchMetrics('n1', '1h')

      expect(getGpuMetrics).toHaveBeenCalledWith('n1', '1h')
      expect(store.metrics).toEqual(mockMetrics)
    })
  })

  describe('selectNode 动作', () => {
    it('设置 selectedNodeId 并拉取指标', async () => {
      const mockMetrics = [{ utilization: 50 }]
      getGpuMetrics.mockResolvedValueOnce({ metrics: mockMetrics })

      const store = useGpuStore()
      store.selectNode('n1')

      expect(store.selectedNodeId).toBe('n1')
    })

    it('空 id 不拉取指标', () => {
      const store = useGpuStore()
      store.selectNode('')

      expect(store.selectedNodeId).toBe('')
    })
  })

  describe('getters', () => {
    it('selectedNode 返回匹配节点', () => {
      const store = useGpuStore()
      store.nodes = [{ id: 'n1', name: 'node1' }, { id: 'n2', name: 'node2' }]
      store.selectedNodeId = 'n2'

      expect(store.selectedNode).toEqual({ id: 'n2', name: 'node2' })
    })

    it('totalGpu 返回节点数', () => {
      const store = useGpuStore()
      store.nodes = [{ id: 'n1' }, { id: 'n2' }]

      expect(store.totalGpu).toBe(2)
    })

    it('healthyNodes 返回健康节点数', () => {
      const store = useGpuStore()
      store.nodes = [{ id: 'n1', health: 'healthy' }, { id: 'n2', health: 'unhealthy' }]

      expect(store.healthyNodes).toBe(1)
    })

    it('activeWorkloads 返回运行中工作负载数', () => {
      const store = useGpuStore()
      store.workloads = [{ id: 'w1', status: 'running' }, { id: 'w2', status: 'failed' }]

      expect(store.activeWorkloads).toBe(1)
    })
  })
})
