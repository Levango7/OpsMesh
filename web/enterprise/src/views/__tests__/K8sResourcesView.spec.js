// K8sResourcesView 视图单元测试
// 覆盖：
//   - 组件挂载与基本结构（title / 资源表格）
//   - onMounted 触发 fetchClusters（及已有集群时 selectCluster）
//   - 错误提示
//   - 集群选择器
//   - 资源类型 tab
//   - namespace 输入
//   - 资源区域（未选集群 / 加载中 / 已渲染表格）
//
// mock 策略：
//   - @/i18n：返回键本身
//   - @/stores/k8s：返回可控 store 对象
//   - vue-router：useRoute 返回带 path 的对象，RouterLink 为 stub
//   - @/utils/toast：stub
//   - @/components/DataTable.vue / StatusBadge.vue / Icon.vue / ConfirmModal.vue：stub
//   - 全局 $t：通过 global.mocks 注入
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const { k8sState, mockFetchClusters, mockSetResourceType, mockSelectCluster, mockSetNamespace, mockFetchResources } = vi.hoisted(() => {
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
    mockFetchResources: vi.fn(),
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
    fetchResources: mockFetchResources,
    setResourceType: mockSetResourceType,
    selectCluster: mockSelectCluster,
    setNamespace: mockSetNamespace,
  }),
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
  useRoute: () => ({ path: '/k8s/resources' }),
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
    template: '<div class="datatable-mock"><slot name="cell-actions" :row="rows[0] || {}" /></div>',
  },
}))

vi.mock('@/components/ConfirmModal.vue', () => ({
  default: {
    name: 'ConfirmModal',
    props: ['modelValue', 'title', 'message', 'confirmText', 'info'],
    emits: ['update:modelValue', 'confirm', 'cancel'],
    template: '<div v-if="modelValue" class="confirm-modal-mock" />',
  },
}))

import K8sResourcesView from '@/views/K8sResourcesView.vue'

const mockT = (key, params) => (params ? key + JSON.stringify(params) : key)

const RouterLinkStub = {
  name: 'RouterLink',
  props: ['to'],
  template: '<a :href="to"><slot /></a>',
}

function mountK8s() {
  return mount(K8sResourcesView, {
    global: { mocks: { $t: mockT }, components: { RouterLink: RouterLinkStub } },
  })
}

describe('K8sResourcesView 视图', () => {
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
    it('挂载成功，包含 data-testid="k8s-resources-title"', () => {
      const wrapper = mountK8s()
      expect(wrapper.find('[data-testid="k8s-resources-title"]').exists()).toBe(true)
    })

    it('挂载后调用 fetchClusters', async () => {
      mountK8s()
      await flushPromises()
      expect(mockFetchClusters).toHaveBeenCalledTimes(1)
    })

    it('已有当前集群时挂载调用 selectCluster', async () => {
      k8sState.currentClusterID = 'c1'
      mountK8s()
      await flushPromises()
      expect(mockSelectCluster).toHaveBeenCalledWith('c1')
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
    it('渲染 35 个资源类型 tab', () => {
      const wrapper = mountK8s()
      const tabs = wrapper.findAll('[data-testid^="k8s-tab-"]')
      expect(tabs).toHaveLength(35)
    })

    it('包含 pods/deployments/services/configmaps/secrets/nodes tab', () => {
      const wrapper = mountK8s()
      for (const rt of ['pods', 'deployments', 'services', 'configmaps', 'secrets', 'nodes']) {
        expect(wrapper.find(`[data-testid="k8s-tab-${rt}"]`).exists()).toBe(true)
      }
    })

    it('包含新增资源类型 tab（networkpolicies/endpointslices/csrs 等）', () => {
      const wrapper = mountK8s()
      for (const rt of [
        'networkpolicies', 'ingressclasses', 'storageclasses', 'pdbs', 'serviceaccounts',
        'endpointslices', 'resourcequotas', 'limitranges', 'priorityclasses', 'runtimeclasses',
        'leases', 'events', 'mutatingwebhooks', 'validatingwebhooks', 'csrs'
      ]) {
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

    it('回车触发 store.setNamespace', async () => {
      const wrapper = mountK8s()
      const input = wrapper.find('[data-testid="k8s-namespace-input"]')
      await input.setValue('  kube-system  ')
      await input.trigger('keyup.enter')
      expect(mockSetNamespace).toHaveBeenCalledWith('kube-system')
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
})
