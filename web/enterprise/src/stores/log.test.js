// log store 单元测试
// 覆盖：初始状态、search/prev/next/reset 动作、page getter、空字符串参数清理。
// 覆盖：高级查询模式（KQL/Lucene 语法）、模式切换、语法错误处理。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

// mock @/api/log：避免真实网络请求，用 vi.fn 控制返回值
vi.mock('@/api/log', () => ({
  getLogs: vi.fn(),
  queryLogs: vi.fn(),
}))

import { useLogStore } from '@/stores/log'
import { getLogs, queryLogs } from '@/api/log'

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

    it('mode 初始为 simple', () => {
      const store = useLogStore()
      expect(store.mode).toBe('simple')
    })

    it('q 初始为空字符串', () => {
      const store = useLogStore()
      expect(store.q).toBe('')
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

  describe('search 动作（simple 模式）', () => {
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

    it('失败时保留上次的结果列表', async () => {
      getLogs.mockResolvedValueOnce([{ id: 'l1' }])
      getLogs.mockRejectedValueOnce({ j: { error: '网络错误' } })

      const store = useLogStore()
      await store.search() // 第一次成功
      expect(store.list).toHaveLength(1)

      await store.search() // 第二次失败
      expect(store.list).toHaveLength(1) // 保留上次结果
      expect(store.error).toBe('网络错误')
    })
  })

  describe('search 动作（advanced 模式）', () => {
    it('advanced 模式调用 queryLogs 而非 getLogs', async () => {
      queryLogs.mockResolvedValueOnce([])
      const store = useLogStore()
      store.setMode('advanced')
      store.q = 'level=error'

      await store.search(0)

      expect(queryLogs).toHaveBeenCalled()
      expect(getLogs).not.toHaveBeenCalled()
    })

    it('advanced 模式传入 q 参数', async () => {
      queryLogs.mockResolvedValueOnce([])
      const store = useLogStore()
      store.setMode('advanced')
      store.q = 'level=error AND device=dev-1'

      await store.search(0)

      expect(queryLogs).toHaveBeenCalledWith(expect.objectContaining({
        q: 'level=error AND device=dev-1'
      }))
    })

    it('advanced 模式传入 from/to/limit/offset', async () => {
      queryLogs.mockResolvedValueOnce([])
      const store = useLogStore()
      store.setMode('advanced')
      store.q = 'level=error'
      store.filters.from = '2026-01-01T00:00:00Z'
      store.filters.to = '2026-01-02T00:00:00Z'
      store.filters.limit = 50

      await store.search(100)

      expect(queryLogs).toHaveBeenCalledWith(expect.objectContaining({
        q: 'level=error',
        from: '2026-01-01T00:00:00Z',
        to: '2026-01-02T00:00:00Z',
        limit: 50,
        offset: 100
      }))
    })

    it('advanced 模式不传入简单搜索的字段（deviceID/level 等）', async () => {
      queryLogs.mockResolvedValueOnce([])
      const store = useLogStore()
      store.setMode('advanced')
      store.q = 'level=error'
      // 即使 filters 里有值，advanced 模式也不应传这些字段
      store.filters.deviceID = 'd1'
      store.filters.level = 'warn'
      store.filters.keyword = 'foo'

      await store.search(0)

      const callArgs = queryLogs.mock.calls[0][0]
      expect(callArgs).not.toHaveProperty('deviceID')
      expect(callArgs).not.toHaveProperty('level')
      expect(callArgs).not.toHaveProperty('keyword')
      expect(callArgs).toHaveProperty('q', 'level=error')
    })

    it('advanced 模式 q 为空时仍调用 API（让后端决定行为）', async () => {
      queryLogs.mockResolvedValueOnce([])
      const store = useLogStore()
      store.setMode('advanced')
      store.q = ''

      await store.search(0)

      expect(queryLogs).toHaveBeenCalled()
      // 空 q 会被清空参数逻辑删除
      const callArgs = queryLogs.mock.calls[0][0]
      expect(callArgs).not.toHaveProperty('q')
    })

    it('语法错误（400）时 error 带有"查询语法错误"前缀', async () => {
      queryLogs.mockRejectedValueOnce({ s: 400, j: { error: 'unexpected token at pos 5' } })

      const store = useLogStore()
      store.setMode('advanced')
      store.q = 'level=error AND'

      await store.search(0)

      expect(store.error).toContain('查询语法错误')
      expect(store.error).toContain('unexpected token at pos 5')
    })

    it('语法错误时保留上次的结果列表', async () => {
      queryLogs.mockResolvedValueOnce([{ id: 'l1' }])
      queryLogs.mockRejectedValueOnce({ s: 400, j: { error: 'parse failed' } })

      const store = useLogStore()
      store.setMode('advanced')
      store.q = 'level=error'
      await store.search() // 第一次成功
      expect(store.list).toHaveLength(1)

      store.q = 'level=error AND' // 语法错误
      await store.search() // 第二次失败
      expect(store.list).toHaveLength(1) // 保留上次结果
      expect(store.error).toContain('查询语法错误')
    })

    it('advanced 模式成功时清空 error', async () => {
      queryLogs.mockResolvedValueOnce([])
      const store = useLogStore()
      store.setMode('advanced')
      store.error = '之前的错误'

      await store.search(0)

      expect(store.error).toBe('')
    })
  })

  describe('setMode 动作', () => {
    it('切换到 advanced', () => {
      const store = useLogStore()
      store.setMode('advanced')
      expect(store.mode).toBe('advanced')
    })

    it('切换到 simple', () => {
      const store = useLogStore()
      store.setMode('advanced')
      store.setMode('simple')
      expect(store.mode).toBe('simple')
    })

    it('无效模式不切换', () => {
      const store = useLogStore()
      store.setMode('invalid')
      expect(store.mode).toBe('simple')
    })

    it('切换模式时清空 error', () => {
      const store = useLogStore()
      store.error = '某个错误'
      store.setMode('advanced')
      expect(store.error).toBe('')
    })

    it('切换模式时保留 filters 和 q（便于用户来回切换）', () => {
      const store = useLogStore()
      store.filters.deviceID = 'd1'
      store.q = 'level=error'
      store.setMode('advanced')
      expect(store.filters.deviceID).toBe('d1')
      expect(store.q).toBe('level=error')
      store.setMode('simple')
      expect(store.filters.deviceID).toBe('d1')
      expect(store.q).toBe('level=error')
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

    it('清空 q 和 error', () => {
      const store = useLogStore()
      store.q = 'level=error'
      store.error = '某个错误'

      store.reset()

      expect(store.q).toBe('')
      expect(store.error).toBe('')
    })
  })
})
