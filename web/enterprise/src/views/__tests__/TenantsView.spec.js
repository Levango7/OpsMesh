// TenantsView 服务端分页单元测试（ADR-2026-11-15 §ADR-3）
//
// 覆盖：
//   - 首屏请求带 {page:1, pageSize:20}
//   - 信封 {tenants,total,page} 渲染数据行；total > pageSize 时渲染 Pagination
//   - total <= pageSize 时不渲染 Pagination
//   - 点击下一页请求 page=2
//   - page>1 返回空页时回退请求 page-1
//   - API 失败时展示加载错误横幅
//
// mock 策略：
//   - @/i18n：t 返回键本身，currentLang 固定 zh
//   - @/api/tenant：listTenants 用 vi.fn 控制信封（其余导出占位）
//   - @/utils/toast：stub，避免操作反馈副作用
//   - @/components/Icon.vue：stub（纯装饰）
//   - 其余组件（DataTable / Pagination / PageShell 等）用真实实现，
//     以验证行渲染与分页器确实出现在 DOM 里
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const { mockListTenants } = vi.hoisted(() => ({
  mockListTenants: vi.fn()
}))

vi.mock('@/i18n', () => ({
  t: (key) => key,
  currentLang: { value: 'zh' },
  setLang: vi.fn(),
  initLang: vi.fn(),
  default: {}
}))

vi.mock('@/api/tenant', () => ({
  listTenants: mockListTenants,
  createTenant: vi.fn(),
  getTenant: vi.fn(),
  updateTenant: vi.fn(),
  deleteTenant: vi.fn(),
  suspendTenant: vi.fn(),
  activateTenant: vi.fn()
}))

vi.mock('@/utils/toast', () => ({
  toast: { success: vi.fn(), error: vi.fn(), warn: vi.fn() },
  default: {}
}))

vi.mock('@/components/Icon.vue', () => ({
  default: { name: 'Icon', props: ['name', 'size'], template: '<span class="icon-mock" />' }
}))

import TenantsView from '@/views/TenantsView.vue'

const mockT = (key) => key

function mountView() {
  return mount(TenantsView, { global: { mocks: { $t: mockT } } })
}

function envelope(page, tenants, total) {
  return { tenants, total, page, pageSize: 20 }
}

const acme = { id: 't1', name: 'acme', status: 'active', usage: { devices: 1, tasks: 2, alerts: 0 } }
const globex = { id: 't2', name: 'globex', status: 'suspended', usage: {} }

describe('TenantsView 服务端分页', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('首屏请求发送 {page:1, pageSize:20}', async () => {
    mockListTenants.mockResolvedValue(envelope(1, [], 0))
    mountView()
    await flushPromises()

    expect(mockListTenants).toHaveBeenCalledTimes(1)
    expect(mockListTenants).toHaveBeenCalledWith({ page: 1, pageSize: 20 })
  })

  it('信封 tenants/total/page 渲染数据行，并在 total > pageSize 时渲染分页器', async () => {
    mockListTenants.mockResolvedValue(envelope(1, [acme, globex], 45))
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.findAll('tbody tr')).toHaveLength(2)
    expect(wrapper.text()).toContain('acme')
    expect(wrapper.text()).toContain('globex')
    expect(wrapper.find('.pager').exists()).toBe(true)
  })

  it('total <= pageSize 时不渲染分页器', async () => {
    mockListTenants.mockResolvedValue(envelope(1, [acme], 1))
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('.pager').exists()).toBe(false)
  })

  it('点击下一页请求 page=2 并渲染第二页数据', async () => {
    mockListTenants.mockImplementation(({ page }) =>
      Promise.resolve(page === 1 ? envelope(1, [acme], 45) : envelope(2, [globex], 45))
    )
    const wrapper = mountView()
    await flushPromises()

    await wrapper.findAll('.pager button')[1].trigger('click')
    await flushPromises()

    expect(mockListTenants).toHaveBeenLastCalledWith({ page: 2, pageSize: 20 })
    expect(wrapper.text()).toContain('globex')
  })

  it('page>1 返回空页时回退请求上一页', async () => {
    const requested = []
    mockListTenants.mockImplementation(({ page }) => {
      requested.push(page)
      return Promise.resolve(page === 2 ? envelope(2, [], 45) : envelope(1, [acme], 45))
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.findAll('.pager button')[1].trigger('click')
    await flushPromises()

    expect(requested).toEqual([1, 2, 1])
    expect(mockListTenants).toHaveBeenLastCalledWith({ page: 1, pageSize: 20 })
    expect(wrapper.text()).toContain('acme')
  })

  it('API 失败时展示加载错误横幅', async () => {
    mockListTenants.mockRejectedValue({ j: { error: 'list tenants failed' } })
    const wrapper = mountView()
    await flushPromises()

    const err = wrapper.find('[data-testid="tenants-error"]')
    expect(err.exists()).toBe(true)
    expect(err.text()).toContain('list tenants failed')
  })
})
