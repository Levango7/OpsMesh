// OverviewView 视图单元测试
// 覆盖：
//   - 组件挂载与基本结构（overview-view / title / stats）
//   - 统计卡片渲染（devices/managed/alerts/workflows）
//   - 加载骨架屏（statsLoading）
//   - 运维能力 6 方向渲染
//   - 快速入口渲染
//   - 近期告警列表（空/非空）
//   - onMounted 触发 fetch
//
// mock 策略：
//   - @/i18n：返回键本身
//   - @/stores/device|alert|workflow：返回可控 store 对象
//   - @/composables/useFormatTime：fmtTime 返回原值
//   - @/components/Icon.vue / StatusBadge.vue：stub
//   - router-link：stub 为 a 标签
//   - 全局 $t：通过 global.mocks 注入
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'


const { deviceState, alertState, workflowState, mockFetchDevices, mockFetchAlerts } = vi.hoisted(() => {
  const { reactive } = require('vue')
  return {
    deviceState: reactive({ total: 0, managed: 0, loading: false }),
    alertState: reactive({ list: [], loading: false }),
    workflowState: reactive({ list: [] }),
    mockFetchDevices: vi.fn(),
    mockFetchAlerts: vi.fn(),
  }
})

vi.mock('@/i18n', () => ({
  t: (key) => key,
  currentLang: { value: 'zh' },
  setLang: vi.fn(),
  initLang: vi.fn(),
  default: {},
}))

vi.mock('@/stores/device', () => ({
  useDeviceStore: () => ({
    total: deviceState.total,
    managed: deviceState.managed,
    loading: deviceState.loading,
    fetchDevices: mockFetchDevices,
  }),
}))

vi.mock('@/stores/alert', () => ({
  useAlertStore: () => ({
    list: alertState.list,
    loading: alertState.loading,
    fetchAlerts: mockFetchAlerts,
  }),
}))

vi.mock('@/stores/workflow', () => ({
  useWorkflowStore: () => ({
    list: workflowState.list,
  }),
}))

vi.mock('@/composables/useFormatTime', () => ({
  fmtTime: (v) => v || '',
}))

vi.mock('@/components/Icon.vue', () => ({
  default: {
    name: 'Icon',
    props: ['name', 'size'],
    template: '<span class="icon-mock" />',
  },
}))

vi.mock('@/components/StatusBadge.vue', () => ({
  default: {
    name: 'StatusBadge',
    props: ['status', 'text'],
    template: '<span class="badge-mock" />',
  },
}))

import OverviewView from '@/views/OverviewView.vue'

const mockT = (key) => key

function mountOverview() {
  return mount(OverviewView, {
    global: {
      mocks: { $t: mockT },
      stubs: { 'router-link': { template: '<a class="router-link-stub"><slot /></a>' } },
    },
  })
}

describe('OverviewView 视图', () => {
  beforeEach(() => {
    deviceState.total = 0
    deviceState.managed = 0
    deviceState.loading = false
    alertState.list.splice(0, alertState.list.length)
    alertState.loading = false
    workflowState.list = []
    vi.clearAllMocks()
  })

  describe('组件挂载', () => {
    it('挂载成功，包含 data-testid="overview-view"', () => {
      const wrapper = mountOverview()
      expect(wrapper.find('[data-testid="overview-view"]').exists()).toBe(true)
    })

    it('包含标题 data-testid="overview-title"', () => {
      const wrapper = mountOverview()
      expect(wrapper.find('[data-testid="overview-title"]').exists()).toBe(true)
    })

    it('挂载后调用 fetchDevices 与 fetchAlerts', () => {
      mountOverview()
      expect(mockFetchDevices).toHaveBeenCalledTimes(1)
      expect(mockFetchAlerts).toHaveBeenCalledTimes(1)
    })
  })

  describe('统计卡片渲染', () => {
    it('非加载态显示 data-testid="overview-stats"', () => {
      const wrapper = mountOverview()
      expect(wrapper.find('[data-testid="overview-stats"]').exists()).toBe(true)
    })

    it('包含 4 个 stat-card', () => {
      const wrapper = mountOverview()
      expect(wrapper.findAll('.stat-card')).toHaveLength(4)
    })

    it('设备数渲染到 stat-devices-value', () => {
      deviceState.total = 42
      const wrapper = mountOverview()
      expect(wrapper.find('[data-testid="stat-devices-value"]').text()).toBe('42')
    })

    it('已纳管数渲染到 stat-managed-value', () => {
      deviceState.managed = 30
      const wrapper = mountOverview()
      expect(wrapper.find('[data-testid="stat-managed-value"]').text()).toBe('30')
    })

    it('告警数渲染到 stat-alerts-value', () => {
      alertState.list.push({ id: 'a1' }, { id: 'a2' }, { id: 'a3' })
      const wrapper = mountOverview()
      expect(wrapper.find('[data-testid="stat-alerts-value"]').text()).toBe('3')
    })

    it('工作流数渲染到 stat-workflows-value（数组）', () => {
      workflowState.list = [{ id: 1 }, { id: 2 }]
      const wrapper = mountOverview()
      expect(wrapper.find('[data-testid="stat-workflows-value"]').text()).toBe('2')
    })

    it('工作流数为对象时按 key 数量计算', () => {
      workflowState.list = { a: 1, b: 2, c: 3 }
      const wrapper = mountOverview()
      expect(wrapper.find('[data-testid="stat-workflows-value"]').text()).toBe('3')
    })
  })

  describe('加载骨架屏', () => {
    it('deviceStore.loading 时显示骨架屏', () => {
      deviceState.loading = true
      const wrapper = mountOverview()
      expect(wrapper.find('[data-testid="overview-stats-skeleton"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="overview-stats"]').exists()).toBe(false)
    })

    it('alertStore.loading 时显示骨架屏', () => {
      alertState.loading = true
      const wrapper = mountOverview()
      expect(wrapper.find('[data-testid="overview-stats-skeleton"]').exists()).toBe(true)
    })

    it('两者均不 loading 时显示 stats 网格', () => {
      const wrapper = mountOverview()
      expect(wrapper.find('[data-testid="overview-stats"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="overview-stats-skeleton"]').exists()).toBe(false)
    })
  })

  describe('运维能力 6 方向', () => {
    it('渲染 data-testid="overview-capabilities" 容器', () => {
      const wrapper = mountOverview()
      expect(wrapper.find('[data-testid="overview-capabilities"]').exists()).toBe(true)
    })

    it('渲染 6 个能力卡片', () => {
      const wrapper = mountOverview()
      const caps = wrapper.findAll('[data-testid^="overview-cap-"]')
      expect(caps).toHaveLength(6)
    })

    it('包含 enroll/task/config/service/file/monitor 6 个 key', () => {
      const wrapper = mountOverview()
      for (const key of ['enroll', 'task', 'config', 'service', 'file', 'monitor']) {
        expect(wrapper.find(`[data-testid="overview-cap-${key}"]`).exists()).toBe(true)
      }
    })
  })

  describe('快速入口', () => {
    it('渲染 data-testid="overview-quick-entries" 容器', () => {
      const wrapper = mountOverview()
      expect(wrapper.find('[data-testid="overview-quick-entries"]').exists()).toBe(true)
    })

    it('渲染 8 个快速入口', () => {
      const wrapper = mountOverview()
      const entries = wrapper.findAll('.quick-item')
      expect(entries).toHaveLength(8)
    })
  })

  describe('近期告警', () => {
    it('无告警时显示空提示 data-testid="overview-alerts-empty"', () => {
      const wrapper = mountOverview()
      expect(wrapper.find('[data-testid="overview-alerts-empty"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="overview-alerts-list"]').exists()).toBe(false)
    })

    it('有告警时渲染告警列表', () => {
      alertState.list.push(
        { id: 'a1', severity: 'critical', name: 'CPU 高' },
        { id: 'a2', severity: 'warning', name: '磁盘满' },
      )
      const wrapper = mountOverview()
      expect(wrapper.find('[data-testid="overview-alerts-list"]').exists()).toBe(true)
      expect(wrapper.findAll('.alert-row')).toHaveLength(2)
    })

    it('告警最多渲染 6 条', () => {
      for (let i = 0; i < 10; i++) {
        alertState.list.push({ id: `a${i}`, severity: 'warning', name: `告警${i}` })
      }
      const wrapper = mountOverview()
      expect(wrapper.findAll('.alert-row')).toHaveLength(6)
    })
  })
})