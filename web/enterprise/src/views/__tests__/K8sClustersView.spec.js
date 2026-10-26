// K8sClustersView 视图单元测试
// 覆盖：
//   - 组件挂载与基本结构（title / 集群表格）
//   - onMounted 触发 fetchClusters
//   - 错误提示
//   - 集群行操作（测试连接 / 管理资源 / 删除）
//   - 删除确认弹窗
//   - 添加集群对话框（名称 / server / kubeconfig / 确认）
//   - 跳转资源视图
//
// mock 策略：
//   - @/i18n：返回键本身
//   - @/stores/k8s：返回可控 store 对象
//   - vue-router：useRouter 返回带 push 的 vi.fn，RouterLink 为 stub
//   - @/utils/toast：stub
//   - @/components/DataTable.vue / StatusBadge.vue / Icon.vue / ConfirmModal.vue：stub
//   - 全局 $t：通过 global.mocks 注入
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const { k8sState, mockFetchClusters, mockCreateCluster, mockRemoveCluster, mockTestCluster, mockSelectCluster, mockPush } = vi.hoisted(() => {
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
    mockCreateCluster: vi.fn(),
    mockRemoveCluster: vi.fn(),
    mockTestCluster: vi.fn(),
    mockSelectCluster: vi.fn(),
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
    createCluster: mockCreateCluster,
    removeCluster: mockRemoveCluster,
    testCluster: mockTestCluster,
    selectCluster: mockSelectCluster,
    setResourceType: vi.fn(),
    setNamespace: vi.fn(),
  }),
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: mockPush }),
  useRoute: () => ({ path: '/k8s/clusters' }),
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

import K8sClustersView from '@/views/K8sClustersView.vue'

const mockT = (key, params) => (params ? key + JSON.stringify(params) : key)

const RouterLinkStub = {
  name: 'RouterLink',
  props: ['to'],
  template: '<a :href="to"><slot /></a>',
}

function mountK8s() {
  return mount(K8sClustersView, {
    global: { mocks: { $t: mockT }, components: { RouterLink: RouterLinkStub } },
  })
}

describe('K8sClustersView 视图', () => {
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

    it('挂载后调用 fetchClusters', async () => {
      mountK8s()
      await flushPromises()
      expect(mockFetchClusters).toHaveBeenCalledTimes(1)
    })
  })

  describe('集群列表', () => {
    it('渲染集群表格并把 clusters 作为 rows 传入', () => {
      k8sState.clusters = [{ id: 'c1', name: 'prod', server: 'https://k8s:6443' }]
      const wrapper = mountK8s()
      const table = wrapper.findComponent({ name: 'DataTable' })
      expect(table.exists()).toBe(true)
      expect(table.props('rows')).toHaveLength(1)
    })

    it('无集群时渲染空表格', () => {
      const wrapper = mountK8s()
      const table = wrapper.findComponent({ name: 'DataTable' })
      expect(table.exists()).toBe(true)
      expect(table.props('rows')).toHaveLength(0)
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

  describe('集群行操作', () => {
    beforeEach(() => {
      k8sState.clusters = [{ id: 'c1', name: 'prod', server: 'https://k8s:6443' }]
    })

    it('渲染测试 / 管理 / 删除按钮', () => {
      const wrapper = mountK8s()
      expect(wrapper.find('[data-testid="k8s-test-btn"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="k8s-manage-btn"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="k8s-delete-cluster-btn"]').exists()).toBe(true)
    })

    it('点击测试按钮调用 store.testCluster', async () => {
      mockTestCluster.mockResolvedValue({ s: 200, j: { status: 'ok' } })
      const wrapper = mountK8s()
      await wrapper.find('[data-testid="k8s-test-btn"]').trigger('click')
      expect(mockTestCluster).toHaveBeenCalledWith('c1')
    })

    it('点击管理按钮先 selectCluster 再跳转资源页', async () => {
      mockSelectCluster.mockResolvedValue(undefined)
      const wrapper = mountK8s()
      await wrapper.find('[data-testid="k8s-manage-btn"]').trigger('click')
      expect(mockSelectCluster).toHaveBeenCalledWith('c1')
      expect(mockPush).toHaveBeenCalledWith('/k8s/resources')
    })

    it('点击删除按钮打开删除确认弹窗', async () => {
      const wrapper = mountK8s()
      await wrapper.find('[data-testid="k8s-delete-cluster-btn"]').trigger('click')
      expect(wrapper.findComponent({ name: 'ConfirmModal' }).props('modelValue')).toBe(true)
    })
  })

  describe('添加集群对话框', () => {
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

    it('表单不完整时不调用 createCluster', async () => {
      const wrapper = mountK8s()
      await wrapper.find('[data-testid="k8s-add-cluster-btn"]').trigger('click')
      await wrapper.find('[data-testid="k8s-add-confirm"]').trigger('click')
      expect(mockCreateCluster).not.toHaveBeenCalled()
    })

    it('表单完整时调用 store.createCluster', async () => {
      mockCreateCluster.mockResolvedValue({ s: 200, j: { id: 'c2' } })
      const wrapper = mountK8s()
      await wrapper.find('[data-testid="k8s-add-cluster-btn"]').trigger('click')
      await wrapper.find('[data-testid="k8s-add-name"]').setValue('dev')
      await wrapper.find('[data-testid="k8s-add-server"]').setValue('https://dev:6443')
      await wrapper.find('[data-testid="k8s-add-kubeconfig"]').setValue('kubeconfig-body')
      await wrapper.find('[data-testid="k8s-add-confirm"]').trigger('click')
      expect(mockCreateCluster).toHaveBeenCalledWith('dev', 'https://dev:6443', 'kubeconfig-body')
    })
  })
})
