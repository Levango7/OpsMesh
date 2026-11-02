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
  listCloudAccounts: vi.fn().mockResolvedValue({ accounts: [{ id: 'acc1', name: '阿里云主账号' }] })
}))

import CloudResourcesView from '../CloudResourcesView.vue'

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
    mount(CloudResourcesView, { global: { mocks: { $t: mockT } } })
    await flushPromises()
    expect(st.fetchResources).toHaveBeenCalled()
  })

  it('渲染 resourceName（不是 name——后端无此字段）', async () => {
    st.state.resources = [{ id: 'r1', resourceName: 'ecs-01', resourceType: 'ecs', cloudAccountId: 'acc1' }]
    const wrapper = mount(CloudResourcesView, { global: { mocks: { $t: mockT } } })
    await flushPromises()
    expect(wrapper.text()).toContain('ecs-01')
    expect(wrapper.text()).toContain('ecs')
  })

  it('账号列把 cloudAccountId 映射成账号名', async () => {
    st.state.resources = [{ id: 'r1', resourceName: 'ecs-01', cloudAccountId: 'acc1' }]
    const wrapper = mount(CloudResourcesView, { global: { mocks: { $t: mockT } } })
    await flushPromises()
    expect(wrapper.text()).toContain('阿里云主账号')
  })

  it('切换筛选下拉触发 setFilter 并重新拉取', async () => {
    const wrapper = mount(CloudResourcesView, { global: { mocks: { $t: mockT } } })
    await flushPromises()
    const sel = wrapper.find('[data-testid="cloud-resources-account-filter"]')
    await sel.setValue('acc1')
    await flushPromises()
    expect(st.setFilter).toHaveBeenCalledWith('accountID', 'acc1')
    expect(st.fetchResources).toHaveBeenCalled()
  })
})
