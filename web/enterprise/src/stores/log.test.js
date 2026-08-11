// log store 单元测试
// 覆盖：初始状态、search/prev/next/reset 动作、page getter、空字符串参数清理。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

// mock @/api/log：避免真实网络请求，用 vi.fn 控制返回值
vi.mock('@/api/log', () => ({
  getLogs: vi.fn(),
}))

import { useLogStore } from '@/stores/log'
import { getLogs } from '@/api/log'

describe('useLogStore', () => {
  beforeEach(() => {
    // 每个测试前创建新 pinia 实例，隔离状态
    setActivePinia(createPinia())
    // 重置 mock 调用记录
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('filters 初始包含默认值（limit=200）', () => {
      const store = useLogStore()
      expect(store.filters).toEqual({
        deviceID: '', agentID: '', level: '', source: '',
        keyword: '', from: '', to: '', limit: 200
      })
    })

    it('list 初始为空数组', () => {
      const store = useLogStore()
      expect(store.list).toEqual([])
    })

    it('offset 初始为 0', () => {
      const store = useLogStore()
      expect(store.offset).toBe(0)
    })

    it('pageSize 初始为 0', () => {
      const store = useLogStore()
      expect(store.pageSize).toBe(0)
    })

    it('loading 初始为 false', () => {
      const store = useLogStore()
      expect(store.loading).toBe(false)
    })

    it('error 初始为空字符串', () => {
      const store = useLogStore()
      expect(store.error).toBe('')
    })
  })

  describe('page getter', () => {
    it('offset=0 时 page=1', () => {
      const store = useLogStore()
      expect(store.page).toBe(1)
    })

    it('offset=200 limit=200 时 page=2', () => {
      const store = useLogStore()
      store.offset = 200
      expect(store.page).toBe(2)
    })

    it('offset=500 limit=200 时 page=3', () => {
      const store = useLogStore()
      store.offset = 500
      expect(store.page).toBe(3)
    })
  })

  describe('search 动作', () => {
    it('成功时设置 list 与 pageSize', async () => {
      const mockLogs = [{ id: 'l1' }, { id: 'l2' }]
      getLogs.mockResolvedValueOnce(mockLogs)

      const store = useLogStore()
      await store.search()

      expect(store.list).toEqual(mockLogs)
      expect(store.pageSize).toBe(2)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('调用 API 时传入 offset 与 filters 合并参数', async () => {
      getLogs.mockResolvedValueOnce([])
      const store = useLogStore()
      store.filters.deviceID = 'd1'
      store.filters.level = 'error'

      await store.search(100)

      expect(getLogs).toHaveBeenCalledWith(expect.objectContaining({
        deviceID: 'd1', level: 'error', offset: 100, limit: 200
      }))
    })

    it('清空字符串与 null 参数后再传给 API', async () => {
      getLogs.mockResolvedValueOnce([])
      const store = useLogStore()
      // 全部为空字符串
      await store.search(0)

      const callArgs = getLogs.mock.calls[0][0]
      expect(callArgs).not.toHaveProperty('deviceID')
      expect(callArgs).not.toHaveProperty('agentID')
      expect(callArgs).not.toHaveProperty('level')
      expect(callArgs).not.toHaveProperty('source')
      expect(callArgs).not.toHaveProperty('keyword')
      expect(callArgs).not.toHaveProperty('from')
      expect(callArgs).not.toHaveProperty('to')
      // limit 与 offset 是数字，应保留
      expect(callArgs).toHaveProperty('limit', 200)
      expect(callArgs).toHaveProperty('offset', 0)
    })

    it('search 后 offset 更新为传入值', async () => {
      getLogs.mockResolvedValueOnce([])
      const store = useLogStore()

      await store.search(400)

      expect(store.offset).toBe(400)
    })

    it('API 返回 null 时 list 为空数组', async () => {
      getLogs.mockResolvedValueOnce(null)

      const store = useLogStore()
      await store.search()

      expect(store.list).toEqual([])
      expect(store.pageSize).toBe(0)
    })

    it('失败时设置 error 并结束 loading', async () => {
      getLogs.mockRejectedValueOnce({ j: { error: '参数非法' } })

      const store = useLogStore()
      await store.search()

      expect(store.error).toBe('参数非法')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用默认错误码', async () => {
      getLogs.mockRejectedValueOnce({})

      const store = useLogStore()
      await store.search()

      expect(store.error).toBe('日志检索失败')
    })
  })

  describe('prev 动作', () => {
    it('offset>0 时向前翻页调用 search', async () => {
      getLogs.mockResolvedValueOnce([])
      const store = useLogStore()
      store.offset = 400

      await store.prev()

      // 400 - 200 = 200
      expect(getLogs).toHaveBeenCalled()
      expect(store.offset).toBe(200)
    })

    it('offset=0 时不调用 search', async () => {
      const store = useLogStore()
      await store.prev()

      expect(getLogs).not.toHaveBeenCalled()
      expect(store.offset).toBe(0)
    })

    it('offset 不足一页时回退到 0', async () => {
      getLogs.mockResolvedValueOnce([])
      const store = useLogStore()
      store.offset = 100

      await store.prev()

      // max(0, 100 - 200) = 0
      expect(store.offset).toBe(0)
    })
  })

  describe('next 动作', () => {
    it('向后翻页调用 search 并更新 offset', async () => {
      getLogs.mockResolvedValueOnce([])
      const store = useLogStore()

      await store.next()

      expect(getLogs).toHaveBeenCalled()
      expect(store.offset).toBe(200)
    })
  })

  describe('reset 动作', () => {
    it('重置 filters 到默认值', () => {
      const store = useLogStore()
      store.filters.deviceID = 'd1'
      store.filters.limit = 50

      store.reset()

      expect(store.filters).toEqual({
        deviceID: '', agentID: '', level: '', source: '',
        keyword: '', from: '', to: '', limit: 200
      })
    })

    it('清空 list、offset、pageSize', () => {
      const store = useLogStore()
      store.list = [{ id: 'l1' }]
      store.offset = 400
      store.pageSize = 200

      store.reset()

      expect(store.list).toEqual([])
      expect(store.offset).toBe(0)
      expect(store.pageSize).toBe(0)
    })
  })
})