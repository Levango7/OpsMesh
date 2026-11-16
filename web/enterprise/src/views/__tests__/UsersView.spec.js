// UsersView 服务端分页单元测试（ADR-2026-11-15 §ADR-3）
//
// 覆盖：
//   - 首屏请求带 {page:1, pageSize:20}
//   - 信封 {users,total,page} 渲染数据行；total > pageSize 时渲染 Pagination
//   - total <= pageSize 时不渲染 Pagination
//   - 点击下一页请求 page=2
//   - page>1 返回空页时回退请求 page-1
//   - API 失败时展示加载错误横幅
//
// mock 策略：
//   - @/i18n：t 返回键本身，currentLang 固定 zh
//   - @/api/auth：authApi.listUsers / listRoles 用 vi.fn 控制信封
//   - @/components/Icon.vue：stub（纯装饰）
//   - 其余组件（DataTable / Pagination / PageShell 等）用真实实现，
//     以验证行渲染与分页器确实出现在 DOM 里
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const { mockListUsers, mockListRoles } = vi.hoisted(() => ({
  mockListUsers: vi.fn(),
  mockListRoles: vi.fn()
}))

vi.mock('@/i18n', () => ({
  t: (key) => key,
  currentLang: { value: 'zh' },
  setLang: vi.fn(),
  initLang: vi.fn(),
  default: {}
}))

vi.mock('@/api/auth', () => ({
  authApi: {
    listUsers: mockListUsers,
    listRoles: mockListRoles
  }
}))

vi.mock('@/components/Icon.vue', () => ({
  default: { name: 'Icon', props: ['name', 'size'], template: '<span class="icon-mock" />' }
}))

import UsersView from '@/views/UsersView.vue'

const mockT = (key) => key

function mountView() {
  return mount(UsersView, { global: { mocks: { $t: mockT } } })
}

function envelope(page, users, total) {
  return { users, total, page, pageSize: 20 }
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

const alice = { id: 'u1', username: 'alice', status: 'active', roleIDs: [] }
const bob = { id: 'u2', username: 'bob', status: 'pending', roleIDs: [] }

describe('UsersView 服务端分页', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockListRoles.mockResolvedValue({ roles: [] })
  })

  it('首屏请求发送 {page:1, pageSize:20}', async () => {
    mockListUsers.mockResolvedValue(envelope(1, [], 0))
    mountView()
    await flushPromises()

    expect(mockListUsers).toHaveBeenCalledTimes(1)
    expect(mockListUsers).toHaveBeenCalledWith({ page: 1, pageSize: 20 })
  })

  it('信封 users/total/page 渲染数据行，并在 total > pageSize 时渲染分页器', async () => {
    mockListUsers.mockResolvedValue(envelope(1, [alice, bob], 45))
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.findAll('tbody tr')).toHaveLength(2)
    expect(wrapper.text()).toContain('alice')
    expect(wrapper.text()).toContain('bob')
    expect(wrapper.find('.pager').exists()).toBe(true)
  })

  it('total <= pageSize 时不渲染分页器', async () => {
    mockListUsers.mockResolvedValue(envelope(1, [alice], 1))
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('.pager').exists()).toBe(false)
  })

  it('点击下一页请求 page=2 并渲染第二页数据', async () => {
    mockListUsers.mockImplementation(({ page }) =>
      Promise.resolve(page === 1 ? envelope(1, [alice], 45) : envelope(2, [bob], 45))
    )
    const wrapper = mountView()
    await flushPromises()

    await wrapper.findAll('.pager button')[1].trigger('click')
    await flushPromises()

    expect(mockListUsers).toHaveBeenLastCalledWith({ page: 2, pageSize: 20 })
    expect(wrapper.text()).toContain('bob')
  })

  it('page>1 返回空页时回退请求上一页', async () => {
    const requested = []
    mockListUsers.mockImplementation(({ page }) => {
      requested.push(page)
      return Promise.resolve(page === 2 ? envelope(2, [], 45) : envelope(1, [alice], 45))
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.findAll('.pager button')[1].trigger('click')
    await flushPromises()

    expect(requested).toEqual([1, 2, 1])
    expect(mockListUsers).toHaveBeenLastCalledWith({ page: 1, pageSize: 20 })
    expect(wrapper.text()).toContain('alice')
  })

  it('API 失败时展示加载错误横幅', async () => {
    mockListUsers.mockRejectedValue({ j: { error: 'list users failed' } })
    const wrapper = mountView()
    await flushPromises()

    const err = wrapper.find('[data-testid="users-error"]')
    expect(err.exists()).toBe(true)
    expect(err.text()).toContain('list users failed')
  })

  it('慢的旧响应（page 2）晚到不覆盖较新的 page 1 数据', async () => {
    const slowPage2 = deferred()
    const slowRefresh = deferred()
    let calls = 0
    mockListUsers.mockImplementation(({ page }) => {
      calls += 1
      if (calls === 1) return Promise.resolve(envelope(1, [alice], 45))
      if (page === 2) return slowPage2.promise
      return slowRefresh.promise
    })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('alice')

    // 下一页请求（旧序号）在途时点刷新（新序号，刷新允许在加载中强制发起）
    await wrapper.findAll('.pager button')[1].trigger('click')
    await wrapper.find('[data-testid="users-refresh-btn"]').trigger('click')
    await flushPromises()
    expect(mockListUsers).toHaveBeenCalledTimes(3)

    // 旧 page=2 先返回：必须被丢弃，且不得提前关闭 loading
    slowPage2.resolve(envelope(2, [bob], 45))
    await flushPromises()
    expect(wrapper.text()).not.toContain('bob')
    expect(wrapper.text()).toContain('common.loading')

    // 新 page=1 后返回：正常落盘
    slowRefresh.resolve(envelope(1, [alice], 45))
    await flushPromises()
    expect(wrapper.text()).toContain('alice')
    expect(wrapper.text()).not.toContain('bob')
    expect(wrapper.find('[data-testid="users-error"]').exists()).toBe(false)
  })

  it('旧请求的失败晚到不影响新请求的成功结果', async () => {
    const slowPage2 = deferred()
    const slowRefresh = deferred()
    let calls = 0
    mockListUsers.mockImplementation(({ page }) => {
      calls += 1
      if (calls === 1) return Promise.resolve(envelope(1, [alice], 45))
      if (page === 2) return slowPage2.promise
      return slowRefresh.promise
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.findAll('.pager button')[1].trigger('click')
    await wrapper.find('[data-testid="users-refresh-btn"]').trigger('click')

    // 新请求先成功，旧请求随后失败：错误横幅不得出现
    slowRefresh.resolve(envelope(1, [alice], 45))
    await flushPromises()
    slowPage2.reject({ j: { error: 'stale page 2 failed' } })
    await flushPromises()

    expect(wrapper.text()).toContain('alice')
    expect(wrapper.find('[data-testid="users-error"]').exists()).toBe(false)
  })

  it('加载中重复点击下一页只发起一次额外请求', async () => {
    const slowPage2 = deferred()
    let calls = 0
    mockListUsers.mockImplementation(({ page }) => {
      calls += 1
      if (page === 1) return Promise.resolve(envelope(1, [alice], 45))
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
    slowPage2.resolve(envelope(2, [bob], 45))
    await flushPromises()
    expect(wrapper.text()).toContain('bob')
  })

  it('null 信封渲染空状态且 total 归零，不崩溃', async () => {
    mockListUsers.mockResolvedValue(null)
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="dt-empty"]').exists()).toBe(true)
    expect(wrapper.find('.pager').exists()).toBe(false)
    expect(wrapper.find('[data-testid="users-error"]').exists()).toBe(false)
  })

  it('畸形信封（users=null、total/page 非数字）渲染空状态不崩溃', async () => {
    mockListUsers.mockResolvedValue({ users: null, total: 'not-a-number', page: 'x' })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="dt-empty"]').exists()).toBe(true)
    expect(wrapper.find('.pager').exists()).toBe(false)
    expect(wrapper.find('[data-testid="users-error"]').exists()).toBe(false)
  })

  it('字符串 total 可参与分页判定，负数 total 收敛为 0', async () => {
    mockListUsers.mockResolvedValue({ users: [alice], total: '45', page: '1' })
    const withStringTotal = mountView()
    await flushPromises()
    expect(withStringTotal.find('.pager').exists()).toBe(true)

    mockListUsers.mockResolvedValue({ users: [alice], total: -7, page: '1' })
    const withNegativeTotal = mountView()
    await flushPromises()
    expect(withNegativeTotal.find('.pager').exists()).toBe(false)
    expect(withNegativeTotal.text()).toContain('alice')
  })

  it('旧版数组信封仍可渲染', async () => {
    mockListUsers.mockResolvedValue([alice, bob])
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.findAll('tbody tr')).toHaveLength(2)
    expect(wrapper.text()).toContain('alice')
    expect(wrapper.text()).toContain('bob')
  })
})
