// K8sManageView 视图单元测试
// 覆盖：
//   - 组件挂载与基本结构（title / clusters / resources）
//   - 集群添加按钮
//   - 集群选择器
//   - 资源类型 tab
//   - namespace 输入
//   - 错误提示
//   - onMounted 触发 fetchClusters
//   - 添加集群对话框
//
// mock 策略：
//   - @/i18n：返回键本身
//   - @/stores/k8s：返回可控 store 对象
//   - @/utils/toast：stub
//   - @/components/DataTable.vue / StatusBadge.vue / Icon.vue / ConfirmModal.vue：stub
//   - 全局 $t：通过 global.mocks 注入
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'

const { k8sState, mockFetchClusters, mockSetResourceType, mockSelectCluster, mockSetNamespace } = vi.hoisted(() => {
  const { reactive } = require('vue')
  return {
    k8sState: reactive({
      clusters: [],
      loading: false,
      error: '',
      currentClusterID: '',
      resourceType: 'pods',
      resources: [],
      resourcesLoading: false,
    }),
    mockFetchClusters: vi.fn(),
    mockSetResourceType: vi.fn(),
    mockSelectCluster: vi.fn(),
    mockSetNamespace: vi.fn(),
  }
})

vi.mock('@/i18n', () => ({
  t: (key, params) => (params ? key + JSON.stringify(params) : key),
  currentLang: { value: 'zh' },
  setLang: vi.fn(),
  initLang: vi.fn(),
  default: {},
}))

vi.mock('@/stores/k8s', () => ({
  useK8sStore: () => ({
    clusters: k8sState.clusters,
    loading: k8sState.loading,
    error: k8sState.error,
    currentClusterID: k8sState.currentClusterID,
    resourceType: k8sState.resourceType,
    resources: k8sState.resources,
    resourcesLoading: k8sState.resourcesLoading,
    fetchClusters: mockFetchClusters,
    setResourceType: mockSetResourceType,
    selectCluster: mockSelectCluster,
    setNamespace: mockSetNamespace,
  }),
}))

vi.mock('@/utils/toast', () => ({
  toast: { error: vi.fn(), warn: vi.fn(), success: vi.fn() },
  default: {},
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
    props: ['columns', 'rows', 'rowKey', 'emptyText', 'loading'],
    template: '<div class="datatable-mock" />',
  },
}))

vi.mock('@/components/ConfirmModal.vue', () => ({
  default: {
    name: 'ConfirmModal',
    props: ['open', 'title', 'message', 'confirmText', 'danger'],
    emits: ['confirm', 'cancel'],
    template: '<div v-if="open" class="confirm-modal-mock" />',
  },
}))

import K8sManageView from '@/views/K8sManageView.vue'

const mockT = (key, params) => (params ? key + JSON.stringify(params) : key)

function mountK8s() {
  return mount(K8sManageView, { global: { mocks: { $t: mockT } } })
}

describe('K8sManageView 视图', () => {
  beforeEach(() => {
    k8sState.clusters = []
    k8sState.loading = false
    k8sState.error = ''
    k8sState.currentClusterID = ''
    k8sState.resourceType = 'pods'
    k8sState.resources = []
    k8sState.resourcesLoading = false
    vi.clearAllMocks()
  })

  describe('组件挂载', () => {
    it('挂载成功，包含 data-testid="k8s-title"', () => {
      const wrapper = mountK8s()
      expect(wrapper.find('[data-testid="k8s-title"]').exists()).toBe(true)
    })

    it('挂载后调用 fetchClusters', () => {
      mountK8s()
      expect(mockFetchClusters).toHaveBeenCalledTimes(1)
    })
  })

  describe('集群管理', () => {
    it('包含添加集群按钮 data-testid="k8s-add-cluster-btn"', () => {
      const wrapper = mountK8s()
      expect(wrapper.find('[data-testid="k8s-add-cluster-btn"]').exists()).toBe(true)
    })

    it('点击添加集群按钮打开对话框', async () => {
      const wrapper = mountK8s()
      expect(wrapper.find('[data-testid="k8s-add-modal"]').exists()).toBe(false)
      await wrapper.find('[data-testid="k8s-add-cluster-btn"]').trigger('click')
      expect(wrapper.find('[data-testid="k8s-add-modal"]').exists()).toBe(true)
    })
  })

  describe('集群选择器', () => {
    it('包含集群选择器 data-testid="k8s-cluster-select"', () => {
      const wrapper = mountK8s()
      expect(wrapper.find('[data-testid="k8s-cluster-select"]').exists()).toBe(true)
    })

    it('无集群时选择器只有占位 option', () => {
      const wrapper = mountK8s()
      const options = wrapper.find('[data-testid="k8s-cluster-select"]').findAll('option')
      expect(options).toHaveLength(1)
    })

    it('有集群时选择器渲染对应 option', () => {
      k8sState.clusters = [
        { id: 'c1', name: 'prod', server: 'https://k8s:6443' },
        { id: 'c2', name: 'dev', server: 'https://dev:6443' },
      ]
      const wrapper = mountK8s()
      const options = wrapper.find('[data-testid="k8s-cluster-select"]').findAll('option')
      // 1 占位 + 2 集群
      expect(options).toHaveLength(3)
    })
  })

  describe('资源类型 tab', () => {
    it('渲染 6 个资源类型 tab', () => {
      const wrapper = mountK8s()
      const tabs = wrapper.findAll('[data-testid^="k8s-tab-"]')
      expect(tabs).toHaveLength(6)
    })

    it('包含 pods/deployments/services/configmaps/secrets/nodes tab', () => {
      const wrapper = mountK8s()
      for (const rt of ['pods', 'deployments', 'services', 'configmaps', 'secrets', 'nodes']) {
        expect(wrapper.find(`[data-testid="k8s-tab-${rt}"]`).exists()).toBe(true)
      }
    })

    it('点击 tab 调用 store.setResourceType', async () => {
      const wrapper = mountK8s()
      await wrapper.find('[data-testid="k8s-tab-deployments"]').trigger('click')
      expect(mockSetResourceType).toHaveBeenCalledWith('deployments')
    })

    it('当前 resourceType 对应 tab 有 active class', () => {
      k8sState.resourceType = 'pods'
      const wrapper = mountK8s()
      const podsTab = wrapper.find('[data-testid="k8s-tab-pods"]')
      expect(podsTab.classes()).toContain('active')
    })
  })

  describe('namespace 输入', () => {
    it('包含 namespace 输入框 data-testid="k8s-namespace-input"', () => {
      const wrapper = mountK8s()
      expect(wrapper.find('[data-testid="k8s-namespace-input"]').exists()).toBe(true)
    })
  })

  describe('错误提示', () => {
    it('store.error 非空时显示 .poll-err', () => {
      k8sState.error = '连接失败'
      const wrapper = mountK8s()
      expect(wrapper.find('.poll-err').exists()).toBe(true)
      expect(wrapper.find('.poll-err').text()).toContain('连接失败')
    })

    it('store.error 为空时不显示 .poll-err', () => {
      const wrapper = mountK8s()
      expect(wrapper.find('.poll-err').exists()).toBe(false)
    })
  })

  describe('资源区域', () => {
    it('未选择集群时显示 noClusterSelected 提示', () => {
      k8sState.currentClusterID = ''
      const wrapper = mountK8s()
      expect(wrapper.text()).toContain('k8s.noClusterSelected')
    })

    it('选择集群且非加载时渲染资源表格', () => {
      k8sState.currentClusterID = 'c1'
      k8sState.resourcesLoading = false
      const wrapper = mountK8s()
      expect(wrapper.find('.datatable-mock').exists()).toBe(true)
    })

    it('资源加载中且无资源时显示 loading 文本', () => {
      k8sState.currentClusterID = 'c1'
      k8sState.resourcesLoading = true
      k8sState.resources = []
      const wrapper = mountK8s()
      expect(wrapper.text()).toContain('common.loading')
    })
  })

  describe('添加集群对话框', () => {
    it('打开后包含名称输入框 data-testid="k8s-add-name"', async () => {
      const wrapper = mountK8s()
      await wrapper.find('[data-testid="k8s-add-cluster-btn"]').trigger('click')
      expect(wrapper.find('[data-testid="k8s-add-name"]').exists()).toBe(true)
    })

    it('打开后包含 server 输入框 data-testid="k8s-add-server"', async () => {
      const wrapper = mountK8s()
      await wrapper.find('[data-testid="k8s-add-cluster-btn"]').trigger('click')
      expect(wrapper.find('[data-testid="k8s-add-server"]').exists()).toBe(true)
    })

    it('打开后包含 kubeconfig 输入框 data-testid="k8s-add-kubeconfig"', async () => {
      const wrapper = mountK8s()
      await wrapper.find('[data-testid="k8s-add-cluster-btn"]').trigger('click')
      expect(wrapper.find('[data-testid="k8s-add-kubeconfig"]').exists()).toBe(true)
    })

    it('打开后包含确认按钮 data-testid="k8s-add-confirm"', async () => {
      const wrapper = mountK8s()
      await wrapper.find('[data-testid="k8s-add-cluster-btn"]').trigger('click')
      expect(wrapper.find('[data-testid="k8s-add-confirm"]').exists()).toBe(true)
    })
  })
})