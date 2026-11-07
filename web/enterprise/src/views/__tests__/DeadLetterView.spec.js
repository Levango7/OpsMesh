import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { reactive } from 'vue'

const state = reactive({ list: [], loading: false, error: '' })
const fetchMock = vi.fn().mockResolvedValue()
const retryMock = vi.fn().mockResolvedValue()
const purgeMock = vi.fn().mockResolvedValue()

vi.mock('@/i18n', () => ({
  t: (key, params) => (params ? key + JSON.stringify(params) : key),
  currentLang: { value: 'zh' },
  setLang: vi.fn(),
  initLang: vi.fn(),
  default: {},
}))

vi.mock('@/stores/dead-letter', () => ({
  useDeadLetterStore: () => ({
    list: state.list,
    loading: state.loading,
    error: state.error,
    fetchDeadLetterTasks: fetchMock,
    retry: retryMock,
    purge: purgeMock,
  }),
}))

// 2026-11-06 五维排查批 2：视图在 setup 取 auth store 做处置门（无 Pinia 会抛 [🍍]）。
const perms = reactive({ list: [] })
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ hasPerm: (p) => perms.list.includes(p) }),
}))
vi.mock('@/utils/toast', () => ({
  toast: { success: vi.fn(), error: vi.fn(), warn: vi.fn() },
}))

vi.mock('@/components/Icon.vue', () => ({
  default: { name: 'Icon', props: ['name', 'size'], template: '<span class="icon-mock" />' },
}))
vi.mock('@/components/StatusBadge.vue', () => ({
  default: { name: 'StatusBadge', props: ['status', 'text'], template: '<span class="badge-mock" />' },
}))
vi.mock('@/components/DataTable.vue', () => ({
  default: {
    name: 'DataTable',
    props: ['columns', 'rows', 'rowKey', 'loading', 'emptyText'],
    template: '<div class="datatable-mock"><slot name="cell-actions" :row="rows[0]" /></div>',
  },
}))

import DeadLetterView from '@/views/DeadLetterView.vue'
// ConfirmModal 内部 Teleport 到 body，用 findComponent 断言 props/emit 比查 DOM 稳
import ConfirmModal from '@/components/ConfirmModal.vue'

const mockT = (key) => key

describe('DeadLetterView', () => {
  beforeEach(() => {
    state.list = []
    state.loading = false
    state.error = ''
    vi.clearAllMocks()
  })

  it('mounts and calls fetch', async () => {
    const w = mount(DeadLetterView, { global: { mocks: { $t: mockT } } })
    await nextTick()
    await nextTick()
    expect(w.exists()).toBe(true)
    expect(w.find('[data-testid="dead-letter-title"]').exists()).toBe(true)
    expect(fetchMock).toHaveBeenCalled()
  })

  it('renders refresh button', async () => {
    const w = mount(DeadLetterView, { global: { mocks: { $t: mockT } } })
    await nextTick()
    expect(w.find('[data-testid="dead-letter-refresh-btn"]').exists()).toBe(true)
  })

  // —— 2026-11-06 五维排查批 2：处置门 + 清除确认（改回"点即删"即判红）——
  it('无 task:write/task:cancel：重试与清除按钮禁用 + 常驻提示', async () => {
    perms.list = []
    state.list = [{ taskID: 't1', agentID: 'a1', command: 'echo hi' }]
    const w = mount(DeadLetterView, { global: { mocks: { $t: mockT } } })
    await nextTick()
    expect(w.find('[data-testid="dead-letter-retry-btn"]').attributes('disabled')).toBeDefined()
    expect(w.find('[data-testid="dead-letter-purge-btn"]').attributes('disabled')).toBeDefined()
    expect(w.find('[data-testid="write-denied-hint"]').exists()).toBe(true)
    expect(w.find('[data-testid="write-denied-hint"]').text()).toBe('common.write_denied')
  })

  it('持 task:write：点清除只开确认框（不调 API），确认后才调 purge', async () => {
    perms.list = ['task:write']
    state.list = [{ taskID: 't1', agentID: 'a1', command: 'echo hi' }]
    const w = mount(DeadLetterView, { global: { mocks: { $t: mockT } } })
    await nextTick()
    const btn = w.find('[data-testid="dead-letter-purge-btn"]')
    expect(btn.attributes('disabled')).toBeUndefined()
    expect(w.find('[data-testid="write-denied-hint"]').exists()).toBe(false)

    await btn.trigger('click')
    expect(purgeMock).not.toHaveBeenCalled()
    const modal = w.findComponent(ConfirmModal)
    expect(modal.props('modelValue')).toBe(true)
    expect(modal.props('message')).toBe('dead_letter.purge_confirm')

    modal.vm.$emit('confirm')
    await nextTick()
    expect(purgeMock).toHaveBeenCalledWith('t1')
  })
})
