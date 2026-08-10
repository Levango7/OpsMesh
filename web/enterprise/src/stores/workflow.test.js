// workflow store 单元测试
// 覆盖：初始状态、fetchList/open/save/run/fetchStatus/schedule 动作及错误处理。
// 说明：源码中方法名为 fetchList/open/save/run/fetchStatus/schedule
//      （任务描述中的 fetchWorkflows/fetchWorkflowDetail/createWorkflow/updateWorkflow/
//        runWorkflow/fetchWorkflowStatus/scheduleWorkflow 为别名语义），
//      本测试按实际导出方法验证。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

// mock @/api/workflow：避免真实网络请求，用 vi.fn 控制返回值
vi.mock('@/api/workflow', () => ({
  getWorkflows: vi.fn(),
  getWorkflow: vi.fn(),
  createWorkflow: vi.fn(),
  updateWorkflow: vi.fn(),
  runWorkflow: vi.fn(),
  getWorkflowStatus: vi.fn(),
  scheduleWorkflow: vi.fn(),
}))

import { useWorkflowStore } from '@/stores/workflow'
import {
  getWorkflows, getWorkflow, createWorkflow, updateWorkflow,
  runWorkflow, getWorkflowStatus, scheduleWorkflow
} from '@/api/workflow'

describe('useWorkflowStore', () => {
  beforeEach(() => {
    // 每个测试前创建新 pinia 实例，隔离状态
    setActivePinia(createPinia())
    // 重置 mock 调用记录
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('list 初始为空数组', () => {
      const store = useWorkflowStore()
      expect(store.list).toEqual([])
    })

    it('current 初始为空白作业流模板', () => {
      const store = useWorkflowStore()
      expect(store.current).toEqual({
        id: 0, name: '', agentID: '', cron: '', dag: [], status: 'draft'
      })
    })

    it('loading 初始为 false', () => {
      const store = useWorkflowStore()
      expect(store.loading).toBe(false)
    })

    it('error 初始为空字符串', () => {
      const store = useWorkflowStore()
      expect(store.error).toBe('')
    })

    it('status 初始为空对象', () => {
      const store = useWorkflowStore()
      expect(store.status).toEqual({})
    })

    it('nodePos 初始为空对象', () => {
      const store = useWorkflowStore()
      expect(store.nodePos).toEqual({})
    })

    it('selectedNode / selectedEdge 初始为 null', () => {
      const store = useWorkflowStore()
      expect(store.selectedNode).toBeNull()
      expect(store.selectedEdge).toBeNull()
    })

    it('msg 初始为空字符串', () => {
      const store = useWorkflowStore()
      expect(store.msg).toBe('')
    })
  })

  describe('fetchList 动作', () => {
    it('成功时设置 list', async () => {
      const mockList = [
        { id: 1, name: 'wf-1', status: 'draft' },
        { id: 2, name: 'wf-2', status: 'running' }
      ]
      getWorkflows.mockResolvedValueOnce(mockList)

      const store = useWorkflowStore()
      await store.fetchList()

      expect(getWorkflows).toHaveBeenCalledWith()
      expect(store.list).toEqual(mockList)
      expect(store.error).toBe('')
    })

    it('API 返回 null 时 list 为空数组', async () => {
      getWorkflows.mockResolvedValueOnce(null)

      const store = useWorkflowStore()
      await store.fetchList()

      expect(store.list).toEqual([])
    })

    it('失败时设置 error', async () => {
      getWorkflows.mockRejectedValueOnce({ j: { error: '权限不足' } })

      const store = useWorkflowStore()
      await store.fetchList()

      expect(store.error).toBe('权限不足')
    })

    it('失败且无 error 字段时使用默认错误消息', async () => {
      getWorkflows.mockRejectedValueOnce({})

      const store = useWorkflowStore()
      await store.fetchList()

      expect(store.error).toBe('作业流列表拉取失败')
    })
  })

  describe('open 动作', () => {
    it('id 为空时调用 reset 并返回', async () => {
      const store = useWorkflowStore()
      // 先污染 current，验证 reset 是否生效
      store.current = { id: 9, name: 'x', agentID: 'a', cron: '* * * * *', dag: [{ id: 'n1' }], status: 'running' }

      await store.open(0)

      expect(getWorkflow).not.toHaveBeenCalled()
      expect(store.current).toEqual({
        id: 0, name: '', agentID: '', cron: '', dag: [], status: 'draft'
      })
      expect(store.nodePos).toEqual({})
      expect(store.selectedNode).toBeNull()
    })

    it('成功时解析 dag JSON 并设置 current', async () => {
      const mockWorkflow = {
        id: 7, name: 'deploy', agentID: 'agent-1', cron: '0 * * * *',
        dag: JSON.stringify([{ id: 'n1', name: 'step1', dependsOn: [] }]),
        status: 'running'
      }
      getWorkflow.mockResolvedValueOnce(mockWorkflow)

      const store = useWorkflowStore()
      await store.open(7)

      expect(getWorkflow).toHaveBeenCalledWith(7)
      expect(store.current.id).toBe(7)
      expect(store.current.name).toBe('deploy')
      expect(store.current.agentID).toBe('agent-1')
      expect(store.current.cron).toBe('0 * * * *')
      expect(store.current.dag).toEqual([{ id: 'n1', name: 'step1', dependsOn: [] }])
      expect(store.current.status).toBe('running')
    })

    it('dag 为空时 current.dag 为空数组', async () => {
      getWorkflow.mockResolvedValueOnce({
        id: 3, name: 'empty', agentID: 'a', status: 'draft'
      })

      const store = useWorkflowStore()
      await store.open(3)

      expect(store.current.dag).toEqual([])
    })

    it('cron 缺失时 current.cron 为空字符串', async () => {
      getWorkflow.mockResolvedValueOnce({
        id: 3, name: 'no-cron', agentID: 'a', dag: '[]', status: 'draft'
      })

      const store = useWorkflowStore()
      await store.open(3)

      expect(store.current.cron).toBe('')
    })

    it('失败时设置 error', async () => {
      getWorkflow.mockRejectedValueOnce({ j: { error: '作业流不存在' } })

      const store = useWorkflowStore()
      await store.open(99)

      expect(store.error).toBe('作业流不存在')
    })

    it('失败且无 error 字段时使用默认错误消息', async () => {
      getWorkflow.mockRejectedValueOnce({})

      const store = useWorkflowStore()
      await store.open(99)

      expect(store.error).toBe('作业流拉取失败')
    })
  })

  describe('save 动作', () => {
    it('current.id 为 0 时调用 createWorkflow', async () => {
      createWorkflow.mockResolvedValueOnce({ s: 201, j: { id: 10, status: 'draft' } })
      getWorkflows.mockResolvedValueOnce([{ id: 10 }])

      const store = useWorkflowStore()
      store.current.name = 'new-wf'
      store.current.agentID = 'agent-1'
      store.current.cron = '0 * * * *'
      store.current.dag = [{ id: 'n1', command: 'uptime', dependsOn: [] }]

      const r = await store.save()

      expect(createWorkflow).toHaveBeenCalledWith({
        name: 'new-wf', agentID: 'agent-1', cron: '0 * * * *',
        dag: JSON.stringify([{ id: 'n1', command: 'uptime', dependsOn: [] }])
      })
      expect(updateWorkflow).not.toHaveBeenCalled()
      expect(store.current.id).toBe(10)
      expect(store.current.status).toBe('draft')
      expect(store.msg).toContain('[201]')
      expect(getWorkflows).toHaveBeenCalled()
      expect(r).toEqual({ s: 201, j: { id: 10, status: 'draft' } })
    })

    it('current.id 存在时调用 updateWorkflow', async () => {
      updateWorkflow.mockResolvedValueOnce({ s: 200, j: { id: 5, status: 'running' } })
      getWorkflows.mockResolvedValueOnce([])

      const store = useWorkflowStore()
      store.current.id = 5
      store.current.name = 'update-wf'
      store.current.agentID = 'agent-2'

      const r = await store.save()

      expect(updateWorkflow).toHaveBeenCalledWith(5, expect.objectContaining({
        name: 'update-wf', agentID: 'agent-2'
      }))
      expect(createWorkflow).not.toHaveBeenCalled()
      expect(store.current.status).toBe('running')
      expect(r).toEqual({ s: 200, j: { id: 5, status: 'running' } })
    })

    it('失败时设置 error 并向上抛出异常', async () => {
      createWorkflow.mockRejectedValueOnce({ j: { error: '名称重复' } })

      const store = useWorkflowStore()
      await expect(store.save()).rejects.toEqual({ j: { error: '名称重复' } })

      expect(store.error).toBe('名称重复')
    })

    it('失败且无 error 字段时使用默认错误消息', async () => {
      createWorkflow.mockRejectedValueOnce(new Error('network'))

      const store = useWorkflowStore()
      await expect(store.save()).rejects.toThrow('network')

      expect(store.error).toBe('保存失败')
    })
  })

  describe('run 动作', () => {
    it('成功时返回结果', async () => {
      runWorkflow.mockResolvedValueOnce({ s: 200, j: { ok: true } })

      const store = useWorkflowStore()
      store.current.id = 8

      const r = await store.run()

      expect(runWorkflow).toHaveBeenCalledWith(8)
      expect(r).toEqual({ s: 200, j: { ok: true } })
    })

    it('失败时设置 error 并向上抛出异常', async () => {
      runWorkflow.mockRejectedValueOnce({ j: { error: '运行被拒绝' } })

      const store = useWorkflowStore()
      await expect(store.run()).rejects.toEqual({ j: { error: '运行被拒绝' } })

      expect(store.error).toBe('运行被拒绝')
    })

    it('失败且无 error 字段时使用默认错误消息', async () => {
      runWorkflow.mockRejectedValueOnce({})

      const store = useWorkflowStore()
      await expect(store.run()).rejects.toEqual({})

      expect(store.error).toBe('运行失败')
    })
  })

  describe('fetchStatus 动作', () => {
    it('成功时设置 status', async () => {
      const mockStatus = { n1: 'done', n2: 'running' }
      getWorkflowStatus.mockResolvedValueOnce(mockStatus)

      const store = useWorkflowStore()
      store.current.id = 12

      await store.fetchStatus()

      expect(getWorkflowStatus).toHaveBeenCalledWith(12)
      expect(store.status).toEqual(mockStatus)
    })

    it('API 返回 null 时 status 为空对象', async () => {
      getWorkflowStatus.mockResolvedValueOnce(null)

      const store = useWorkflowStore()
      await store.fetchStatus()

      expect(store.status).toEqual({})
    })

    it('失败时设置 error', async () => {
      getWorkflowStatus.mockRejectedValueOnce({ j: { error: '状态查询失败' } })

      const store = useWorkflowStore()
      await store.fetchStatus()

      expect(store.error).toBe('状态查询失败')
    })

    it('失败且无 error 字段时使用默认错误消息', async () => {
      getWorkflowStatus.mockRejectedValueOnce({})

      const store = useWorkflowStore()
      await store.fetchStatus()

      expect(store.error).toBe('运行态拉取失败')
    })
  })

  describe('schedule 动作', () => {
    it('成功时返回结果', async () => {
      scheduleWorkflow.mockResolvedValueOnce({ s: 200, j: { ok: true } })

      const store = useWorkflowStore()
      store.current.id = 15

      const r = await store.schedule('0 0 * * *')

      expect(scheduleWorkflow).toHaveBeenCalledWith(15, '0 0 * * *')
      expect(r).toEqual({ s: 200, j: { ok: true } })
    })

    it('失败时设置 error 并向上抛出异常', async () => {
      scheduleWorkflow.mockRejectedValueOnce({ j: { error: 'cron 表达式非法' } })

      const store = useWorkflowStore()
      await expect(store.schedule('invalid')).rejects.toEqual({ j: { error: 'cron 表达式非法' } })

      expect(store.error).toBe('cron 表达式非法')
    })

    it('失败且无 error 字段时使用默认错误消息', async () => {
      scheduleWorkflow.mockRejectedValueOnce({})

      const store = useWorkflowStore()
      await expect(store.schedule('x')).rejects.toEqual({})

      expect(store.error).toBe('定时设置失败')
    })
  })

  describe('reset 动作', () => {
    it('重置 current 与编辑态字段', () => {
      const store = useWorkflowStore()
      store.current = { id: 1, name: 'x', agentID: 'a', cron: 'c', dag: [{ id: 'n1' }], status: 'running' }
      store.nodePos = { n1: { x: 1, y: 1 } }
      store.selectedNode = 'n1'
      store.selectedEdge = { src: 'n1', dst: 'n2' }
      store.status = { n1: 'done' }

      store.reset()

      expect(store.current).toEqual({
        id: 0, name: '', agentID: '', cron: '', dag: [], status: 'draft'
      })
      expect(store.nodePos).toEqual({})
      expect(store.selectedNode).toBeNull()
      expect(store.selectedEdge).toBeNull()
      expect(store.status).toEqual({})
    })
  })
})