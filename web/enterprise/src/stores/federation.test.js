// federation store 单元测试
// 覆盖：初始状态、fetchPeers/fetchDevices/forward 动作、loading/error 状态、
//       成功/失败分支、i18n 默认错误码回退。
// 说明：源码中方法名为 fetchPeers/fetchDevices/forward（任务描述中的
//      forwardTask 为对应 API 名称语义），本测试按实际导出方法验证。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

// mock @/api/federation：避免真实网络请求，用 vi.fn 控制返回值
vi.mock('@/api/federation', () => ({
  getPeers: vi.fn(),
  forwardTask: vi.fn(),
  getFederationDevices: vi.fn(),
}))

import { useFederationStore } from '@/stores/federation'
import { getPeers, forwardTask, getFederationDevices } from '@/api/federation'

describe('useFederationStore', () => {
  beforeEach(() => {
    // 每个测试前创建新 pinia 实例，隔离状态
    setActivePinia(createPinia())
    // 重置 mock 调用记录
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('peers 初始为空数组', () => {
      const store = useFederationStore()
      expect(store.peers).toEqual([])
    })

    it('devices 初始为空数组', () => {
      const store = useFederationStore()
      expect(store.devices).toEqual([])
    })

    it('devicePeers 初始为空数组', () => {
      const store = useFederationStore()
      expect(store.devicePeers).toEqual([])
    })

    it('lastForward 初始为 null', () => {
      const store = useFederationStore()
      expect(store.lastForward).toBeNull()
    })

    it('loading 初始为 false', () => {
      const store = useFederationStore()
      expect(store.loading).toBe(false)
    })

    it('error 初始为空字符串', () => {
      const store = useFederationStore()
      expect(store.error).toBe('')
    })
  })

  describe('fetchPeers 动作', () => {
    it('成功时从 r.peers 设置 peers', async () => {
      const mockResp = { peers: [{ id: 'p1', name: 'peer-1' }] }
      getPeers.mockResolvedValueOnce(mockResp)

      const store = useFederationStore()
      await store.fetchPeers()

      expect(getPeers).toHaveBeenCalled()
      expect(store.peers).toEqual([{ id: 'p1', name: 'peer-1' }])
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('响应无 peers 字段但本身是数组时直接使用数组', async () => {
      const mockArr = [{ id: 'p1' }, { id: 'p2' }]
      getPeers.mockResolvedValueOnce(mockArr)

      const store = useFederationStore()
      await store.fetchPeers()

      expect(store.peers).toEqual(mockArr)
      expect(store.loading).toBe(false)
    })

    it('API 返回 null 时 peers 为空数组', async () => {
      getPeers.mockResolvedValueOnce(null)

      const store = useFederationStore()
      await store.fetchPeers()

      expect(store.peers).toEqual([])
      expect(store.loading).toBe(false)
    })

    it('失败时优先使用后端返回的 error 文案', async () => {
      getPeers.mockRejectedValueOnce({ j: { error: 'Peer 服务不可达' } })

      const store = useFederationStore()
      await store.fetchPeers()

      expect(store.error).toBe('Peer 服务不可达')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getPeers.mockRejectedValueOnce({})

      const store = useFederationStore()
      await store.fetchPeers()

      // i18n 默认回退到 zh：error.federationPeersFailed → "联邦 Peer 列表拉取失败"
      expect(store.error).toBe('联邦 Peer 列表拉取失败')
      expect(store.loading).toBe(false)
    })
  })

  describe('fetchDevices 动作', () => {
    it('成功时设置 devices 与 devicePeers', async () => {
      const mockResp = {
        devices: [{ id: 'd1', hostname: 'host-1' }],
        peers: [{ id: 'p1', name: 'peer-1' }],
      }
      getFederationDevices.mockResolvedValueOnce(mockResp)

      const store = useFederationStore()
      await store.fetchDevices()

      expect(getFederationDevices).toHaveBeenCalled()
      expect(store.devices).toEqual([{ id: 'd1', hostname: 'host-1' }])
      expect(store.devicePeers).toEqual([{ id: 'p1', name: 'peer-1' }])
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('响应缺少字段时 devices 与 devicePeers 均为空数组', async () => {
      getFederationDevices.mockResolvedValueOnce({})

      const store = useFederationStore()
      await store.fetchDevices()

      expect(store.devices).toEqual([])
      expect(store.devicePeers).toEqual([])
      expect(store.loading).toBe(false)
    })

    it('API 返回 null 时 devices 与 devicePeers 均为空数组', async () => {
      getFederationDevices.mockResolvedValueOnce(null)

      const store = useFederationStore()
      await store.fetchDevices()

      expect(store.devices).toEqual([])
      expect(store.devicePeers).toEqual([])
      expect(store.loading).toBe(false)
    })

    it('失败时优先使用后端返回的 error 文案', async () => {
      getFederationDevices.mockRejectedValueOnce({ j: { error: '聚合视图拉取失败' } })

      const store = useFederationStore()
      await store.fetchDevices()

      expect(store.error).toBe('聚合视图拉取失败')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getFederationDevices.mockRejectedValueOnce({})

      const store = useFederationStore()
      await store.fetchDevices()

      expect(store.error).toBe('联邦设备聚合拉取失败')
      expect(store.loading).toBe(false)
    })
  })

  describe('forward 动作', () => {
    it('成功时设置 lastForward 并返回结果', async () => {
      const mockResp = { s: 200, j: { taskId: 't1', status: 'forwarded' } }
      forwardTask.mockResolvedValueOnce(mockResp)

      const store = useFederationStore()
      const r = await store.forward({ peer: 'p1', task: 'deploy' })

      expect(forwardTask).toHaveBeenCalledWith({ peer: 'p1', task: 'deploy' })
      expect(store.lastForward).toEqual({ taskId: 't1', status: 'forwarded' })
      expect(r).toEqual(mockResp)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('响应体 j 为空时 lastForward 为 null', async () => {
      forwardTask.mockResolvedValueOnce({ s: 200, j: null })

      const store = useFederationStore()
      await store.forward({})

      expect(store.lastForward).toBeNull()
      expect(store.loading).toBe(false)
    })

    it('失败时设置 error 并向上抛出异常', async () => {
      const err = { j: { error: 'Peer 拒绝转发' } }
      forwardTask.mockRejectedValueOnce(err)

      const store = useFederationStore()
      await expect(store.forward({})).rejects.toEqual(err)

      expect(store.error).toBe('Peer 拒绝转发')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码并抛出', async () => {
      forwardTask.mockRejectedValueOnce(new Error('network'))

      const store = useFederationStore()
      await expect(store.forward({})).rejects.toThrow('network')

      expect(store.error).toBe('任务转发失败')
      expect(store.loading).toBe(false)
    })
  })
})