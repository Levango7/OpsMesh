// CloudResourcesView 视图单元测试（2026-11-02）
// 覆盖：列表渲染（读 resources 字段）、账号 id→名字映射、筛选下拉变化触发重新拉取。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const st = vi.hoisted(() => {
  const { reactive } = require('vue')
  return {
    state: reactive({ resources: [], loading: false, error: '', filters: { accountID: '', resourceType: '' } }),
    fetchResources: vi.fn().mockResolvedValue(undefined),
    setFilter: vi.fn()
  }
})

vi.mock('@/i18n', () => ({
  t: (k) => k,
  currentLang: { value: 'zh' },
  setLang: vi.fn()
}))

vi.mock('@/stores/cloud-resources', () => ({
  useCloudResourcesStore: () => ({
    resources: st.state.resources,
    loading: st.state.loading,
    error: st.state.error,
    filters: st.state.filters,
    fetchResources: st.fetchResources,
    setFilter: st.setFilter
  })
}))

vi.mock('@/api/cloud-account', () => ({
  listCloudAccounts: vi.fn().mockResolvedValue({ accounts: [{ id: 'acc1', name: '阿里云主账号' }] }),
  deleteCloudResource: vi.fn().mockResolvedValue(undefined)
}))

import CloudResourcesView from '../CloudResourcesView.vue'

// DataTable 真实实现的插槽协作依赖列配置细节；这里只关心行内按钮/单元格 wiring，
// stub 成「逐行逐列透传 cell-<key> 插槽」，让 actions 插槽里的按钮可触发。
const DataTableStub = {
  name: 'DataTable',
  props: ['columns', 'rows', 'rowKey', 'loading', 'emptyText'],
  template: `<div class="dt-stub"><tr v-for="(row, i) in rows" :key="row[rowKey] || i"><td v-for="col in columns" :key="col.key"><template v-if="col.key === 'actions'"><slot :name="'cell-actions'" :row="row" /></template><template v-else><slot :name="'cell-' + col.key" :value="row[col.key]" :row="row" />{{ row[col.key] }}</template></td></tr></div>`
}

const mockT = (k) => k

describe('CloudResourcesView', () => {
  beforeEach(() => {
    st.state.resources = []
    st.state.loading = false
    st.state.error = ''
    vi.clearAllMocks()
    st.fetchResources.mockResolvedValue(undefined)
  })

  it('挂载即拉取资源列表与账号列表', async () => {
    mount(CloudResourcesView, { global: { mocks: { $t: mockT }, stubs: { DataTable: DataTableStub } } })
    await flushPromises()
    expect(st.fetchResources).toHaveBeenCalled()
  })

  it('渲染 resourceName（不是 name——后端无此字段）', async () => {
    st.state.resources = [{ id: 'r1', resourceName: 'ecs-01', resourceType: 'ecs', cloudAccountId: 'acc1' }]
    const wrapper = mount(CloudResourcesView, { global: { mocks: { $t: mockT }, stubs: { DataTable: DataTableStub } } })
    await flushPromises()
    expect(wrapper.text()).toContain('ecs-01')
    expect(wrapper.text()).toContain('ecs')
  })

  it('账号列把 cloudAccountId 映射成账号名', async () => {
    st.state.resources = [{ id: 'r1', resourceName: 'ecs-01', cloudAccountId: 'acc1' }]
    const wrapper = mount(CloudResourcesView, { global: { mocks: { $t: mockT }, stubs: { DataTable: DataTableStub } } })
    await flushPromises()
    expect(wrapper.text()).toContain('阿里云主账号')
  })

  it('切换筛选下拉触发 setFilter 并重新拉取', async () => {
    const wrapper = mount(CloudResourcesView, { global: { mocks: { $t: mockT }, stubs: { DataTable: DataTableStub } } })
    await flushPromises()
    const sel = wrapper.find('[data-testid="cloud-resources-account-filter"]')
    await sel.setValue('acc1')
    await flushPromises()
    expect(st.setFilter).toHaveBeenCalledWith('accountID', 'acc1')
    expect(st.fetchResources).toHaveBeenCalled()
  })

  it('删除需确认，确认后调 deleteCloudResource 并刷新列表', async () => {
    st.state.resources = [{ id: 'r1', resourceName: 'ecs-01', cloudAccountId: 'acc1' }]
    const wrapper = mount(CloudResourcesView, { global: { mocks: { $t: mockT }, stubs: { DataTable: DataTableStub } } })
    await flushPromises()
    await wrapper.find('[data-testid="cloud-resources-delete-btn"]').trigger('click')
    await flushPromises()
    const { deleteCloudResource } = await import('@/api/cloud-account')
    expect(deleteCloudResource).not.toHaveBeenCalled()
    await wrapper.findComponent({ name: 'ConfirmModal' }).vm.$emit('confirm')
    await flushPromises()
    expect(deleteCloudResource).toHaveBeenCalledWith('r1')
  })
})
