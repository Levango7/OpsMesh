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

// 手动可控 Promise：用于精确编排「旧响应晚到」的竞态顺序（不依赖真实计时器）。
function deferred() {
  let resolve
  let reject
  const promise = new Promise((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
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

  it('慢的旧响应（page 2）晚到不覆盖较新的 page 1 数据', async () => {
    const slowPage2 = deferred()
    const slowRefresh = deferred()
    let calls = 0
    mockListTenants.mockImplementation(({ page }) => {
      calls += 1
      if (calls === 1) return Promise.resolve(envelope(1, [acme], 45))
      if (page === 2) return slowPage2.promise
      return slowRefresh.promise
    })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('acme')

    // 下一页请求（旧序号）在途时点刷新（新序号，刷新允许在加载中强制发起）
    await wrapper.findAll('.pager button')[1].trigger('click')
    const refreshBtn = wrapper.findAll('button').find((b) => b.text().includes('common.refresh'))
    await refreshBtn.trigger('click')
    await flushPromises()
    expect(mockListTenants).toHaveBeenCalledTimes(3)

    slowPage2.resolve(envelope(2, [globex], 45))
    await flushPromises()
    expect(wrapper.text()).not.toContain('globex')
    expect(wrapper.text()).toContain('common.loading')

    slowRefresh.resolve(envelope(1, [acme], 45))
    await flushPromises()
    expect(wrapper.text()).toContain('acme')
    expect(wrapper.text()).not.toContain('globex')
    expect(wrapper.find('[data-testid="tenants-error"]').exists()).toBe(false)
  })

  it('旧请求的失败晚到不影响新请求的成功结果', async () => {
    const slowPage2 = deferred()
    const slowRefresh = deferred()
    let calls = 0
    mockListTenants.mockImplementation(({ page }) => {
      calls += 1
      if (calls === 1) return Promise.resolve(envelope(1, [acme], 45))
      if (page === 2) return slowPage2.promise
      return slowRefresh.promise
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.findAll('.pager button')[1].trigger('click')
    const refreshBtn = wrapper.findAll('button').find((b) => b.text().includes('common.refresh'))
    await refreshBtn.trigger('click')

    slowRefresh.resolve(envelope(1, [acme], 45))
    await flushPromises()
    slowPage2.reject({ j: { error: 'stale page 2 failed' } })
    await flushPromises()

    expect(wrapper.text()).toContain('acme')
    expect(wrapper.find('[data-testid="tenants-error"]').exists()).toBe(false)
  })

  it('加载中重复点击下一页只发起一次额外请求', async () => {
    const slowPage2 = deferred()
    let calls = 0
    mockListTenants.mockImplementation(({ page }) => {
      calls += 1
      if (page === 1) return Promise.resolve(envelope(1, [acme], 45))
      return slowPage2.promise
    })
    const wrapper = mountView()
    await flushPromises()
    expect(calls).toBe(1)

    const nextBtn = wrapper.findAll('.pager button')[1]
    await nextBtn.trigger('click')
    await nextBtn.trigger('click')
    await nextBtn.trigger('click')
    await flushPromises()

    expect(calls).toBe(2)
    slowPage2.resolve(envelope(2, [globex], 45))
    await flushPromises()
    expect(wrapper.text()).toContain('globex')
  })

  it('null 信封渲染空状态且 total 归零，不崩溃', async () => {
    mockListTenants.mockResolvedValue(null)
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="dt-empty"]').exists()).toBe(true)
    expect(wrapper.find('.pager').exists()).toBe(false)
    expect(wrapper.find('[data-testid="tenants-error"]').exists()).toBe(false)
  })

  it('畸形信封（tenants=null、total/page 非数字）渲染空状态不崩溃', async () => {
    mockListTenants.mockResolvedValue({ tenants: null, total: 'not-a-number', page: 'x' })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="dt-empty"]').exists()).toBe(true)
    expect(wrapper.find('.pager').exists()).toBe(false)
    expect(wrapper.find('[data-testid="tenants-error"]').exists()).toBe(false)
  })

  it('字符串 total 可参与分页判定，负数 total 收敛为 0', async () => {
    mockListTenants.mockResolvedValue({ tenants: [acme], total: '45', page: '1' })
    const withStringTotal = mountView()
    await flushPromises()
    expect(withStringTotal.find('.pager').exists()).toBe(true)

    mockListTenants.mockResolvedValue({ tenants: [acme], total: -7, page: '1' })
    const withNegativeTotal = mountView()
    await flushPromises()
    expect(withNegativeTotal.find('.pager').exists()).toBe(false)
    expect(withNegativeTotal.text()).toContain('acme')
  })

  it('旧版数组信封仍可渲染', async () => {
    mockListTenants.mockResolvedValue([acme, globex])
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.findAll('tbody tr')).toHaveLength(2)
    expect(wrapper.text()).toContain('acme')
    expect(wrapper.text()).toContain('globex')
  })
})
