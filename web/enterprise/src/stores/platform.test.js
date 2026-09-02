// platform store 单元测试
// 覆盖：初始状态、fetchConfig/updateConfig/fetchHealth/fetchMetrics 动作、
//       loading/error 状态、成功/失败分支、i18n 默认错误码回退。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

// mock @/api/platform：避免真实网络请求，用 vi.fn 控制返回值
vi.mock('@/api/platform', () => ({
  getPlatformConfig: vi.fn(),
  updatePlatformConfig: vi.fn(),
  getPlatformHealth: vi.fn(),
  getPlatformMetrics: vi.fn(),
}))

import { usePlatformStore } from '@/stores/platform'
import {
  getPlatformConfig,
  updatePlatformConfig,
  getPlatformHealth,
  getPlatformMetrics,
} from '@/api/platform'

describe('usePlatformStore', () => {
  beforeEach(() => {
    // 每个测试前创建新 pinia 实例，隔离状态
    setActivePinia(createPinia())
    // 重置 mock 调用记录
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('config 初始为 null', () => {
      const store = usePlatformStore()
      expect(store.config).toBeNull()
    })

    it('health 初始为 null', () => {
      const store = usePlatformStore()
      expect(store.health).toBeNull()
    })

    it('metrics 初始为 null', () => {
      const store = usePlatformStore()
      expect(store.metrics).toBeNull()
    })

    it('loading 初始为 false', () => {
      const store = usePlatformStore()
      expect(store.loading).toBe(false)
    })

    it('error 初始为空字符串', () => {
      const store = usePlatformStore()
      expect(store.error).toBe('')
    })
  })

  describe('fetchConfig 动作', () => {
    it('成功时设置 config 并结束 loading', async () => {
      const mockConfig = { platform: 'opsmesh', version: '1.0.0' }
      getPlatformConfig.mockResolvedValueOnce(mockConfig)

      const store = usePlatformStore()
      await store.fetchConfig()

      expect(getPlatformConfig).toHaveBeenCalled()
      expect(store.config).toEqual(mockConfig)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('API 返回 null 时 config 为 null', async () => {
      getPlatformConfig.mockResolvedValueOnce(null)

      const store = usePlatformStore()
      await store.fetchConfig()

      expect(store.config).toBeNull()
      expect(store.loading).toBe(false)
    })

    it('失败时优先使用后端返回的 error 文案', async () => {
      getPlatformConfig.mockRejectedValueOnce({ j: { error: '配置服务不可用' } })

      const store = usePlatformStore()
      await store.fetchConfig()

      expect(store.error).toBe('配置服务不可用')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getPlatformConfig.mockRejectedValueOnce({})

      const store = usePlatformStore()
      await store.fetchConfig()

      // i18n 默认回退到 zh：error.platformConfigFailed → "平台配置拉取失败"
      expect(store.error).toBe('平台配置拉取失败')
      expect(store.loading).toBe(false)
    })
  })

  describe('updateConfig 动作', () => {
    it('成功时用响应体更新 config 并返回结果', async () => {
      const mockResp = { s: 200, j: { platform: 'opsmesh', version: '1.1.0' } }
      updatePlatformConfig.mockResolvedValueOnce(mockResp)

      const store = usePlatformStore()
      const r = await store.updateConfig({ version: '1.1.0' })

      expect(updatePlatformConfig).toHaveBeenCalledWith({ version: '1.1.0' })
      expect(store.config).toEqual({ platform: 'opsmesh', version: '1.1.0' })
      expect(r).toEqual(mockResp)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('响应体 j 为空时保留原 config', async () => {
      const store = usePlatformStore()
      store.config = { platform: 'opsmesh' }

      updatePlatformConfig.mockResolvedValueOnce({ s: 200, j: null })

      await store.updateConfig({ foo: 'bar' })

      expect(store.config).toEqual({ platform: 'opsmesh' })
      expect(store.loading).toBe(false)
    })

    it('失败时设置 error 并向上抛出异常', async () => {
      const err = { j: { error: '配置更新冲突' } }
      updatePlatformConfig.mockRejectedValueOnce(err)

      const store = usePlatformStore()
      await expect(store.updateConfig({ version: '1.1.0' })).rejects.toEqual(err)

      expect(store.error).toBe('配置更新冲突')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码并抛出', async () => {
      updatePlatformConfig.mockRejectedValueOnce(new Error('network'))

      const store = usePlatformStore()
      await expect(store.updateConfig({})).rejects.toThrow('network')

      expect(store.error).toBe('平台配置更新失败')
      expect(store.loading).toBe(false)
    })
  })

  describe('fetchHealth 动作', () => {
    it('成功时设置 health 并结束 loading', async () => {
      const mockHealth = { status: 'ok', components: [] }
      getPlatformHealth.mockResolvedValueOnce(mockHealth)

      const store = usePlatformStore()
      await store.fetchHealth()

      expect(getPlatformHealth).toHaveBeenCalled()
      expect(store.health).toEqual(mockHealth)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('API 返回 null 时 health 为 null', async () => {
      getPlatformHealth.mockResolvedValueOnce(null)

      const store = usePlatformStore()
      await store.fetchHealth()

      expect(store.health).toBeNull()
      expect(store.loading).toBe(false)
    })

    it('失败时优先使用后端返回的 error 文案', async () => {
      getPlatformHealth.mockRejectedValueOnce({ j: { error: '健康检查超时' } })

      const store = usePlatformStore()
      await store.fetchHealth()

      expect(store.error).toBe('健康检查超时')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getPlatformHealth.mockRejectedValueOnce({})

      const store = usePlatformStore()
      await store.fetchHealth()

      expect(store.error).toBe('平台健康检查失败')
      expect(store.loading).toBe(false)
    })
  })

  describe('fetchMetrics 动作', () => {
    it('成功时设置 metrics 并结束 loading', async () => {
      const mockMetrics = { cpu: 0.42, mem: 0.55, qps: 1200 }
      getPlatformMetrics.mockResolvedValueOnce(mockMetrics)

      const store = usePlatformStore()
      await store.fetchMetrics()

      expect(getPlatformMetrics).toHaveBeenCalled()
      expect(store.metrics).toEqual(mockMetrics)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('API 返回 null 时 metrics 为 null', async () => {
      getPlatformMetrics.mockResolvedValueOnce(null)

      const store = usePlatformStore()
      await store.fetchMetrics()

      expect(store.metrics).toBeNull()
      expect(store.loading).toBe(false)
    })

    it('失败时优先使用后端返回的 error 文案', async () => {
      getPlatformMetrics.mockRejectedValueOnce({ j: { error: '指标采集失败' } })

      const store = usePlatformStore()
      await store.fetchMetrics()

      expect(store.error).toBe('指标采集失败')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getPlatformMetrics.mockRejectedValueOnce({})

      const store = usePlatformStore()
      await store.fetchMetrics()

      expect(store.error).toBe('平台指标拉取失败')
      expect(store.loading).toBe(false)
    })
  })
})