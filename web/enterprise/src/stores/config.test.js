// config store 单元测试
// 覆盖：初始状态、hotpush/canary/fetchVersions 动作、loading/error 状态、
//       成功/失败分支、i18n 默认错误码回退。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

// mock @/api/config：避免真实网络请求，用 vi.fn 控制返回值
vi.mock('@/api/config', () => ({
  hotpushConfig: vi.fn(),
  canaryConfig: vi.fn(),
  getConfigVersions: vi.fn(),
}))

import { useConfigStore } from '@/stores/config'
import { hotpushConfig, canaryConfig, getConfigVersions } from '@/api/config'

describe('useConfigStore', () => {
  beforeEach(() => {
    // 每个测试前创建新 pinia 实例，隔离状态
    setActivePinia(createPinia())
    // 重置 mock 调用记录
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('versions 初始为空数组', () => {
      const store = useConfigStore()
      expect(store.versions).toEqual([])
    })

    it('lastHotpush 初始为 null', () => {
      const store = useConfigStore()
      expect(store.lastHotpush).toBeNull()
    })

    it('lastCanary 初始为 null', () => {
      const store = useConfigStore()
      expect(store.lastCanary).toBeNull()
    })

    it('loading 初始为 false', () => {
      const store = useConfigStore()
      expect(store.loading).toBe(false)
    })

    it('error 初始为空字符串', () => {
      const store = useConfigStore()
      expect(store.error).toBe('')
    })
  })

  describe('hotpush 动作', () => {
    it('成功时设置 lastHotpush 并返回结果', async () => {
      const mockResp = { s: 200, j: { version: 'v1.2', pushedAt: '2026-09-03' } }
      hotpushConfig.mockResolvedValueOnce(mockResp)

      const store = useConfigStore()
      const r = await store.hotpush({ key: 'app.yml', content: '...' })

      expect(hotpushConfig).toHaveBeenCalledWith({ key: 'app.yml', content: '...' })
      expect(store.lastHotpush).toEqual({ version: 'v1.2', pushedAt: '2026-09-03' })
      expect(r).toEqual(mockResp)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('响应体 j 为空时 lastHotpush 为 null', async () => {
      hotpushConfig.mockResolvedValueOnce({ s: 200, j: null })

      const store = useConfigStore()
      await store.hotpush({})

      expect(store.lastHotpush).toBeNull()
      expect(store.loading).toBe(false)
    })

    it('失败时设置 error 并向上抛出异常', async () => {
      const err = { j: { error: '热推送被拒绝' } }
      hotpushConfig.mockRejectedValueOnce(err)

      const store = useConfigStore()
      await expect(store.hotpush({})).rejects.toEqual(err)

      expect(store.error).toBe('热推送被拒绝')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码并抛出', async () => {
      hotpushConfig.mockRejectedValueOnce(new Error('network'))

      const store = useConfigStore()
      await expect(store.hotpush({})).rejects.toThrow('network')

      // i18n 默认回退到 zh：error.configHotpushFailed → "配置热推送失败"
      expect(store.error).toBe('配置热推送失败')
      expect(store.loading).toBe(false)
    })
  })

  describe('canary 动作', () => {
    it('成功时设置 lastCanary 并返回结果', async () => {
      const mockResp = { s: 200, j: { canaryId: 'c1', weight: 10 } }
      canaryConfig.mockResolvedValueOnce(mockResp)

      const store = useConfigStore()
      const r = await store.canary({ key: 'app.yml', weight: 10 })

      expect(canaryConfig).toHaveBeenCalledWith({ key: 'app.yml', weight: 10 })
      expect(store.lastCanary).toEqual({ canaryId: 'c1', weight: 10 })
      expect(r).toEqual(mockResp)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('响应体 j 为空时 lastCanary 为 null', async () => {
      canaryConfig.mockResolvedValueOnce({ s: 200, j: null })

      const store = useConfigStore()
      await store.canary({})

      expect(store.lastCanary).toBeNull()
      expect(store.loading).toBe(false)
    })

    it('失败时设置 error 并向上抛出异常', async () => {
      const err = { j: { error: '灰度发布冲突' } }
      canaryConfig.mockRejectedValueOnce(err)

      const store = useConfigStore()
      await expect(store.canary({})).rejects.toEqual(err)

      expect(store.error).toBe('灰度发布冲突')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码并抛出', async () => {
      canaryConfig.mockRejectedValueOnce(new Error('network'))

      const store = useConfigStore()
      await expect(store.canary({})).rejects.toThrow('network')

      expect(store.error).toBe('配置灰度发布失败')
      expect(store.loading).toBe(false)
    })
  })

  describe('fetchVersions 动作', () => {
    it('成功时从 r.versions 设置 versions', async () => {
      const mockResp = { versions: [{ id: 'v1', ts: '2026-09-01' }, { id: 'v2', ts: '2026-09-02' }] }
      getConfigVersions.mockResolvedValueOnce(mockResp)

      const store = useConfigStore()
      await store.fetchVersions({ key: 'app.yml', limit: 10 })

      expect(getConfigVersions).toHaveBeenCalledWith({ key: 'app.yml', limit: 10 })
      expect(store.versions).toEqual([
        { id: 'v1', ts: '2026-09-01' },
        { id: 'v2', ts: '2026-09-02' },
      ])
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('响应缺少 versions 字段时 versions 为空数组', async () => {
      getConfigVersions.mockResolvedValueOnce({})

      const store = useConfigStore()
      await store.fetchVersions()

      expect(store.versions).toEqual([])
      expect(store.loading).toBe(false)
    })

    it('API 返回 null 时 versions 为空数组', async () => {
      getConfigVersions.mockResolvedValueOnce(null)

      const store = useConfigStore()
      await store.fetchVersions()

      expect(store.versions).toEqual([])
      expect(store.loading).toBe(false)
    })

    it('支持按 key / agentID 过滤与 limit 限制参数', async () => {
      getConfigVersions.mockResolvedValueOnce({ versions: [{ id: 'v1' }] })

      const store = useConfigStore()
      await store.fetchVersions({ key: 'db.yml', agentID: 'a-1', limit: 5 })

      expect(getConfigVersions).toHaveBeenCalledWith({ key: 'db.yml', agentID: 'a-1', limit: 5 })
      expect(store.versions).toEqual([{ id: 'v1' }])
    })

    it('失败时优先使用后端返回的 error 文案', async () => {
      getConfigVersions.mockRejectedValueOnce({ j: { error: '版本历史不可达' } })

      const store = useConfigStore()
      await store.fetchVersions()

      expect(store.error).toBe('版本历史不可达')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getConfigVersions.mockRejectedValueOnce({})

      const store = useConfigStore()
      await store.fetchVersions()

      expect(store.error).toBe('配置版本历史拉取失败')
      expect(store.loading).toBe(false)
    })
  })
})