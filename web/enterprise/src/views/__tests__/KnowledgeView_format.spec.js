import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { reactive } from 'vue'

const store = reactive({
  stats: { totalDocs: 1 },
  syncConfigs: [{ source: 'cmdb', enabled: true, interval: 3600000000000, lastSync: '0001-01-01T00:00:00Z' }],
  documents: [{ id: 'doc-1', title: 'Doc', source: 'cmdb', createdAt: '0001-01-01T00:00:00Z' }],
  searchResults: [],
  searchQuery: '',
  searchLoading: false,
  loading: false,
  error: '',
  fetchDocs: vi.fn().mockResolvedValue(),
  fetchStats: vi.fn().mockResolvedValue(),
  fetchSyncConfigs: vi.fn().mockResolvedValue(),
})

vi.mock('@/stores/knowledge', () => ({ useKnowledgeStore: () => store }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ hasPerm: () => true }) }))
vi.mock('@/i18n', () => ({ t: (key) => key }))

import KnowledgeView from '@/views/KnowledgeView.vue'

describe('Knowledge 时间/间隔展示', () => {
  it('纳秒转小时，零时间显示未同步/空态而非原始 Go 值', async () => {
    const wrapper = mount(KnowledgeView, {
      global: {
        mocks: { $t: (key) => key },
        stubs: {
          PageShell: { template: '<div><slot /></div>' },
          MetricsCard: { template: '<div><slot /></div>' },
          ConfirmModal: true,
        },
      },
    })
    await flushPromises()
    const text = wrapper.text()
    expect(text).toContain('1h')
    expect(text).toContain('knowledge.never_synced')
    expect(text).toContain('—')
    expect(text).not.toContain('3600000000000')
    expect(text).not.toContain('0001-01-01T00:00:00Z')
  })

  it('间隔边界使用 m/s/ms，非法值不伪造为 0', () => {
    const wrapper = mount(KnowledgeView, {
      global: {
        mocks: { $t: (key) => key },
        stubs: { PageShell: { template: '<div><slot /></div>' }, MetricsCard: true, ConfirmModal: true },
      },
    })
    expect(wrapper.vm.formatInterval(5400000000000)).toBe('1.5h')
    expect(wrapper.vm.formatInterval(120000000000)).toBe('2m')
    expect(wrapper.vm.formatInterval(90000000000)).toBe('1.5m')
    expect(wrapper.vm.formatInterval(5000000)).toBe('5ms')
    expect(wrapper.vm.formatInterval(0)).toBe('0s')
    expect(wrapper.vm.formatInterval(-1)).toBe('—')
    expect(wrapper.vm.formatInterval('not-a-number')).toBe('—')
  })
})
