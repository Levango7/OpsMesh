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
})
