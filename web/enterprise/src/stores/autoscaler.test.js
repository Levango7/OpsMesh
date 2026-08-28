// autoscaler store 单元测试
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

vi.mock('@/api/autoscaler', () => ({
  getScalingRules: vi.fn(),
  createScalingRule: vi.fn(),
  updateScalingRule: vi.fn(),
  deleteScalingRule: vi.fn(),
  getScalingDecisions: vi.fn(),
  manualScale: vi.fn(),
  getCooldowns: vi.fn()
}))

import { useAutoscalerStore } from '@/stores/autoscaler'
import {
  getScalingRules,
  createScalingRule,
  deleteScalingRule,
  getScalingDecisions,
  manualScale,
  getCooldowns
} from '@/api/autoscaler'

describe('useAutoscalerStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('rules 初始为空数组', () => {
      const store = useAutoscalerStore()
      expect(store.rules).toEqual([])
    })

    it('decisions 初始为空数组', () => {
      const store = useAutoscalerStore()
      expect(store.decisions).toEqual([])
    })

    it('cooldowns 初始为空数组', () => {
      const store = useAutoscalerStore()
      expect(store.cooldowns).toEqual([])
    })
  })

  describe('fetchRules 动作', () => {
    it('成功时设置 rules', async () => {
      const mockRules = [{ id: 'r1', name: 'cpu-scale', metric: 'cpu', threshold: 80, enabled: true }]
      getScalingRules.mockResolvedValueOnce({ rules: mockRules })

      const store = useAutoscalerStore()
      await store.fetchRules()

      expect(store.rules).toEqual(mockRules)
      expect(store.loading).toBe(false)
    })

    it('失败时设置 error', async () => {
      getScalingRules.mockRejectedValueOnce({ j: { error: '获取失败' } })

      const store = useAutoscalerStore()
      await store.fetchRules()

      expect(store.error).toBe('获取失败')
    })
  })

  describe('addRule 动作', () => {
    it('成功时返回结果', async () => {
      createScalingRule.mockResolvedValueOnce({ s: 200, j: { id: 'r1' } })

      const store = useAutoscalerStore()
      const r = await store.addRule('cpu-scale', 'cpu', 80, 1, 10, 300)

      expect(createScalingRule).toHaveBeenCalledWith('cpu-scale', 'cpu', 80, 1, 10, 300)
      expect(r).toEqual({ s: 200, j: { id: 'r1' } })
    })
  })

  describe('removeRule 动作', () => {
    it('成功时返回结果', async () => {
      deleteScalingRule.mockResolvedValueOnce({ s: 204, j: null })

      const store = useAutoscalerStore()
      const r = await store.removeRule('r1')

      expect(deleteScalingRule).toHaveBeenCalledWith('r1')
      expect(r).toEqual({ s: 204, j: null })
    })
  })

  describe('fetchDecisions 动作', () => {
    it('成功时设置 decisions', async () => {
      const mockDecisions = [{ id: 'd1', action: 'scale-up', fromReplicas: 2, toReplicas: 4 }]
      getScalingDecisions.mockResolvedValueOnce({ decisions: mockDecisions })

      const store = useAutoscalerStore()
      await store.fetchDecisions()

      expect(store.decisions).toEqual(mockDecisions)
    })
  })

  describe('triggerScale 动作', () => {
    it('成功时返回结果', async () => {
      manualScale.mockResolvedValueOnce({ s: 200, j: { status: 'ok' } })

      const store = useAutoscalerStore()
      const r = await store.triggerScale('deploy/app', 5, 'manual')

      expect(manualScale).toHaveBeenCalledWith('deploy/app', 5, 'manual')
      expect(r).toEqual({ s: 200, j: { status: 'ok' } })
    })
  })

  describe('fetchCooldowns 动作', () => {
    it('成功时设置 cooldowns', async () => {
      const mockCooldowns = [{ ruleId: 'r1', ruleName: 'cpu-scale', remaining: 120, expiresAt: '2026-01-01T00:05:00Z' }]
      getCooldowns.mockResolvedValueOnce({ cooldowns: mockCooldowns })

      const store = useAutoscalerStore()
      await store.fetchCooldowns()

      expect(store.cooldowns).toEqual(mockCooldowns)
    })
  })

  describe('getters', () => {
    it('enabledRules 返回启用的规则', () => {
      const store = useAutoscalerStore()
      store.rules = [
        { id: 'r1', enabled: true },
        { id: 'r2', enabled: false },
        { id: 'r3', enabled: true }
      ]

      expect(store.enabledRules.length).toBe(2)
    })

    it('activeCooldowns 返回剩余时间 > 0 的冷却', () => {
      const store = useAutoscalerStore()
      store.cooldowns = [
        { ruleId: 'r1', remaining: 120 },
        { ruleId: 'r2', remaining: 0 },
        { ruleId: 'r3', remaining: 60 }
      ]

      expect(store.activeCooldowns.length).toBe(2)
    })
  })
})
