// DevicesView 视图单元测试
// 覆盖：
//   - 组件挂载与基本结构（title / empty / loading）
//   - 网段分组渲染（seg-block）
//   - 错误提示渲染
//   - onMounted 触发 fetchDevices
//   - 设备详情抽屉（DetailDrawer）
//
// mock 策略：
//   - @/i18n：返回键本身
//   - @/stores/device：返回可控 store 对象
//   - vue-router：useRouter 返回带 push 的 vi.fn
//   - @/composables/useFormatTime：fmtTime 返回原值
//   - @/components/DataTable.vue / StatusBadge.vue / DetailDrawer.vue / Icon.vue：stub
//   - 全局 $t：通过 global.mocks 注入
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'

const { deviceState, mockFetchDevices, mockOpenDevice, mockCloseDrawer, mockProvision, mockPush } = vi.hoisted(() => {
  const { reactive } = require('vue')
  return {
    deviceState: reactive({
      segments: {},
      current: null,
      loading: false,
      error: '',
      total: 0,
    }),
    mockFetchDevices: vi.fn(),
    mockOpenDevice: vi.fn(),
    mockCloseDrawer: vi.fn(),
    mockProvision: vi.fn(),
    mockPush: vi.fn(),
  }
})

vi.mock('@/i18n', () => ({
  t: (key, params) => (params ? key + JSON.stringify(params) : key),
  currentLang: { value: 'zh' },
  setLang: vi.fn(),
  initLang: vi.fn(),
  default: {},
}))

vi.mock('@/stores/device', () => ({
  useDeviceStore: () => ({
    segments: deviceState.segments,
    current: deviceState.current,
    loading: deviceState.loading,
    error: deviceState.error,
    total: deviceState.total,
    fetchDevices: mockFetchDevices,
    openDevice: mockOpenDevice,
    closeDrawer: mockCloseDrawer,
    provision: mockProvision,
  }),
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: mockPush }),
  useRoute: () => ({}),
}))

vi.mock('@/composables/useFormatTime', () => ({
  fmtTime: (v) => v || '',
}))

vi.mock('@/components/Icon.vue', () => ({
  default: { name: 'Icon', props: ['name', 'size'], template: '<span class="icon-mock" />' },
}))

vi.mock('@/components/StatusBadge.vue', () => ({
  default: {
    name: 'StatusBadge',
    props: ['status', 'text'],
    template: '<span class="badge-mock" />',
  },
}))

vi.mock('@/components/DataTable.vue', () => ({
  default: {
    name: 'DataTable',
    props: ['columns', 'rows', 'rowKey', 'clickable', 'rowClass', 'loading', 'emptyText'],
    emits: ['row-click'],
    template: '<div class="datatable-mock"><slot name="cell-actions" :row="rows[0]" /><slot name="cell-state" :value="\'managed\'" /></div>',
  },
}))

vi.mock('@/components/DetailDrawer.vue', () => ({
  default: {
    name: 'DetailDrawer',
    props: ['open', 'title'],
    emits: ['close'],
    template: '<div class="drawer-mock" v-if="open"><slot /></div>',
  },
}))

import DevicesView from '@/views/DevicesView.vue'

const mockT = (key, params) => (params ? key + JSON.stringify(params) : key)

function mountDevices() {
  return mount(DevicesView, { global: { mocks: { $t: mockT } } })
}

function resetSegments() {
  for (const k of Object.keys(deviceState.segments)) delete deviceState.segments[k]
}

describe('DevicesView 视图', () => {
  beforeEach(() => {
    resetSegments()
    deviceState.current = null
    deviceState.loading = false
    deviceState.error = ''
    deviceState.total = 0
    vi.clearAllMocks()
  })

  describe('组件挂载', () => {
    it('挂载成功，包含 data-testid="devices-title"', () => {
      const wrapper = mountDevices()
      expect(wrapper.find('[data-testid="devices-title"]').exists()).toBe(true)
    })

    it('total 为 0 时挂载调用 fetchDevices', () => {
      deviceState.total = 0
      mountDevices()
      expect(mockFetchDevices).toHaveBeenCalledTimes(1)
    })

    it('total 非 0 时不调用 fetchDevices', () => {
      deviceState.total = 5
      mountDevices()
      expect(mockFetchDevices).not.toHaveBeenCalled()
    })
  })

  describe('空状态', () => {
    it('无设备且非 loading 时显示 data-testid="devices-empty"', () => {
      const wrapper = mountDevices()
      expect(wrapper.find('[data-testid="devices-empty"]').exists()).toBe(true)
    })
  })

  describe('加载状态', () => {
    it('loading 且无设备时显示 loading 文本', () => {
      deviceState.loading = true
      const wrapper = mountDevices()
      expect(wrapper.find('[data-testid="devices-empty"]').exists()).toBe(false)
      expect(wrapper.text()).toContain('common.loading')
    })
  })

  describe('错误提示', () => {
    it('store.error 非空时显示 .poll-err', () => {
      deviceState.error = '网络异常'
      const wrapper = mountDevices()
      expect(wrapper.find('.poll-err').exists()).toBe(true)
      expect(wrapper.find('.poll-err').text()).toContain('网络异常')
    })

    it('store.error 为空时不显示 .poll-err', () => {
      const wrapper = mountDevices()
      expect(wrapper.find('.poll-err').exists()).toBe(false)
    })
  })

  describe('网段分组渲染', () => {
    it('单个网段渲染 1 个 .seg-block', () => {
      deviceState.segments['10.0.0.0/24'] = [
        { deviceID: 'd1', hostname: 'h1', state: 'managed' },
      ]
      deviceState.total = 1
      const wrapper = mountDevices()
      expect(wrapper.findAll('.seg-block')).toHaveLength(1)
    })

    it('多个网段渲染多个 .seg-block', () => {
      deviceState.segments['10.0.0.0/24'] = [{ deviceID: 'd1', hostname: 'h1' }]
      deviceState.segments['192.168.0.0/16'] = [
        { deviceID: 'd2', hostname: 'h2' },
        { deviceID: 'd3', hostname: 'h3' },
      ]
      deviceState.total = 3
      const wrapper = mountDevices()
      expect(wrapper.findAll('.seg-block')).toHaveLength(2)
    })

    it('网段标题包含网段名', () => {
      deviceState.segments['10.0.0.0/24'] = [{ deviceID: 'd1', hostname: 'h1' }]
      deviceState.total = 1
      const wrapper = mountDevices()
      const h3 = wrapper.find('.seg-block h3')
      expect(h3.exists()).toBe(true)
      expect(h3.text()).toContain('10.0.0.0/24')
    })
  })

  describe('设备详情抽屉', () => {
    it('store.current 为空时抽屉不渲染内容', () => {
      const wrapper = mountDevices()
      expect(wrapper.find('.drawer-mock').exists()).toBe(false)
    })

    it('store.current 非空时抽屉渲染', () => {
      deviceState.current = {
        device: { deviceID: 'd1', ip: '10.0.0.1', agentID: 'a1', tenantID: 't1', state: 'managed' },
        tasks: [],
        results: [],
      }
      const wrapper = mountDevices()
      expect(wrapper.find('.drawer-mock').exists()).toBe(true)
    })

    it('设备状态为 discovered 时显示纳管按钮', () => {
      deviceState.current = {
        device: { deviceID: 'd1', ip: '10.0.0.1', state: 'discovered' },
        tasks: [],
        results: [],
      }
      const wrapper = mountDevices()
      expect(wrapper.find('[data-testid="device-provision-btn"]').exists()).toBe(true)
    })

    it('设备状态非 discovered 时不显示纳管按钮', () => {
      deviceState.current = {
        device: { deviceID: 'd1', ip: '10.0.0.1', state: 'managed' },
        tasks: [],
        results: [],
      }
      const wrapper = mountDevices()
      expect(wrapper.find('[data-testid="device-provision-btn"]').exists()).toBe(false)
    })
  })
})