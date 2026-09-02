// 控制面联邦 store — Peer 列表 / 跨 Peer 设备聚合 / 任务转发
import { defineStore } from 'pinia'
import { getPeers, forwardTask, getFederationDevices } from '@/api/federation'
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
    // 跨 Peer 聚合设备视图
    async fetchDevices() {
      this.loading = true; this.error = ''
      try {
        const r = await getFederationDevices() || {}
        this.devices = r.devices || []
        this.devicePeers = r.peers || []
      } catch (e) {
        this.error = e.j?.error || t('error.federationDevicesFailed')
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
    }
  }
})