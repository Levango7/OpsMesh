// alert store 单元测试
// 覆盖：初始状态、fetchAlerts/ack/silence 动作、critical/warning getters。
// 说明：源码中方法名为 ack/silence（任务描述中的 ackAlert/silenceAlert 为别名语义），
//      本测试按实际导出方法验证。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

// mock @/api/alert：避免真实网络请求，用 vi.fn 控制返回值
vi.mock('@/api/alert', () => ({
  getAlerts: vi.fn(),
  ackAlert: vi.fn(),
  silenceAlert: vi.fn(),
}))

import { useAlertStore } from '@/stores/alert'
import { getAlerts, ackAlert, silenceAlert } from '@/api/alert'

describe('useAlertStore', () => {
  beforeEach(() => {
    // 每个测试前创建新 pinia 实例，隔离状态
    setActivePinia(createPinia())
    // 重置 mock 调用记录
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('list 初始为空数组', () => {
      const store = useAlertStore()
      expect(store.list).toEqual([])
    })

    it('loading 初始为 false', () => {
      const store = useAlertStore()
      expect(store.loading).toBe(false)
    })

    it('error 初始为空字符串', () => {
      const store = useAlertStore()
      expect(store.error).toBe('')
    })
  })

  describe('fetchAlerts 动作', () => {
    it('成功时设置 list', async () => {
      const mockAlerts = [
        { id: 'a1', severity: 'critical' },
        { id: 'a2', severity: 'warning' }
      ]
      getAlerts.mockResolvedValueOnce(mockAlerts)

      const store = useAlertStore()
      await store.fetchAlerts()

      expect(getAlerts).toHaveBeenCalled()
      expect(store.list).toEqual(mockAlerts)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('API 返回 null 时 list 为空数组', async () => {
      getAlerts.mockResolvedValueOnce(null)

      const store = useAlertStore()
      await store.fetchAlerts()

      expect(store.list).toEqual([])
    })

    it('失败时设置 error 并结束 loading', async () => {
      getAlerts.mockRejectedValueOnce({ j: { error: '服务不可用' } })

      const store = useAlertStore()
      await store.fetchAlerts()

      expect(store.error).toBe('服务不可用')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getAlerts.mockRejectedValueOnce({})

      const store = useAlertStore()
      await store.fetchAlerts()

      expect(store.error).toBe('告警列表拉取失败')
    })
  })

  describe('ack 动作', () => {
    it('成功时返回结果并刷新告警列表', async () => {
      ackAlert.mockResolvedValueOnce({ s: 200, j: { ok: true } })
      getAlerts.mockResolvedValueOnce([{ id: 'a1', status: 'acknowledged' }])

      const store = useAlertStore()
      const r = await store.ack('a1')

      expect(ackAlert).toHaveBeenCalledWith('a1')
      expect(getAlerts).toHaveBeenCalled()
      expect(r).toEqual({ s: 200, j: { ok: true } })
      expect(store.error).toBe('')
    })

    it('失败时设置 error 并向上抛出异常', async () => {
      ackAlert.mockRejectedValueOnce({ j: { error: 'ack 冲突' } })

      const store = useAlertStore()
      await expect(store.ack('a1')).rejects.toEqual({ j: { error: 'ack 冲突' } })

      expect(store.error).toBe('ack 冲突')
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      ackAlert.mockRejectedValueOnce({})

      const store = useAlertStore()
      await expect(store.ack('a1')).rejects.toEqual({})

      expect(store.error).toBe('告警确认失败')
    })
  })

  describe('silence 动作', () => {
    it('成功时返回结果并刷新告警列表', async () => {
      const body = { duration: 60, comment: 'investigating' }
      silenceAlert.mockResolvedValueOnce({ s: 200, j: { ok: true } })
      getAlerts.mockResolvedValueOnce([{ id: 'a1', status: 'silenced' }])

      const store = useAlertStore()
      const r = await store.silence('a1', body)

      expect(silenceAlert).toHaveBeenCalledWith('a1', body)
      expect(getAlerts).toHaveBeenCalled()
      expect(r).toEqual({ s: 200, j: { ok: true } })
      expect(store.error).toBe('')
    })

    it('失败时设置 error 并向上抛出异常', async () => {
      silenceAlert.mockRejectedValueOnce({ j: { error: '静默失败' } })

      const store = useAlertStore()
      await expect(store.silence('a1', {})).rejects.toEqual({ j: { error: '静默失败' } })

      expect(store.error).toBe('静默失败')
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      silenceAlert.mockRejectedValueOnce({})

      const store = useAlertStore()
      await expect(store.silence('a1', {})).rejects.toEqual({})

      expect(store.error).toBe('告警静默失败')
    })
  })

  describe('getters', () => {
    it('critical 仅返回 severity=critical 的告警', () => {
      const store = useAlertStore()
      store.list = [
        { id: 'a1', severity: 'critical' },
        { id: 'a2', severity: 'warning' },
        { id: 'a3', severity: 'critical' }
      ]
      expect(store.critical).toHaveLength(2)
      expect(store.critical[0].id).toBe('a1')
      expect(store.critical[1].id).toBe('a3')
    })

    it('warning 仅返回 severity=warning 的告警', () => {
      const store = useAlertStore()
      store.list = [
        { id: 'a1', severity: 'critical' },
        { id: 'a2', severity: 'warning' }
      ]
      expect(store.warning).toHaveLength(1)
      expect(store.warning[0].id).toBe('a2')
    })
  })
})