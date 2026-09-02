// alert-rule store 单元测试
// 覆盖：初始状态、告警规则 CRUD（fetchRules/createRule/updateRule/removeRule）、
//      引擎规则 CRUD（fetchEngineRules/createEngineRule/updateEngineRule/removeEngineRule）、
//      静默规则（fetchSilences/createSilence/removeSilence）、loading/error 状态。
// 说明：源码中方法名为 removeRule/removeEngineRule/removeSilence（任务描述中的
//      deleteRule/deleteEngineRule/deleteSilence 为别名语义），本测试按实际导出方法验证。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

// mock @/api/alert-rule：避免真实网络请求，用 vi.fn 控制返回值
vi.mock('@/api/alert-rule', () => ({
  getAlertRules: vi.fn(),
  createAlertRule: vi.fn(),
  updateAlertRule: vi.fn(),
  deleteAlertRule: vi.fn(),
  getAlertEngineRules: vi.fn(),
  createAlertEngineRule: vi.fn(),
  updateAlertEngineRule: vi.fn(),
  deleteAlertEngineRule: vi.fn(),
  getAlertSilences: vi.fn(),
  createAlertSilence: vi.fn(),
  deleteAlertSilence: vi.fn()
}))

import { useAlertRuleStore } from '@/stores/alert-rule'
import {
  getAlertRules, createAlertRule, updateAlertRule, deleteAlertRule,
  getAlertEngineRules, createAlertEngineRule, updateAlertEngineRule, deleteAlertEngineRule,
  getAlertSilences, createAlertSilence, deleteAlertSilence
} from '@/api/alert-rule'

describe('useAlertRuleStore', () => {
  beforeEach(() => {
    // 每个测试前创建新 pinia 实例，隔离状态
    setActivePinia(createPinia())
    // 重置 mock 调用记录
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('rules 初始为空数组', () => {
      const store = useAlertRuleStore()
      expect(store.rules).toEqual([])
    })

    it('engineRules 初始为空数组', () => {
      const store = useAlertRuleStore()
      expect(store.engineRules).toEqual([])
    })

    it('silences 初始为空数组', () => {
      const store = useAlertRuleStore()
      expect(store.silences).toEqual([])
    })

    it('loading 初始为 false', () => {
      const store = useAlertRuleStore()
      expect(store.loading).toBe(false)
    })

    it('error 初始为空字符串', () => {
      const store = useAlertRuleStore()
      expect(store.error).toBe('')
    })
  })

  // ---------- 告警规则 ----------
  describe('fetchRules 动作', () => {
    it('成功时设置 rules（响应含 rules 字段）', async () => {
      const mockResp = { rules: [{ id: 'r1', name: 'cpu-high' }] }
      getAlertRules.mockResolvedValueOnce(mockResp)

      const store = useAlertRuleStore()
      await store.fetchRules()

      expect(getAlertRules).toHaveBeenCalled()
      expect(store.rules).toEqual(mockResp.rules)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('成功时设置 rules（响应直接为数组）', async () => {
      const mockRules = [{ id: 'r1' }, { id: 'r2' }]
      getAlertRules.mockResolvedValueOnce(mockRules)

      const store = useAlertRuleStore()
      await store.fetchRules()

      expect(store.rules).toEqual(mockRules)
    })

    it('API 返回 null 时 rules 为空数组', async () => {
      getAlertRules.mockResolvedValueOnce(null)

      const store = useAlertRuleStore()
      await store.fetchRules()

      expect(store.rules).toEqual([])
    })

    it('失败时设置 error 并结束 loading', async () => {
      getAlertRules.mockRejectedValueOnce({ j: { error: '网络异常' } })

      const store = useAlertRuleStore()
      await store.fetchRules()

      expect(store.error).toBe('网络异常')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getAlertRules.mockRejectedValueOnce({})

      const store = useAlertRuleStore()
      await store.fetchRules()

      // i18n 默认回退到 zh：error.alertRuleListFailed → "告警规则列表拉取失败"
      expect(store.error).toBe('告警规则列表拉取失败')
    })
  })

  describe('createRule 动作', () => {
    it('成功时返回结果并刷新规则列表', async () => {
      const body = { name: 'mem-high', metric: 'mem', op: '>', threshold: 0.9 }
      createAlertRule.mockResolvedValueOnce({ s: 200, j: { ok: true } })
      getAlertRules.mockResolvedValueOnce({ rules: [{ id: 'r1' }] })

      const store = useAlertRuleStore()
      const r = await store.createRule(body)

      expect(createAlertRule).toHaveBeenCalledWith(body)
      expect(getAlertRules).toHaveBeenCalled()
      expect(r).toEqual({ s: 200, j: { ok: true } })
    })

    it('API 抛错时向上抛出异常', async () => {
      createAlertRule.mockRejectedValueOnce(new Error('create failed'))

      const store = useAlertRuleStore()
      await expect(store.createRule({})).rejects.toThrow('create failed')
    })
  })

  describe('updateRule 动作', () => {
    it('成功时返回结果并刷新规则列表', async () => {
      const body = { threshold: 0.95 }
      updateAlertRule.mockResolvedValueOnce({ s: 200, j: { ok: true } })
      getAlertRules.mockResolvedValueOnce({ rules: [] })

      const store = useAlertRuleStore()
      const r = await store.updateRule('r1', body)

      expect(updateAlertRule).toHaveBeenCalledWith('r1', body)
      expect(getAlertRules).toHaveBeenCalled()
      expect(r).toEqual({ s: 200, j: { ok: true } })
    })

    it('API 抛错时向上抛出异常', async () => {
      updateAlertRule.mockRejectedValueOnce(new Error('update failed'))

      const store = useAlertRuleStore()
      await expect(store.updateRule('r1', {})).rejects.toThrow('update failed')
    })
  })

  describe('removeRule 动作', () => {
    it('成功时返回结果并刷新规则列表', async () => {
      deleteAlertRule.mockResolvedValueOnce({ s: 204 })
      getAlertRules.mockResolvedValueOnce({ rules: [] })

      const store = useAlertRuleStore()
      const r = await store.removeRule('r1')

      expect(deleteAlertRule).toHaveBeenCalledWith('r1')
      expect(getAlertRules).toHaveBeenCalled()
      expect(r).toEqual({ s: 204 })
    })

    it('API 抛错时向上抛出异常', async () => {
      deleteAlertRule.mockRejectedValueOnce(new Error('delete failed'))

      const store = useAlertRuleStore()
      await expect(store.removeRule('r1')).rejects.toThrow('delete failed')
    })
  })

  // ---------- 多条件引擎规则 ----------
  describe('fetchEngineRules 动作', () => {
    it('成功时设置 engineRules（响应含 rules 字段）', async () => {
      const mockResp = { rules: [{ id: 'e1', conditions: [] }] }
      getAlertEngineRules.mockResolvedValueOnce(mockResp)

      const store = useAlertRuleStore()
      await store.fetchEngineRules()

      expect(getAlertEngineRules).toHaveBeenCalled()
      expect(store.engineRules).toEqual(mockResp.rules)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('成功时设置 engineRules（响应直接为数组）', async () => {
      const mockRules = [{ id: 'e1' }]
      getAlertEngineRules.mockResolvedValueOnce(mockRules)

      const store = useAlertRuleStore()
      await store.fetchEngineRules()

      expect(store.engineRules).toEqual(mockRules)
    })

    it('API 返回 null 时 engineRules 为空数组', async () => {
      getAlertEngineRules.mockResolvedValueOnce(null)

      const store = useAlertRuleStore()
      await store.fetchEngineRules()

      expect(store.engineRules).toEqual([])
    })

    it('失败时设置 error 并结束 loading', async () => {
      getAlertEngineRules.mockRejectedValueOnce({ j: { error: '引擎不可用' } })

      const store = useAlertRuleStore()
      await store.fetchEngineRules()

      expect(store.error).toBe('引擎不可用')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getAlertEngineRules.mockRejectedValueOnce({})

      const store = useAlertRuleStore()
      await store.fetchEngineRules()

      // i18n 默认回退到 zh：error.alertEngineListFailed → "引擎规则列表拉取失败"
      expect(store.error).toBe('引擎规则列表拉取失败')
    })
  })

  describe('createEngineRule 动作', () => {
    it('成功时返回结果并刷新引擎规则列表', async () => {
      const body = { name: 'multi', conditions: [], action: 'alert' }
      createAlertEngineRule.mockResolvedValueOnce({ s: 200, j: { ok: true } })
      getAlertEngineRules.mockResolvedValueOnce({ rules: [] })

      const store = useAlertRuleStore()
      const r = await store.createEngineRule(body)

      expect(createAlertEngineRule).toHaveBeenCalledWith(body)
      expect(getAlertEngineRules).toHaveBeenCalled()
      expect(r).toEqual({ s: 200, j: { ok: true } })
    })

    it('API 抛错时向上抛出异常', async () => {
      createAlertEngineRule.mockRejectedValueOnce(new Error('create engine failed'))

      const store = useAlertRuleStore()
      await expect(store.createEngineRule({})).rejects.toThrow('create engine failed')
    })
  })

  describe('updateEngineRule 动作', () => {
    it('成功时返回结果并刷新引擎规则列表', async () => {
      const body = { action: 'silence' }
      updateAlertEngineRule.mockResolvedValueOnce({ s: 200, j: { ok: true } })
      getAlertEngineRules.mockResolvedValueOnce({ rules: [] })

      const store = useAlertRuleStore()
      const r = await store.updateEngineRule('e1', body)

      expect(updateAlertEngineRule).toHaveBeenCalledWith('e1', body)
      expect(getAlertEngineRules).toHaveBeenCalled()
      expect(r).toEqual({ s: 200, j: { ok: true } })
    })

    it('API 抛错时向上抛出异常', async () => {
      updateAlertEngineRule.mockRejectedValueOnce(new Error('update engine failed'))

      const store = useAlertRuleStore()
      await expect(store.updateEngineRule('e1', {})).rejects.toThrow('update engine failed')
    })
  })

  describe('removeEngineRule 动作', () => {
    it('成功时返回结果并刷新引擎规则列表', async () => {
      deleteAlertEngineRule.mockResolvedValueOnce({ s: 204 })
      getAlertEngineRules.mockResolvedValueOnce({ rules: [] })

      const store = useAlertRuleStore()
      const r = await store.removeEngineRule('e1')

      expect(deleteAlertEngineRule).toHaveBeenCalledWith('e1')
      expect(getAlertEngineRules).toHaveBeenCalled()
      expect(r).toEqual({ s: 204 })
    })

    it('API 抛错时向上抛出异常', async () => {
      deleteAlertEngineRule.mockRejectedValueOnce(new Error('delete engine failed'))

      const store = useAlertRuleStore()
      await expect(store.removeEngineRule('e1')).rejects.toThrow('delete engine failed')
    })
  })

  // ---------- 静默规则 ----------
  describe('fetchSilences 动作', () => {
    it('成功时设置 silences（响应含 silences 字段）', async () => {
      const mockResp = { silences: [{ id: 's1', matchers: [] }] }
      getAlertSilences.mockResolvedValueOnce(mockResp)

      const store = useAlertRuleStore()
      await store.fetchSilences()

      expect(getAlertSilences).toHaveBeenCalled()
      expect(store.silences).toEqual(mockResp.silences)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('成功时设置 silences（响应直接为数组）', async () => {
      const mockSilences = [{ id: 's1' }]
      getAlertSilences.mockResolvedValueOnce(mockSilences)

      const store = useAlertRuleStore()
      await store.fetchSilences()

      expect(store.silences).toEqual(mockSilences)
    })

    it('API 返回 null 时 silences 为空数组', async () => {
      getAlertSilences.mockResolvedValueOnce(null)

      const store = useAlertRuleStore()
      await store.fetchSilences()

      expect(store.silences).toEqual([])
    })

    it('失败时设置 error 并结束 loading', async () => {
      getAlertSilences.mockRejectedValueOnce({ j: { error: '静默列表不可用' } })

      const store = useAlertRuleStore()
      await store.fetchSilences()

      expect(store.error).toBe('静默列表不可用')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getAlertSilences.mockRejectedValueOnce({})

      const store = useAlertRuleStore()
      await store.fetchSilences()

      // i18n 默认回退到 zh：error.alertSilenceListFailed → "静默规则列表拉取失败"
      expect(store.error).toBe('静默规则列表拉取失败')
    })
  })

  describe('createSilence 动作', () => {
    it('成功时返回结果并刷新静默列表', async () => {
      const body = { matchers: [], startsAt: '2026-01-01T00:00:00Z', endsAt: '2026-01-01T01:00:00Z', comment: 'investigating' }
      createAlertSilence.mockResolvedValueOnce({ s: 200, j: { ok: true } })
      getAlertSilences.mockResolvedValueOnce({ silences: [] })

      const store = useAlertRuleStore()
      const r = await store.createSilence(body)

      expect(createAlertSilence).toHaveBeenCalledWith(body)
      expect(getAlertSilences).toHaveBeenCalled()
      expect(r).toEqual({ s: 200, j: { ok: true } })
    })

    it('API 抛错时向上抛出异常', async () => {
      createAlertSilence.mockRejectedValueOnce(new Error('create silence failed'))

      const store = useAlertRuleStore()
      await expect(store.createSilence({})).rejects.toThrow('create silence failed')
    })
  })

  describe('removeSilence 动作', () => {
    it('成功时返回结果并刷新静默列表', async () => {
      deleteAlertSilence.mockResolvedValueOnce({ s: 204 })
      getAlertSilences.mockResolvedValueOnce({ silences: [] })

      const store = useAlertRuleStore()
      const r = await store.removeSilence('s1')

      expect(deleteAlertSilence).toHaveBeenCalledWith('s1')
      expect(getAlertSilences).toHaveBeenCalled()
      expect(r).toEqual({ s: 204 })
    })

    it('API 抛错时向上抛出异常', async () => {
      deleteAlertSilence.mockRejectedValueOnce(new Error('delete silence failed'))

      const store = useAlertRuleStore()
      await expect(store.removeSilence('s1')).rejects.toThrow('delete silence failed')
    })
  })
})