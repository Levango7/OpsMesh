// GPU 资源管理 store — 节点、工作负载、模型、配额、指标
import { defineStore } from 'pinia'
import {
  getGpuNodes,
  getGpuWorkloads,
  createGpuWorkload,
  deleteGpuWorkload,
  getGpuModels,
  pullGpuModel,
  deleteGpuModel,
  getGpuQuotas,
  getGpuMetrics
} from '@/api/gpu'
import { t } from '@/i18n'

export const useGpuStore = defineStore('gpu', {
  state: () => ({
    nodes: [],
    workloads: [],
    models: [],
    quotas: [],
    metrics: [],
    selectedNodeId: '',
    loading: false,
    workloadsLoading: false,
    modelsLoading: false,
    metricsLoading: false,
    error: ''
  }),
  getters: {
    selectedNode: (s) => s.nodes.find((n) => n.id === s.selectedNodeId) || null,
    totalGpu: (s) => s.nodes.length,
    healthyNodes: (s) => s.nodes.filter((n) => n.health === 'healthy').length,
    activeWorkloads: (s) => s.workloads.filter((w) => w.status === 'running').length
  },
  actions: {
    async fetchNodes() {
      this.loading = true; this.error = ''
      try {
        const data = await getGpuNodes()
        this.nodes = (data && data.nodes) || data || []
      } catch (e) {
        this.error = e.j?.error || t('error.gpuNodesFailed')
      } finally {
        this.loading = false
      }
    },
    async fetchWorkloads() {
      this.workloadsLoading = true; this.error = ''
      try {
        const data = await getGpuWorkloads()
        this.workloads = (data && data.workloads) || data || []
      } catch (e) {
        this.error = e.j?.error || t('error.gpuWorkloadsFailed')
      } finally {
        this.workloadsLoading = false
      }
    },
    async addWorkload(name, type, model, gpuCount, nodeId) {
      return await createGpuWorkload(name, type, model, gpuCount, nodeId)
    },
    async removeWorkload(id) {
      return await deleteGpuWorkload(id)
    },
    async fetchModels() {
      this.modelsLoading = true; this.error = ''
      try {
        const data = await getGpuModels()
        this.models = (data && data.models) || data || []
      } catch (e) {
        this.error = e.j?.error || t('error.gpuModelsFailed')
      } finally {
        this.modelsLoading = false
      }
    },
    async pullModel(name, nodeId) {
      return await pullGpuModel(name, nodeId)
    },
    async removeModel(name) {
      return await deleteGpuModel(name)
    },
    async fetchQuotas() {
      try {
        const data = await getGpuQuotas()
        this.quotas = (data && data.quotas) || data || []
      } catch (e) {
        this.quotas = []
      }
    },
    async fetchMetrics(nodeId, range) {
      this.metricsLoading = true; this.error = ''
      try {
        const data = await getGpuMetrics(nodeId, range)
        this.metrics = (data && data.metrics) || data || []
      } catch (e) {
        this.error = e.j?.error || t('error.gpuMetricsFailed')
      } finally {
        this.metricsLoading = false
      }
    },
    selectNode(id) {
      this.selectedNodeId = id || ''
      if (id) {
        this.fetchMetrics(id, '1h')
      }
    }
  }
})
