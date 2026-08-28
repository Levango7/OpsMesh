// bot store 单元测试
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

vi.mock('@/api/bot', () => ({
  executeCommand: vi.fn(),
  getCommandHistory: vi.fn(),
  getBotPlatforms: vi.fn(),
  getQuickCommands: vi.fn()
}))

import { useBotStore } from '@/stores/bot'
import {
  executeCommand,
  getCommandHistory,
  getBotPlatforms,
  getQuickCommands
} from '@/api/bot'

describe('useBotStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('history 初始为空数组', () => {
      const store = useBotStore()
      expect(store.history).toEqual([])
    })

    it('platforms 初始为空数组', () => {
      const store = useBotStore()
      expect(store.platforms).toEqual([])
    })

    it('selectedPlatform 初始为空字符串', () => {
      const store = useBotStore()
      expect(store.selectedPlatform).toBe('')
    })

    it('executing 初始为 false', () => {
      const store = useBotStore()
      expect(store.executing).toBe(false)
    })
  })

  describe('fetchPlatforms 动作', () => {
    it('成功时设置 platforms', async () => {
      const mockPlatforms = [{ id: 'wecom', name: '企业微信', enabled: true }]
      getBotPlatforms.mockResolvedValueOnce({ platforms: mockPlatforms })

      const store = useBotStore()
      await store.fetchPlatforms()

      expect(store.platforms).toEqual(mockPlatforms)
    })

    it('失败时 platforms 为空数组', async () => {
      getBotPlatforms.mockRejectedValueOnce(new Error('fail'))

      const store = useBotStore()
      await store.fetchPlatforms()

      expect(store.platforms).toEqual([])
    })
  })

  describe('fetchQuickCommands 动作', () => {
    it('成功时设置 quickCommands', async () => {
      const mockCmds = [{ label: '状态', command: 'status', platform: 'wecom' }]
      getQuickCommands.mockResolvedValueOnce({ commands: mockCmds })

      const store = useBotStore()
      await store.fetchQuickCommands()

      expect(store.quickCommands).toEqual(mockCmds)
    })
  })

  describe('fetchHistory 动作', () => {
    it('成功时设置 history', async () => {
      const mockHistory = [{ id: 'h1', command: 'status', status: 'success' }]
      getCommandHistory.mockResolvedValueOnce({ history: mockHistory })

      const store = useBotStore()
      await store.fetchHistory('wecom', 20)

      expect(getCommandHistory).toHaveBeenCalledWith('wecom', 20)
      expect(store.history).toEqual(mockHistory)
    })

    it('失败时设置 error', async () => {
      getCommandHistory.mockRejectedValueOnce({ j: { error: '获取失败' } })

      const store = useBotStore()
      await store.fetchHistory()

      expect(store.error).toBe('获取失败')
    })
  })

  describe('runCommand 动作', () => {
    it('成功时添加到 history 并返回结果', async () => {
      const mockResult = { id: 'r1', command: 'status', response: 'ok', status: 'success' }
      executeCommand.mockResolvedValueOnce({ s: 200, j: mockResult })

      const store = useBotStore()
      const result = await store.runCommand('status', 'wecom')

      expect(executeCommand).toHaveBeenCalledWith('status', 'wecom')
      expect(result).toEqual(mockResult)
      expect(store.history[0]).toEqual(mockResult)
    })

    it('失败时返回 null 并设置 error', async () => {
      executeCommand.mockRejectedValueOnce({ j: { error: '执行失败' } })

      const store = useBotStore()
      const result = await store.runCommand('bad-cmd', 'wecom')

      expect(result).toBeNull()
      expect(store.error).toBe('执行失败')
    })

    it('HTTP 非 2xx 返回 null', async () => {
      executeCommand.mockResolvedValueOnce({ s: 500, j: { error: 'server error' } })

      const store = useBotStore()
      const result = await store.runCommand('cmd', 'wecom')

      expect(result).toBeNull()
    })
  })

  describe('selectPlatform 动作', () => {
    it('设置 selectedPlatform 并拉取历史', async () => {
      getCommandHistory.mockResolvedValueOnce({ history: [] })

      const store = useBotStore()
      store.selectPlatform('feishu')

      expect(store.selectedPlatform).toBe('feishu')
    })
  })

  describe('getters', () => {
    it('enabledPlatforms 返回启用的平台', () => {
      const store = useBotStore()
      store.platforms = [
        { id: 'wecom', name: '企业微信', enabled: true },
        { id: 'slack', name: 'Slack', enabled: false }
      ]

      expect(store.enabledPlatforms).toEqual([{ id: 'wecom', name: '企业微信', enabled: true }])
    })

    it('recentHistory 返回前 20 条', () => {
      const store = useBotStore()
      store.history = Array.from({ length: 30 }, (_, i) => ({ id: `h${i}` }))

      expect(store.recentHistory.length).toBe(20)
    })
  })
})
