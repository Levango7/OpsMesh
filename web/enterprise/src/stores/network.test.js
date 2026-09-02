// network store 单元测试
// 覆盖：初始状态、fetchTopology/fetchCachedTopology/startDiagnose/fetchDiagnoseResult/
//      checkConnectivity/fetchDevices/createDevice/removeDevice/fetchDeviceMetrics/
//      pushConfig/discover 动作、loading/error 状态。
// 说明：源码中方法名为 startDiagnose/checkConnectivity/removeDevice（任务描述中的
//      diagnose/connectivity/deleteDevice 为别名语义），本测试按实际导出方法验证。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

// mock @/api/network：避免真实网络请求，用 vi.fn 控制返回值
vi.mock('@/api/network', () => ({
  getNetworkTopology: vi.fn(),
  getCachedTopology: vi.fn(),
  startDiagnose: vi.fn(),
  getDiagnoseResult: vi.fn(),
  checkConnectivity: vi.fn(),
  getNetworkDevices: vi.fn(),
  createNetworkDevice: vi.fn(),
  deleteNetworkDevice: vi.fn(),
  getDeviceMetrics: vi.fn(),
  pushDeviceConfig: vi.fn(),
  discoverNetwork: vi.fn(),
}))

import { useNetworkStore } from '@/stores/network'
import {
  getNetworkTopology, startDiagnose, checkConnectivity,
  getNetworkDevices, createNetworkDevice, deleteNetworkDevice, discoverNetwork
} from '@/api/network'

describe('useNetworkStore', () => {
  beforeEach(() => {
    // 每个测试前创建新 pinia 实例，隔离状态
    setActivePinia(createPinia())
    // 重置 mock 调用记录
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('topology 初始为 null', () => {
      const store = useNetworkStore()
      expect(store.topology).toBeNull()
    })

    it('diagnoseTask 初始为 null', () => {
      const store = useNetworkStore()
      expect(store.diagnoseTask).toBeNull()
    })

    it('diagnoseResult 初始为 null', () => {
      const store = useNetworkStore()
      expect(store.diagnoseResult).toBeNull()
    })

    it('connectivity 初始为空数组', () => {
      const store = useNetworkStore()
      expect(store.connectivity).toEqual([])
    })

    it('devices 初始为空数组', () => {
      const store = useNetworkStore()
      expect(store.devices).toEqual([])
    })

    it('deviceMetrics 初始为空数组', () => {
      const store = useNetworkStore()
      expect(store.deviceMetrics).toEqual([])
    })

    it('discovered 初始为空数组', () => {
      const store = useNetworkStore()
      expect(store.discovered).toEqual([])
    })

    it('lastConfigPush 初始为 null', () => {
      const store = useNetworkStore()
      expect(store.lastConfigPush).toBeNull()
    })

    it('loading 初始为 false', () => {
      const store = useNetworkStore()
      expect(store.loading).toBe(false)
    })

    it('error 初始为空字符串', () => {
      const store = useNetworkStore()
      expect(store.error).toBe('')
    })
  })

  describe('fetchTopology 动作', () => {
    it('成功时设置 topology', async () => {
      const mockTopo = { nodes: [{ id: 'n1' }], edges: [] }
      getNetworkTopology.mockResolvedValueOnce(mockTopo)

      const store = useNetworkStore()
      const params = { refresh: true }
      await store.fetchTopology(params)

      expect(getNetworkTopology).toHaveBeenCalledWith(params)
      expect(store.topology).toEqual(mockTopo)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('失败时设置 error 并结束 loading', async () => {
      const err = { j: { error: '拓扑服务不可用' } }
      getNetworkTopology.mockRejectedValueOnce(err)

      const store = useNetworkStore()
      await store.fetchTopology()

      expect(store.error).toBe('拓扑服务不可用')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getNetworkTopology.mockRejectedValueOnce({})

      const store = useNetworkStore()
      await store.fetchTopology()

      expect(store.error).toBe('网络拓扑拉取失败')
    })
  })

  describe('startDiagnose 动作（任务描述中的 diagnose）', () => {
    it('成功时设置 diagnoseTask、清空 diagnoseResult 并返回响应', async () => {
      const mockResp = { s: 200, j: { taskID: 't1', status: 'running' } }
      startDiagnose.mockResolvedValueOnce(mockResp)

      const store = useNetworkStore()
      const body = { agentID: 'a1', command: 'ping', target: '10.0.0.1' }
      const r = await store.startDiagnose(body)

      expect(startDiagnose).toHaveBeenCalledWith(body)
      expect(store.diagnoseTask).toEqual({ taskID: 't1', status: 'running' })
      expect(store.diagnoseResult).toBeNull()
      expect(store.loading).toBe(false)
      expect(r).toEqual(mockResp)
    })

    it('响应无 j 字段时 diagnoseTask 为 null', async () => {
      startDiagnose.mockResolvedValueOnce({ s: 200 })

      const store = useNetworkStore()
      await store.startDiagnose({})

      expect(store.diagnoseTask).toBeNull()
    })

    it('失败时设置 error、结束 loading 并向上抛出', async () => {
      const err = { j: { error: 'agent 离线' } }
      startDiagnose.mockRejectedValueOnce(err)

      const store = useNetworkStore()
      await expect(store.startDiagnose({})).rejects.toThrow()

      expect(store.error).toBe('agent 离线')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      startDiagnose.mockRejectedValueOnce({})

      const store = useNetworkStore()
      await expect(store.startDiagnose({})).rejects.toThrow()

      expect(store.error).toBe('网络诊断失败')
    })
  })

  describe('checkConnectivity 动作（任务描述中的 connectivity）', () => {
    it('成功时设置 connectivity 并返回响应', async () => {
      const mockResp = { results: [{ source: 'a', target: 'b', reachable: true, latencyMs: 1 }] }
      checkConnectivity.mockResolvedValueOnce(mockResp)

      const store = useNetworkStore()
      const body = { targets: [{ source: 'a', target: 'b' }] }
      const r = await store.checkConnectivity(body)

      expect(checkConnectivity).toHaveBeenCalledWith(body)
      expect(store.connectivity).toEqual(mockResp.results)
      expect(store.loading).toBe(false)
      expect(r).toEqual(mockResp)
    })

    it('响应无 results 字段时 connectivity 为空数组', async () => {
      checkConnectivity.mockResolvedValueOnce({})

      const store = useNetworkStore()
      await store.checkConnectivity({})

      expect(store.connectivity).toEqual([])
    })

    it('API 返回 null 时 connectivity 为空数组', async () => {
      checkConnectivity.mockResolvedValueOnce(null)

      const store = useNetworkStore()
      await store.checkConnectivity({})

      expect(store.connectivity).toEqual([])
    })

    it('失败时设置 error、结束 loading 并向上抛出', async () => {
      const err = { j: { error: '超时' } }
      checkConnectivity.mockRejectedValueOnce(err)

      const store = useNetworkStore()
      await expect(store.checkConnectivity({})).rejects.toThrow()

      expect(store.error).toBe('超时')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      checkConnectivity.mockRejectedValueOnce({})

      const store = useNetworkStore()
      await expect(store.checkConnectivity({})).rejects.toThrow()

      expect(store.error).toBe('连通性检测失败')
    })
  })

  describe('fetchDevices 动作', () => {
    it('成功时设置 devices（r.devices 形态）', async () => {
      const mockDevices = [{ id: 'd1', name: 'core-sw' }]
      getNetworkDevices.mockResolvedValueOnce({ devices: mockDevices })

      const store = useNetworkStore()
      await store.fetchDevices()

      expect(getNetworkDevices).toHaveBeenCalled()
      expect(store.devices).toEqual(mockDevices)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('成功时设置 devices（裸数组形态）', async () => {
      const mockDevices = [{ id: 'd1' }]
      getNetworkDevices.mockResolvedValueOnce(mockDevices)

      const store = useNetworkStore()
      await store.fetchDevices()

      expect(store.devices).toEqual(mockDevices)
    })

    it('API 返回 null 时 devices 为空数组', async () => {
      getNetworkDevices.mockResolvedValueOnce(null)

      const store = useNetworkStore()
      await store.fetchDevices()

      expect(store.devices).toEqual([])
    })

    it('失败时设置 error 并结束 loading', async () => {
      const err = { j: { error: '网络异常' } }
      getNetworkDevices.mockRejectedValueOnce(err)

      const store = useNetworkStore()
      await store.fetchDevices()

      expect(store.error).toBe('网络异常')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getNetworkDevices.mockRejectedValueOnce({})

      const store = useNetworkStore()
      await store.fetchDevices()

      expect(store.error).toBe('网络设备列表拉取失败')
    })
  })

  describe('createDevice 动作', () => {
    it('成功时调用 API 并刷新列表', async () => {
      const mockResp = { s: 201, j: { id: 'd1', name: 'new-sw' } }
      createNetworkDevice.mockResolvedValueOnce(mockResp)
      getNetworkDevices.mockResolvedValueOnce({ devices: [{ id: 'd1' }] })

      const store = useNetworkStore()
      const body = { name: 'new-sw', type: 'switch', ip: '10.0.0.2' }
      const r = await store.createDevice(body)

      expect(createNetworkDevice).toHaveBeenCalledWith(body)
      expect(getNetworkDevices).toHaveBeenCalled()
      expect(store.devices).toEqual([{ id: 'd1' }])
      expect(r).toEqual(mockResp)
    })

    it('API 抛错时向上抛出异常', async () => {
      createNetworkDevice.mockRejectedValueOnce(new Error('create failed'))

      const store = useNetworkStore()
      await expect(store.createDevice({})).rejects.toThrow('create failed')
    })
  })

  describe('removeDevice 动作（任务描述中的 deleteDevice）', () => {
    it('成功时调用 API 并刷新列表', async () => {
      const mockResp = { s: 200, j: { status: 'deleted' } }
      deleteNetworkDevice.mockResolvedValueOnce(mockResp)
      getNetworkDevices.mockResolvedValueOnce({ devices: [] })

      const store = useNetworkStore()
      const r = await store.removeDevice('d1')

      expect(deleteNetworkDevice).toHaveBeenCalledWith('d1')
      expect(getNetworkDevices).toHaveBeenCalled()
      expect(store.devices).toEqual([])
      expect(r).toEqual(mockResp)
    })

    it('API 抛错时向上抛出异常', async () => {
      deleteNetworkDevice.mockRejectedValueOnce(new Error('delete failed'))

      const store = useNetworkStore()
      await expect(store.removeDevice('d1')).rejects.toThrow('delete failed')
    })
  })

  describe('discover 动作', () => {
    it('成功时设置 discovered 并返回响应', async () => {
      const mockResp = { discovered: [{ id: 'd1', ip: '10.0.0.3' }] }
      discoverNetwork.mockResolvedValueOnce(mockResp)

      const store = useNetworkStore()
      const body = { segment: '10.0.0.0/24', agentID: 'a1' }
      const r = await store.discover(body)

      expect(discoverNetwork).toHaveBeenCalledWith(body)
      expect(store.discovered).toEqual(mockResp.discovered)
      expect(store.loading).toBe(false)
      expect(r).toEqual(mockResp)
    })

    it('响应无 discovered 字段时 discovered 为空数组', async () => {
      discoverNetwork.mockResolvedValueOnce({})

      const store = useNetworkStore()
      await store.discover({})

      expect(store.discovered).toEqual([])
    })

    it('API 返回 null 时 discovered 为空数组', async () => {
      discoverNetwork.mockResolvedValueOnce(null)

      const store = useNetworkStore()
      await store.discover({})

      expect(store.discovered).toEqual([])
    })

    it('失败时设置 error、结束 loading 并向上抛出', async () => {
      const err = { j: { error: 'agent 不可达' } }
      discoverNetwork.mockRejectedValueOnce(err)

      const store = useNetworkStore()
      await expect(store.discover({})).rejects.toThrow()

      expect(store.error).toBe('agent 不可达')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      discoverNetwork.mockRejectedValueOnce({})

      const store = useNetworkStore()
      await expect(store.discover({})).rejects.toThrow()

      expect(store.error).toBe('网络发现失败')
    })
  })
})