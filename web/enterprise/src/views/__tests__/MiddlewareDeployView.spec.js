// MiddlewareDeployView 视图单元测试
// 覆盖：
//   - 单面板 + 分段开关（模板目录 / 已部署实例）切换
//   - 分类筛选仅在模板目录视图可见
//   - 模板表版本列：默认版本 + 「+N」徽标（tooltip 列出其它版本）
//   - 部署对话框：版本下拉（默认版本选中）、切换版本后参数表单按版本参数重渲染
//   - 确认部署时请求体携带选中的版本
//   - 模板详情抽屉「可用版本」区块（默认版本高亮）
//
// mock 策略：
//   - @/i18n：t 返回键本身，currentLang 为 zh
//   - @/stores/middleware：返回可控 store 对象
//   - @/api/middleware：getMiddlewareTemplate 用 vi.fn 控制模板详情
//   - @/api/task：getTaskDetail stub
//   - 全局 $t：通过 global.mocks 注入
// DataTable / StatusBadge / DetailDrawer 使用真实组件（需要断言表格单元格与插槽内容）。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const { mwState, mockFetchTemplates, mockFetchInstances, mockFetchDetail, mockFetchDevices, mockDeploy, mockUninstall, mockGetTemplate } = vi.hoisted(() => {
  const { reactive } = require('vue')
  return {
    mwState: reactive({
      templates: [],
      instances: [],
      current: null,
      devices: [],
      category: '',
      loading: false,
      instancesLoading: false,
      error: '',
    }),
    mockFetchTemplates: vi.fn(),
    mockFetchInstances: vi.fn(),
    mockFetchDetail: vi.fn(),
    mockFetchDevices: vi.fn(),
    mockDeploy: vi.fn(),
    mockUninstall: vi.fn(),
    mockGetTemplate: vi.fn(),
  }
})

vi.mock('@/i18n', () => ({
  t: (key, params) => (params ? key + JSON.stringify(params) : key),
  currentLang: { value: 'zh' },
  setLang: vi.fn(),
  initLang: vi.fn(),
  default: {},
}))

vi.mock('@/stores/middleware', () => ({
  useMiddlewareStore: () => ({
    templates: mwState.templates,
    instances: mwState.instances,
    current: mwState.current,
    devices: mwState.devices,
    category: mwState.category,
    loading: mwState.loading,
    instancesLoading: mwState.instancesLoading,
    error: mwState.error,
    fetchTemplates: mockFetchTemplates,
    fetchInstances: mockFetchInstances,
    fetchDetail: mockFetchDetail,
    fetchDevices: mockFetchDevices,
    deploy: mockDeploy,
    uninstall: mockUninstall,
    setCategory: vi.fn(),
    clearCurrent: vi.fn(),
  }),
}))

vi.mock('@/api/middleware', () => ({
  getMiddlewareTemplate: mockGetTemplate,
}))

vi.mock('@/api/task', () => ({
  getTaskDetail: vi.fn(),
}))

import MiddlewareDeployView from '@/views/MiddlewareDeployView.vue'

const mockT = (key, params) => (params ? key + JSON.stringify(params) : key)

// 带多版本的模板：默认 8.0，5.7 额外带一个版本专属参数 charset
const tplWithVersions = {
  id: 'mysql',
  name: 'MySQL',
  category: 'database',
  version: '8.0',
  deployTypes: ['docker', 'systemd'],
  risk: 'medium',
  params: [
    { name: 'port', type: 'int', default: '3306', required: true, description: '监听端口' }
  ],
  versions: [
    { version: '8.0', label: 'GA', default: true },
    {
      version: '5.7',
      label: 'LTS',
      params: [
        { name: 'port', type: 'int', default: '3307', required: true },
        { name: 'charset', type: 'string', default: 'utf8mb4' }
      ]
    },
    { version: '8.4', label: 'Latest' }
  ]
}

function mountView() {
  return mount(MiddlewareDeployView, {
    global: { mocks: { $t: mockT } },
  })
}

// 表格表头文案列表（用于断言当前渲染的是哪张表）
function headers(wrapper) {
  return wrapper.findAll('th').map((th) => th.text())
}

// 找到模板表行内的「部署」按钮
function findDeployButton(wrapper) {
  return wrapper.findAll('button').find((b) => b.text() === 'mwdep.deploy')
}

describe('MiddlewareDeployView 视图', () => {
  beforeEach(() => {
    mwState.templates = []
    mwState.instances = []
    mwState.current = null
    mwState.devices = [{ agentID: 'agent-1', hostname: 'host-1' }]
    mwState.category = ''
    mwState.loading = false
    mwState.instancesLoading = false
    mwState.error = ''
    vi.clearAllMocks()
    mockGetTemplate.mockResolvedValue(tplWithVersions)
    mockDeploy.mockResolvedValue({ s: 200, j: { taskID: 'task-1' } })
  })

  describe('视图切换（分段开关）', () => {
    it('默认渲染模板目录视图，含分段开关两个选项', () => {
      const w = mountView()
      expect(w.find('[data-testid="mwdep-view-templates"]').exists()).toBe(true)
      expect(w.find('[data-testid="mwdep-view-instances"]').exists()).toBe(true)
      expect(w.find('[data-testid="mwdep-view-templates"]').classes()).toContain('is-active')
    })

    it('模板目录视图渲染模板表，不渲染实例表', () => {
      mwState.templates = [tplWithVersions]
      const w = mountView()
      const hs = headers(w)
      expect(hs).toContain('mwdep.col.name')
      expect(hs).not.toContain('mwdep.instance.col.template')
    })

    it('切到已部署实例视图后渲染实例表并出现刷新按钮', async () => {
      mwState.instances = [{ id: 'i1', templateID: 'mysql', agentID: 'agent-1', deployType: 'docker', version: '8.0', status: 'running' }]
      const w = mountView()
      await w.find('[data-testid="mwdep-view-instances"]').trigger('click')
      await flushPromises()
      const hs = headers(w)
      expect(hs).toContain('mwdep.instance.col.template')
      expect(hs).not.toContain('mwdep.col.name')
      expect(w.find('[data-testid="mwdep-instances-refresh"]').exists()).toBe(true)
    })

    it('切到已部署实例视图会刷新实例列表', async () => {
      const w = mountView()
      mockFetchInstances.mockClear()
      await w.find('[data-testid="mwdep-view-instances"]').trigger('click')
      expect(mockFetchInstances).toHaveBeenCalled()
    })

    it('模板目录视图不显示刷新按钮', () => {
      const w = mountView()
      expect(w.find('[data-testid="mwdep-instances-refresh"]').exists()).toBe(false)
    })
  })

  describe('分类筛选', () => {
    it('模板目录视图显示分类筛选栏', () => {
      const w = mountView()
      expect(w.find('.cat-filter').exists()).toBe(true)
    })

    it('已部署实例视图隐藏分类筛选栏', async () => {
      const w = mountView()
      await w.find('[data-testid="mwdep-view-instances"]').trigger('click')
      expect(w.find('.cat-filter').exists()).toBe(false)
    })

    it('分类集合覆盖后端预置模板的全部 category', () => {
      const w = mountView()
      const labels = w.findAll('.cat-btn').map((b) => b.text())
      const expected = ['all', 'database', 'cache', 'message', 'web', 'search', 'storage', 'service', 'monitor', 'gpu']
      expect(labels).toEqual(expected.map((c) => 'mwdep.category.' + c))
    })
  })

  describe('模板表版本列', () => {
    beforeEach(() => {
      mwState.templates = [tplWithVersions]
    })

    it('显示默认版本', () => {
      const w = mountView()
      expect(w.find('.ver-cell code').text()).toBe('8.0')
    })

    it('多版本时追加 +N 徽标', () => {
      const w = mountView()
      expect(w.find('[data-testid="mwdep-more-versions"]').text()).toBe('+2')
    })

    it('徽标 tooltip 列出其它版本', () => {
      const w = mountView()
      const title = w.find('[data-testid="mwdep-more-versions"]').attributes('title')
      expect(title).toContain('5.7')
      expect(title).toContain('8.4')
    })

    it('单版本模板（无 versions 数组）回退到顶层 version 且无徽标', () => {
      mwState.templates = [{ id: 'redis', name: 'Redis', category: 'cache', version: '7.0' }]
      const w = mountView()
      expect(w.find('.ver-cell code').text()).toBe('7.0')
      expect(w.find('[data-testid="mwdep-more-versions"]').exists()).toBe(false)
    })
  })

  describe('部署对话框（版本选择）', () => {
    beforeEach(() => {
      mwState.templates = [tplWithVersions]
    })

    async function openDeployDialog() {
      const w = mountView()
      await findDeployButton(w).trigger('click')
      await flushPromises()
      return w
    }

    it('打开后渲染版本下拉，选项来自模板 versions', async () => {
      const w = await openDeployDialog()
      const sel = w.find('[data-testid="mwdep-version-select"]')
      expect(sel.exists()).toBe(true)
      const opts = sel.findAll('option')
      expect(opts.map((o) => o.element.value)).toEqual(['8.0', '5.7', '8.4'])
    })

    it('默认选中 default:true 的版本', async () => {
      const w = await openDeployDialog()
      expect(w.find('[data-testid="mwdep-version-select"]').element.value).toBe('8.0')
    })

    it('参数表单按默认版本的合并参数渲染', async () => {
      const w = await openDeployDialog()
      const inputs = w.findAll('.params-form input')
      expect(inputs.length).toBe(1)
      expect(inputs[0].element.value).toBe('3306')
    })

    it('切换版本后参数表单按该版本参数重渲染', async () => {
      const w = await openDeployDialog()
      await w.find('[data-testid="mwdep-version-select"]').setValue('5.7')
      await flushPromises()
      const inputs = w.findAll('.params-form input')
      // 5.7 版本新增了 charset 参数，并覆盖了 port 默认值
      expect(inputs.length).toBe(2)
      expect(inputs[0].element.value).toBe('3307')
      expect(inputs[1].element.value).toBe('utf8mb4')
    })

    it('确认部署时把选中版本传给 store.deploy', async () => {
      const w = await openDeployDialog()
      await w.find('[data-testid="mwdep-version-select"]').setValue('5.7')
      await flushPromises()
      const selects = w.findAll('.deploy-fields select')
      await selects[2].setValue('agent-1')
      const btns = w.findAll('.btnbar button')
      await btns[0].trigger('click')
      await flushPromises()
      expect(mockDeploy).toHaveBeenCalledWith(
        'mysql',
        'agent-1',
        'docker',
        expect.objectContaining({ port: '3307', charset: 'utf8mb4' }),
        '5.7'
      )
    })
  })

  describe('模板详情抽屉', () => {
    it('渲染可用版本区块并高亮默认版本', () => {
      mwState.current = tplWithVersions
      const w = mountView()
      const section = w.find('.versions-section')
      expect(section.exists()).toBe(true)
      expect(section.text()).toContain('8.0')
      expect(section.text()).toContain('5.7')
      expect(section.text()).toContain('8.4')
      const defaults = section.findAll('.version-item.is-default')
      expect(defaults.length).toBe(1)
      expect(defaults[0].text()).toContain('8.0')
    })
  })
})
