// task store 单元测试
// 覆盖：初始状态、fetchTasks/create/cancel 动作。
// 说明：源码中方法名为 create/cancel（任务描述中的 createTask/cancelTask 为别名语义），
//      本测试按实际导出方法验证。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

// mock @/api/task：避免真实网络请求，用 vi.fn 控制返回值
vi.mock('@/api/task', () => ({
  getTasks: vi.fn(),
  createTask: vi.fn(),
  cancelTask: vi.fn(),
  getTaskDetail: vi.fn(),
}))

import { useTaskStore } from '@/stores/task'
import { getTasks, createTask, cancelTask } from '@/api/task'

describe('useTaskStore', () => {
  beforeEach(() => {
    // 每个测试前创建新 pinia 实例，隔离状态
    setActivePinia(createPinia())
    // 重置 mock 调用记录
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('list 初始为空数组', () => {
      const store = useTaskStore()
      expect(store.list).toEqual([])
    })

    it('statusFilter 初始为空字符串', () => {
      const store = useTaskStore()
      expect(store.statusFilter).toBe('')
    })

    it('loading 初始为 false', () => {
      const store = useTaskStore()
      expect(store.loading).toBe(false)
    })

    it('error 初始为空字符串', () => {
      const store = useTaskStore()
      expect(store.error).toBe('')
    })
  })

  describe('fetchTasks 动作', () => {
    it('成功时设置 list', async () => {
      const mockTasks = [{ taskID: 't1', status: 'pending' }, { taskID: 't2', status: 'done' }]
      getTasks.mockResolvedValueOnce(mockTasks)

      const store = useTaskStore()
      await store.fetchTasks()

      expect(getTasks).toHaveBeenCalledWith('')
      expect(store.list).toEqual(mockTasks)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('带 statusFilter 时传给 API', async () => {
      getTasks.mockResolvedValueOnce([])
      const store = useTaskStore()
      store.statusFilter = 'running'

      await store.fetchTasks()

      expect(getTasks).toHaveBeenCalledWith('running')
    })

    it('API 返回 null 时 list 为空数组', async () => {
      getTasks.mockResolvedValueOnce(null)

      const store = useTaskStore()
      await store.fetchTasks()

      expect(store.list).toEqual([])
    })

    it('失败时设置 error 并结束 loading', async () => {
      getTasks.mockRejectedValueOnce({ j: { error: '权限不足' } })

      const store = useTaskStore()
      await store.fetchTasks()

      expect(store.error).toBe('权限不足')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getTasks.mockRejectedValueOnce({})

      const store = useTaskStore()
      await store.fetchTasks()

      expect(store.error).toBe('任务列表拉取失败')
    })
  })

  describe('create 动作', () => {
    it('成功时返回结果并刷新列表', async () => {
      const body = { agentID: 'a1', type: 'shell', command: 'uptime' }
      createTask.mockResolvedValueOnce({ s: 200, j: { taskID: 't1' } })
      getTasks.mockResolvedValueOnce([{ taskID: 't1' }])

      const store = useTaskStore()
      const r = await store.create(body)

      expect(createTask).toHaveBeenCalledWith(body)
      expect(getTasks).toHaveBeenCalled()
      expect(r).toEqual({ s: 200, j: { taskID: 't1' } })
    })

    it('API 抛错时向上抛出异常', async () => {
      createTask.mockRejectedValueOnce(new Error('create failed'))

      const store = useTaskStore()
      await expect(store.create({})).rejects.toThrow('create failed')
    })
  })

  describe('cancel 动作', () => {
    it('成功时返回结果并刷新列表', async () => {
      cancelTask.mockResolvedValueOnce({ s: 200, j: { ok: true } })
      getTasks.mockResolvedValueOnce([])

      const store = useTaskStore()
      const r = await store.cancel('t1')

      expect(cancelTask).toHaveBeenCalledWith('t1')
      expect(getTasks).toHaveBeenCalled()
      expect(r).toEqual({ s: 200, j: { ok: true } })
    })

    it('API 抛错时向上抛出异常', async () => {
      cancelTask.mockRejectedValueOnce(new Error('cancel failed'))

      const store = useTaskStore()
      await expect(store.cancel('t1')).rejects.toThrow('cancel failed')
    })
  })
})