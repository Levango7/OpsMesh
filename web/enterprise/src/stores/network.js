// 网络拓扑诊断 store — 拓扑 / 诊断 / 连通性 / 设备 CRUD / 指标 / 配置下发 / 发现
import { defineStore } from 'pinia'
import {
  getNetworkTopology, getCachedTopology,
  startDiagnose, getDiagnoseResult, checkConnectivity,
  getNetworkDevices, createNetworkDevice, deleteNetworkDevice,
  getDeviceMetrics, pushDeviceConfig, discoverNetwork
} from '@/api/network'
import { t } from '@/i18n'

export const useNetworkStore = defineStore('network', {
  state: () => ({
    topology: null,
    // 最近一次诊断任务返回 {taskID, status}
    diagnoseTask: null,
    // 诊断任务结果 {taskID, status, output, finishedAt}
    diagnoseResult: null,
    // 连通性检测结果
    connectivity: [],
    // 网络设备列表
    devices: [],
    // 当前设备指标
    deviceMetrics: [],
    // 网络发现结果
    discovered: [],
    // 配置下发返回 {taskID}
    lastConfigPush: null,
    loading: false,
    error: ''
  }),
  actions: {
    // ---------- 拓扑 ----------
    async fetchTopology(params) {
      this.loading = true; this.error = ''
      try {
        this.topology = await getNetworkTopology(params)
      } catch (e) {
        this.error = e.j?.error || t('error.networkTopologyFailed')
      } finally {
        this.loading = false
      }
    },
    async fetchCachedTopology() {
      this.loading = true; this.error = ''
      try {
        this.topology = await getCachedTopology()
      } catch (e) {
        this.error = e.j?.error || t('error.networkTopologyFailed')
      } finally {
        this.loading = false
      }
    },

    // ---------- 诊断 ----------
    async startDiagnose(body) {
      this.loading = true; this.error = ''
      try {
        const r = await startDiagnose(body)
        this.diagnoseTask = r.j || null
        this.diagnoseResult = null
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.networkDiagnoseFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    async fetchDiagnoseResult(taskID) {
      this.loading = true; this.error = ''
      try {
        this.diagnoseResult = await getDiagnoseResult(taskID)
      } catch (e) {
        this.error = e.j?.error || t('error.networkDiagnoseFailed')
      } finally {
        this.loading = false
      }
    },
    async checkConnectivity(body) {
      this.loading = true; this.error = ''
      try {
        const r = await checkConnectivity(body) || {}
        this.connectivity = r.results || []
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.networkConnectivityFailed')
        throw e
      } finally {
        this.loading = false
      }
    },

    // ---------- 网络设备 CRUD ----------
    async fetchDevices() {
      this.loading = true; this.error = ''
      try {
        const r = await getNetworkDevices()
        this.devices = (r && r.devices) || r || []
      } catch (e) {
        this.error = e.j?.error || t('error.networkDevicesFailed')
      } finally {
        this.loading = false
      }
    },
    async createDevice(body) {
      const r = await createNetworkDevice(body)
      await this.fetchDevices()
      return r
    },
    async removeDevice(id) {
      const r = await deleteNetworkDevice(id)
      await this.fetchDevices()
      return r
    },
    async fetchDeviceMetrics(id) {
      this.loading = true; this.error = ''
      try {
        const r = await getDeviceMetrics(id) || {}
        this.deviceMetrics = r.metrics || []
      } catch (e) {
        this.error = e.j?.error || t('error.networkDeviceMetricsFailed')
      } finally {
        this.loading = false
      }
    },
    async pushConfig(id, body) {
      this.loading = true; this.error = ''
      try {
        const r = await pushDeviceConfig(id, body)
        this.lastConfigPush = r.j || null
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.networkConfigPushFailed')
        throw e
      } finally {
        this.loading = false
      }
    },

    // ---------- 网络发现 ----------
    async discover(body) {
      this.loading = true; this.error = ''
      try {
        const r = await discoverNetwork(body) || {}
        this.discovered = r.discovered || []
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.networkDiscoverFailed')
        throw e
      } finally {
        this.loading = false
      }
    }
  }
})