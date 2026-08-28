// incident store 单元测试
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

vi.mock('@/api/incident', () => ({
  getIncidents: vi.fn(),
  createIncident: vi.fn(),
  getIncident: vi.fn(),
  updateIncident: vi.fn(),
  deleteIncident: vi.fn(),
  getIncidentTimeline: vi.fn(),
  addTimelineEvent: vi.fn(),
  generatePostmortem: vi.fn(),
  getIncidentMetrics: vi.fn()
}))

import { useIncidentStore } from '@/stores/incident'
import {
  getIncidents,
  createIncident,
  getIncident,
  getIncidentTimeline,
  generatePostmortem,
  getIncidentMetrics
} from '@/api/incident'

describe('useIncidentStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('incidents 初始为空数组', () => {
      const store = useIncidentStore()
      expect(store.incidents).toEqual([])
    })

    it('currentIncident 初始为 null', () => {
      const store = useIncidentStore()
      expect(store.currentIncident).toBeNull()
    })

    it('timeline 初始为空数组', () => {
      const store = useIncidentStore()
      expect(store.timeline).toEqual([])
    })

    it('metrics 初始为 null', () => {
      const store = useIncidentStore()
      expect(store.metrics).toBeNull()
    })
  })

  describe('fetchIncidents 动作', () => {
    it('成功时设置 incidents', async () => {
      const mockIncidents = [{ id: 'i1', title: 'DB Down', severity: 'critical', status: 'open' }]
      getIncidents.mockResolvedValueOnce({ incidents: mockIncidents })

      const store = useIncidentStore()
      await store.fetchIncidents()

      expect(store.incidents).toEqual(mockIncidents)
      expect(store.loading).toBe(false)
    })

    it('失败时设置 error', async () => {
      getIncidents.mockRejectedValueOnce({ j: { error: '获取失败' } })

      const store = useIncidentStore()
      await store.fetchIncidents()

      expect(store.error).toBe('获取失败')
    })
  })

  describe('addIncident 动作', () => {
    it('成功时返回结果', async () => {
      createIncident.mockResolvedValueOnce({ s: 200, j: { id: 'i1' } })

      const store = useIncidentStore()
      const r = await store.addIncident('DB Down', 'critical', 'desc', 'ops-team')

      expect(createIncident).toHaveBeenCalledWith('DB Down', 'critical', 'desc', 'ops-team')
      expect(r).toEqual({ s: 200, j: { id: 'i1' } })
    })
  })

  describe('fetchIncident 动作', () => {
    it('成功时设置 currentIncident', async () => {
      const mockIncident = { id: 'i1', title: 'DB Down', status: 'open' }
      getIncident.mockResolvedValueOnce(mockIncident)

      const store = useIncidentStore()
      await store.fetchIncident('i1')

      expect(getIncident).toHaveBeenCalledWith('i1')
      expect(store.currentIncident).toEqual(mockIncident)
    })
  })

  describe('fetchTimeline 动作', () => {
    it('成功时设置 timeline', async () => {
      const mockEvents = [{ id: 'e1', type: 'detected', content: 'Alert fired', timestamp: '2026-01-01T00:00:00Z' }]
      getIncidentTimeline.mockResolvedValueOnce({ events: mockEvents })

      const store = useIncidentStore()
      await store.fetchTimeline('i1')

      expect(getIncidentTimeline).toHaveBeenCalledWith('i1')
      expect(store.timeline).toEqual(mockEvents)
    })
  })

  describe('fetchPostmortem 动作', () => {
    it('成功时设置 postmortemContent', async () => {
      generatePostmortem.mockResolvedValueOnce({ content: '# Postmortem\n## Summary...' })

      const store = useIncidentStore()
      await store.fetchPostmortem('i1')

      expect(generatePostmortem).toHaveBeenCalledWith('i1')
      expect(store.postmortemContent).toBe('# Postmortem\n## Summary...')
    })
  })

  describe('fetchMetrics 动作', () => {
    it('成功时设置 metrics', async () => {
      const mockMetrics = { mttd: '5min', mttr: '30min', total: 10, open: 2, resolved: 8 }
      getIncidentMetrics.mockResolvedValueOnce(mockMetrics)

      const store = useIncidentStore()
      await store.fetchMetrics()

      expect(store.metrics).toEqual(mockMetrics)
    })
  })

  describe('getters', () => {
    it('openIncidents 返回待处理事件', () => {
      const store = useIncidentStore()
      store.incidents = [
        { id: 'i1', status: 'open' },
        { id: 'i2', status: 'resolved' },
        { id: 'i3', status: 'open' }
      ]

      expect(store.openIncidents.length).toBe(2)
    })

    it('criticalIncidents 返回严重事件', () => {
      const store = useIncidentStore()
      store.incidents = [
        { id: 'i1', severity: 'critical' },
        { id: 'i2', severity: 'low' },
        { id: 'i3', severity: 'critical' }
      ]

      expect(store.criticalIncidents.length).toBe(2)
    })
  })
})
