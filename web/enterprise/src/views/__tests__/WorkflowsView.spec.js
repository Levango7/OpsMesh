// WorkflowsView 视图单元测试
// 覆盖：
//   - 组件挂载与基本结构（title / flowbar / canvas）
//   - 工具栏按钮渲染（save/run/schedule/add-step 等）
//   - 工作流选择下拉
//   - DAG 画布节点渲染
//   - 节点编辑器
//   - 消息/错误提示
//   - onMounted 触发 fetchList
//
// mock 策略：
//   - @/i18n：返回键本身
//   - @/stores/workflow：返回可控 store 对象
//   - @/api/device：getAgents 返回空数组
//   - @/components/PromptModal.vue：stub
//   - 全局 $t：通过 global.mocks 注入
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'

const { wfState, mockFetchList, mockOpen, mockSave, mockRun, mockSchedule, mockAddNode, mockAutoLayout, mockReset, mockDeleteNode, mockGetAgents } = vi.hoisted(() => {
  const { reactive } = require('vue')
  return {
    wfState: reactive({
      list: [],
      current: { id: 0, name: '', cron: '', agentID: '', dag: [] },
      selectedNode: null,
      nodePos: {},
      status: {},
      msg: '',
      error: '',
    }),
    mockFetchList: vi.fn(),
    mockOpen: vi.fn(),
    mockSave: vi.fn(),
    mockRun: vi.fn(),
    mockSchedule: vi.fn(),
    mockAddNode: vi.fn(),
    mockAutoLayout: vi.fn(),
    mockReset: vi.fn(),
    mockDeleteNode: vi.fn(),
    mockGetAgents: vi.fn(),
  }
})

vi.mock('@/i18n', () => ({
  t: (key, params) => (params ? key + JSON.stringify(params) : key),
  currentLang: { value: 'zh' },
  setLang: vi.fn(),
  initLang: vi.fn(),
  default: {},
}))

vi.mock('@/stores/workflow', () => ({
  useWorkflowStore: () => ({
    list: wfState.list,
    current: wfState.current,
    selectedNode: wfState.selectedNode,
    nodePos: wfState.nodePos,
    status: wfState.status,
    msg: wfState.msg,
    error: wfState.error,
    fetchList: mockFetchList,
    open: mockOpen,
    save: mockSave,
    run: mockRun,
    schedule: mockSchedule,
    addNode: mockAddNode,
    autoLayout: mockAutoLayout,
    reset: mockReset,
    deleteNode: mockDeleteNode,
  }),
}))

vi.mock('@/api/device', () => ({
  getAgents: mockGetAgents,
}))


import WorkflowsView from '@/views/WorkflowsView.vue'

const mockT = (key, params) => (params ? key + JSON.stringify(params) : key)

function mountWorkflows() {
  return mount(WorkflowsView, {
    global: {
      mocks: { $t: mockT },
      stubs: { PromptModal: true },
    },
  })
}

describe('WorkflowsView 视图', () => {
  beforeEach(() => {
    wfState.list = []
    wfState.current = { id: 0, name: '', cron: '', agentID: '', dag: [] }
    wfState.selectedNode = null
    wfState.nodePos = {}
    wfState.status = {}
    wfState.msg = ''
    wfState.error = ''
    vi.clearAllMocks()
    mockGetAgents.mockResolvedValue([])
  })

  describe('组件挂载', () => {
    it('挂载成功，包含 data-testid="workflows-view"', () => {
      const wrapper = mountWorkflows()
      expect(wrapper.find('[data-testid="workflows-view"]').exists()).toBe(true)
    })

    it('包含标题 data-testid="workflows-title"', () => {
      const wrapper = mountWorkflows()
      expect(wrapper.find('[data-testid="workflows-title"]').exists()).toBe(true)
    })

    it('挂载后调用 fetchList', () => {
      mountWorkflows()
      expect(mockFetchList).toHaveBeenCalledTimes(1)
    })

    it('挂载后调用 getAgents', async () => {
      mountWorkflows()
      await nextTick()
      expect(mockGetAgents).toHaveBeenCalledTimes(1)
    })
  })

  describe('工具栏按钮', () => {
    it('包含保存按钮 data-testid="workflows-save-btn"', () => {
      const wrapper = mountWorkflows()
      expect(wrapper.find('[data-testid="workflows-save-btn"]').exists()).toBe(true)
    })

    it('包含运行按钮 data-testid="workflows-run-btn"', () => {
      const wrapper = mountWorkflows()
      expect(wrapper.find('[data-testid="workflows-run-btn"]').exists()).toBe(true)
    })

    it('包含添加步骤按钮 data-testid="workflows-add-step-btn"', () => {
      const wrapper = mountWorkflows()
      expect(wrapper.find('[data-testid="workflows-add-step-btn"]').exists()).toBe(true)
    })

    it('包含清空按钮 data-testid="workflows-clear-btn"', () => {
      const wrapper = mountWorkflows()
      expect(wrapper.find('[data-testid="workflows-clear-btn"]').exists()).toBe(true)
    })

    it('current.id 为 0 时运行按钮禁用', () => {
      wfState.current.id = 0
      const wrapper = mountWorkflows()
      expect(wrapper.find('[data-testid="workflows-run-btn"]').attributes('disabled')).toBeDefined()
    })

    it('current.id 非 0 时运行按钮可用', () => {
      wfState.current.id = 5
      const wrapper = mountWorkflows()
      expect(wrapper.find('[data-testid="workflows-run-btn"]').attributes('disabled')).toBeFalsy()
    })

    it('点击添加步骤按钮调用 store.addNode', async () => {
      const wrapper = mountWorkflows()
      await wrapper.find('[data-testid="workflows-add-step-btn"]').trigger('click')
      expect(mockAddNode).toHaveBeenCalledTimes(1)
    })

    it('点击清空按钮调用 store.reset', async () => {
      const wrapper = mountWorkflows()
      await wrapper.find('[data-testid="workflows-clear-btn"]').trigger('click')
      expect(mockReset).toHaveBeenCalledTimes(1)
    })
  })

  describe('DAG 画布', () => {
    it('包含画布 data-testid="workflows-canvas"', () => {
      const wrapper = mountWorkflows()
      expect(wrapper.find('[data-testid="workflows-canvas"]').exists()).toBe(true)
    })

    it('dag 有节点时渲染对应节点元素', () => {
      wfState.current.dag = [
        { id: 'n1', name: '拉取', type: 'shell', command: 'docker pull', dependsOn: [] },
        { id: 'n2', name: '启动', type: 'service', command: 'nginx', dependsOn: ['n1'] },
      ]
      const wrapper = mountWorkflows()
      expect(wrapper.find('[data-testid="workflows-node-n1"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="workflows-node-n2"]').exists()).toBe(true)
    })

    it('dag 为空时不渲染节点', () => {
      wfState.current.dag = []
      const wrapper = mountWorkflows()
      expect(wrapper.findAll('[data-testid^="workflows-node-"]')).toHaveLength(0)
    })
  })

  describe('节点编辑器', () => {
    it('selectedNode 为空时不显示编辑器', () => {
      wfState.selectedNode = null
      const wrapper = mountWorkflows()
      expect(wrapper.find('[data-testid="workflows-node-editor"]').exists()).toBe(false)
    })

    it('selectedNode 匹配 dag 节点时显示编辑器', () => {
      wfState.current.dag = [
        { id: 'n1', name: '步骤1', type: 'shell', command: 'ls', dependsOn: [] },
      ]
      wfState.selectedNode = 'n1'
      const wrapper = mountWorkflows()
      expect(wrapper.find('[data-testid="workflows-node-editor"]').exists()).toBe(true)
    })

    it('编辑器包含删除节点按钮', () => {
      wfState.current.dag = [
        { id: 'n1', name: '步骤1', type: 'shell', command: 'ls', dependsOn: [] },
      ]
      wfState.selectedNode = 'n1'
      const wrapper = mountWorkflows()
      expect(wrapper.find('[data-testid="workflows-delete-node-btn"]').exists()).toBe(true)
    })
  })

  describe('消息与错误提示', () => {
    it('store.msg 非空时显示消息', () => {
      wfState.msg = '保存成功'
      const wrapper = mountWorkflows()
      expect(wrapper.find('[data-testid="workflows-msg"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="workflows-msg"]').text()).toBe('保存成功')
    })

    it('store.msg 为空时不显示消息', () => {
      const wrapper = mountWorkflows()
      expect(wrapper.find('[data-testid="workflows-msg"]').exists()).toBe(false)
    })

    it('store.error 非空时显示错误', () => {
      wfState.error = '运行失败'
      const wrapper = mountWorkflows()
      expect(wrapper.find('[data-testid="workflows-error-msg"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="workflows-error-msg"]').text()).toBe('运行失败')
    })
  })

  describe('工作流选择下拉', () => {
    it('包含选择器 data-testid="workflows-select"', () => {
      const wrapper = mountWorkflows()
      expect(wrapper.find('[data-testid="workflows-select"]').exists()).toBe(true)
    })

    it('store.list 有项时渲染对应 option', () => {
      wfState.list = [
        { id: 1, name: 'flow1', status: 'draft' },
        { id: 2, name: 'flow2', status: 'running' },
      ]
      const wrapper = mountWorkflows()
      const select = wrapper.find('[data-testid="workflows-select"]')
      const options = select.findAll('option')
      // 第一个是 "new_blank" 空选项 + 2 个工作流
      expect(options).toHaveLength(3)
    })
  })

  describe('名称输入', () => {
    it('包含名称输入框 data-testid="input-workflow-name"', () => {
      const wrapper = mountWorkflows()
      expect(wrapper.find('[data-testid="input-workflow-name"]').exists()).toBe(true)
    })

    it('包含 cron 输入框 data-testid="input-workflow-cron"', () => {
      const wrapper = mountWorkflows()
      expect(wrapper.find('[data-testid="input-workflow-cron"]').exists()).toBe(true)
    })
  })
})