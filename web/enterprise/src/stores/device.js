// 设备 store — 网段分组设备列表 + 详情 + 监控指标
import { defineStore } from 'pinia'
import { getDevices, getDevice, provisionDevice, getMetrics } from '@/api/device'
import { t } from '@/i18n'

export const useDeviceStore = defineStore('device', {
  state: () => ({
    segments: {},          // { segName: Device[] }
    current: null,         // 当前打开的设备详情 { device, tasks, results }
    metrics: null,         // 当前设备的监控指标聚合
    loading: false,
    metricsLoading: false,
    error: '',
    metricsError: ''
  }),
  getters: {
    total: (s) => Object.values(s.segments).reduce((n, arr) => n + (arr ? arr.length : 0), 0),
    managed: (s) => Object.values(s.segments).reduce(
      (n, arr) => n + (arr || []).filter((d) => d.state === 'managed' || d.agentID).length, 0
    ),
    flat: (s) => Object.entries(s.segments).flatMap(([seg, arr]) => (arr || []).map((d) => ({ ...d, segment: seg })))
  },
  actions: {
    async fetchDevices() {
      this.loading = true; this.error = ''
      try {
        this.segments = await getDevices() || {}
      } catch (e) {
        this.error = e.j?.error || t('error.deviceListFailed')
      } finally {
        this.loading = false
      }
    },
    async openDevice(id) {
      try {
        this.current = await getDevice(id)
      } catch (e) {
        this.error = e.j?.error || t('error.deviceDetailFailed')
      }
    },
    async provision(id) {
      const r = await provisionDevice(id)
      await this.fetchDevices()
      return r
    },
    // 拉取设备监控指标聚合
    async fetchMetrics(id) {
      this.metricsLoading = true; this.metricsError = ''
      try {
        this.metrics = await getMetrics(id)
      } catch (e) {
        this.metricsError = e.j?.error || t('error.deviceMetricsFailed')
        this.metrics = null
      } finally {
        this.metricsLoading = false
      }
    },
    // 清空监控指标（离开详情页时调用）
    clearMetrics() {
      this.metrics = null
      this.metricsError = ''
    },
    closeDrawer() { this.current = null }
  }
})
