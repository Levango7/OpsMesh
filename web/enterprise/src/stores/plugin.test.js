// plugin store 单元测试
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

vi.mock('@/api/plugin', () => ({
  getPlugins: vi.fn(),
  getPlugin: vi.fn(),
  installPlugin: vi.fn(),
  uninstallPlugin: vi.fn()
}))

import { usePluginStore } from '@/stores/plugin'
import {
  getPlugins,
  getPlugin,
  installPlugin,
  uninstallPlugin
} from '@/api/plugin'

describe('usePluginStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('plugins 初始为空数组', () => {
      const store = usePluginStore()
      expect(store.plugins).toEqual([])
    })

    it('searchQuery 初始为空字符串', () => {
      const store = usePluginStore()
      expect(store.searchQuery).toBe('')
    })
  })

  describe('fetchPlugins 动作', () => {
    it('成功时设置 plugins', async () => {
      const mockPlugins = [{ id: 'p1', name: 'monitor', status: 'available' }]
      getPlugins.mockResolvedValueOnce({ plugins: mockPlugins })

      const store = usePluginStore()
      await store.fetchPlugins()

      expect(store.plugins).toEqual(mockPlugins)
      expect(store.loading).toBe(false)
    })

    it('失败时设置 error', async () => {
      getPlugins.mockRejectedValueOnce({ j: { error: '获取失败' } })

      const store = usePluginStore()
      await store.fetchPlugins()

      expect(store.error).toBe('获取失败')
    })
  })

  describe('fetchPlugin 动作', () => {
    it('成功时设置 selectedPlugin', async () => {
      const mockPlugin = { id: 'p1', name: 'monitor', version: '1.0.0' }
      getPlugin.mockResolvedValueOnce(mockPlugin)

      const store = usePluginStore()
      await store.fetchPlugin('p1')

      expect(getPlugin).toHaveBeenCalledWith('p1')
      expect(store.selectedPlugin).toEqual(mockPlugin)
    })
  })

  describe('install 动作', () => {
    it('成功时返回结果', async () => {
      installPlugin.mockResolvedValueOnce({ s: 200, j: { status: 'installed' } })

      const store = usePluginStore()
      const r = await store.install('p1')

      expect(installPlugin).toHaveBeenCalledWith('p1')
      expect(r).toEqual({ s: 200, j: { status: 'installed' } })
    })
  })

  describe('uninstall 动作', () => {
    it('成功时返回结果', async () => {
      uninstallPlugin.mockResolvedValueOnce({ s: 200, j: { status: 'uninstalled' } })

      const store = usePluginStore()
      const r = await store.uninstall('p1')

      expect(uninstallPlugin).toHaveBeenCalledWith('p1')
      expect(r).toEqual({ s: 200, j: { status: 'uninstalled' } })
    })
  })

  describe('setSearch 动作', () => {
    it('设置 searchQuery', () => {
      const store = usePluginStore()
      store.setSearch('monitor')

      expect(store.searchQuery).toBe('monitor')
    })
  })

  describe('getters', () => {
    it('filteredPlugins 按搜索词过滤', () => {
      const store = usePluginStore()
      store.plugins = [
        { id: 'p1', name: 'monitor-a', description: 'system monitoring' },
        { id: 'p2', name: 'log-b', description: 'log collector' }
      ]

      store.searchQuery = 'monitor'

      expect(store.filteredPlugins.length).toBe(1)
    })

    it('installedPlugins 返回已安装插件', () => {
      const store = usePluginStore()
      store.plugins = [
        { id: 'p1', status: 'installed' },
        { id: 'p2', status: 'available' },
        { id: 'p3', status: 'installed' }
      ]

      expect(store.installedPlugins.length).toBe(2)
    })
  })
})
