// FluxView 写按钮级权限门测试（2026-11-06，按钮级门范式延伸批）
// 背景：flux.go 5 个 handler 一律 requirePermission('flux:read'/'flux:write')——
// viewer/operator 持 flux:read 可进页但写操作必 403。本用例钉住前端显式化：
// 无 flux:write 时新建/同步/编辑/删除全禁用 + 常驻提示；有权限时全可用。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const st = vi.hoisted(() => {
  const { reactive } = require('vue')
  return {
    auth: reactive({ perms: [] }),
    listFluxApps: vi.fn().mockResolvedValue({ apps: [] }),
    createFluxApp: vi.fn(),
    updateFluxApp: vi.fn(),
    deleteFluxApp: vi.fn(),
    syncFluxApp: vi.fn()
  }
})

vi.mock('@/i18n', () => ({
  t: (k) => k,
  currentLang: { value: 'zh' },
  setLang: vi.fn()
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ hasPerm: (p) => st.auth.perms.includes(p) })
}))

vi.mock('@/api/flux', () => ({
  listFluxApps: st.listFluxApps,
  createFluxApp: st.createFluxApp,
  updateFluxApp: st.updateFluxApp,
  deleteFluxApp: st.deleteFluxApp,
  syncFluxApp: st.syncFluxApp
}))

vi.mock('@/utils/toast', () => ({
  toast: { success: vi.fn(), error: vi.fn(), warn: vi.fn() }
}))

vi.mock('@/components/DataTable.vue', () => ({
  default: {
    name: 'DataTable',
    props: ['columns', 'rows', 'rowKey', 'emptyText', 'loading'],
    template: '<div class="dt-stub"><slot name="cell-actions" :row="rows[0] || { id: \'a1\', name: \'app1\' }" /></div>'
  }
}))

import FluxView from '@/views/FluxView.vue'

const mockT = (k) => k

function mountView() {
  return mount(FluxView, { global: { mocks: { $t: mockT } } })
}

describe('FluxView 写按钮级权限门', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    st.auth.perms = []
  })

  it('无 flux:write：四类写按钮全禁用 + 常驻提示出现', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('[data-testid="flux-add-btn"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="flux-sync-btn"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="flux-edit-btn"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="flux-delete-btn"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="flux-write-denied"]').exists()).toBe(true)
  })

  it('持 flux:write：按钮可用且无禁用提示', async () => {
    st.auth.perms = ['flux:write']
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('[data-testid="flux-add-btn"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.find('[data-testid="flux-sync-btn"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.find('[data-testid="flux-write-denied"]').exists()).toBe(false)
  })
})
