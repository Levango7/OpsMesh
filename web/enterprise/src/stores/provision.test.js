// provision store 单元测试
// 覆盖：初始状态、autoProvision 动作、loading/error 状态。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

// mock @/api/provision：避免真实网络请求，用 vi.fn 控制返回值
vi.mock('@/api/provision', () => ({
  autoProvision: vi.fn(),
}))

import { useProvisionStore } from '@/stores/provision'
import { autoProvision } from '@/api/provision'

describe('useProvisionStore', () => {
  beforeEach(() => {
    // 每个测试前创建新 pinia 实例，隔离状态
    setActivePinia(createPinia())
    // 重置 mock 调用记录
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('result 初始为 null', () => {
      const store = useProvisionStore()
      expect(store.result).toBeNull()
    })

    it('loading 初始为 false', () => {
      const store = useProvisionStore()
      expect(store.loading).toBe(false)
    })

    it('error 初始为空字符串', () => {
      const store = useProvisionStore()
      expect(store.error).toBe('')
    })
  })

  describe('autoProvision 动作', () => {
    it('成功时设置 result 并返回响应', async () => {
      const mockResp = {
        s: 201,
        j: { discovered: 3, provisioned: 2, failed: 1, devices: [] }
      }
      autoProvision.mockResolvedValueOnce(mockResp)

      const store = useProvisionStore()
      const body = { segment: '10.0.0.0/24', agentVersion: 'v1.2.0' }
      const r = await store.autoProvision(body)

      expect(autoProvision).toHaveBeenCalledWith(body)
      expect(store.result).toEqual({ discovered: 3, provisioned: 2, failed: 1, devices: [] })
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
      expect(r).toEqual(mockResp)
    })

    it('响应无 j 字段时 result 为 null', async () => {
      autoProvision.mockResolvedValueOnce({ s: 201 })

      const store = useProvisionStore()
      await store.autoProvision({})

      expect(store.result).toBeNull()
    })

    it('失败时设置 error、结束 loading 并向上抛出', async () => {
      const err = { j: { error: '网段无可用 agent' } }
      autoProvision.mockRejectedValueOnce(err)

      const store = useProvisionStore()
      await expect(store.autoProvision({})).rejects.toThrow()

      expect(store.error).toBe('网段无可用 agent')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      autoProvision.mockRejectedValueOnce({})

      const store = useProvisionStore()
      await expect(store.autoProvision({})).rejects.toThrow()

      expect(store.error).toBe('自动纳管失败')
    })

    it('API 抛出原生 Error 时向上抛出异常', async () => {
      autoProvision.mockRejectedValueOnce(new Error('network down'))

      const store = useProvisionStore()
      await expect(store.autoProvision({})).rejects.toThrow('network down')

      expect(store.loading).toBe(false)
    })
  })
})