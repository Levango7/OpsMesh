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
})
