// 控制面联邦 store — Peer 列表 / 跨 Peer 设备聚合 / 任务转发 / 任务状态查询
import { defineStore } from 'pinia'
import { getPeers, forwardTask, getFederationDevices, getFederationTaskStatus } from '@/api/federation'
import { t } from '@/i18n'

export const useFederationStore = defineStore('federation', {
  state: () => ({
    peers: [],
    devices: [],
    devicePeers: [],
    // 最近一次任务转发返回
    lastForward: null,
    loading: false,
    error: ''
  }),
  actions: {
    // 获取 Peer 列表
    async fetchPeers() {
      this.loading = true; this.error = ''
      try {
        const r = await getPeers()
        this.peers = (r && r.peers) || r || []
      } catch (e) {
        this.error = e.j?.error || t('error.federationPeersFailed')
      } finally {
        this.loading = false
      }
    },
    // 跨 Peer 聚合设备视图（支持分页+搜索）
    async fetchDevices({ page = 1, pageSize = 50, search = '' } = {}) {
      this.loading = true; this.error = ''
      try {
        const r = await getFederationDevices({ page, pageSize, search }) || {}
        this.devices = r.data || []
        this.devicePeers = r.peers || []
        return { total: r.total || 0, page: r.page || 1, pageSize: r.pageSize || 50, hasMore: r.hasMore || false }
      } catch (e) {
        this.error = e.j?.error || t('error.federationDevicesFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    // 转发任务到指定 Peer
    async forward(body) {
      this.loading = true; this.error = ''
      try {
        const r = await forwardTask(body)
        this.lastForward = r.j || null
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.federationForwardFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    // 查询联邦任务在指定 Peer 的执行状态
    async taskStatus(taskID, peerURL) {
      this.loading = true; this.error = ''
      try {
        const r = await getFederationTaskStatus(taskID, peerURL)
        return r.j || r
      } catch (e) {
        this.error = e.j?.error || t('error.federationTaskStatusFailed')
        throw e
      } finally {
        this.loading = false
      }
    }
  }
})
