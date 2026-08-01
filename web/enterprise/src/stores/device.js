// 设备 store — 网段分组设备列表 + 详情
import { defineStore } from 'pinia'
import { getDevices, getDevice, provisionDevice } from '@/api/device'

export const useDeviceStore = defineStore('device', {
  state: () => ({
    segments: {},          // { segName: Device[] }
    current: null,         // 当前打开的设备详情 { device, tasks, results }
    loading: false,
    error: ''
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
        this.error = e.j?.error || '设备列表拉取失败'
      } finally {
        this.loading = false
      }
    },
    async openDevice(id) {
      try {
        this.current = await getDevice(id)
      } catch (e) {
        this.error = e.j?.error || '设备详情拉取失败'
      }
    },
    async provision(id) {
      const r = await provisionDevice(id)
      await this.fetchDevices()
      return r
    },
    closeDrawer() { this.current = null }
  }
})