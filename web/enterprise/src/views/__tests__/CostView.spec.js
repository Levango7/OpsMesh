// CostView 预算口径提示守卫（TD-97 ① 的用户面半）
//
// 背景：预算执法已落 main（a1f6e61），但「额度是进程内计数」这一口径只写在
// internal/ai/gateway/gateway.go 的 Complete 契约注释里，页面上一个字没有——
// 客户看到的是两个裸数字，会把「重启归零 / 多副本各自计数」当成不存在的行为。
// 本文件守两件事：提示真出现在渲染 DOM 里（不是只在源码里写了个键），
// 且双语都能解析、并把三个要素说全（进程内 / 重启归零 / 多副本×额度）。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, reactive } from 'vue'
import zhOps from '@/i18n/locales/zh/ops.json'
import enOps from '@/i18n/locales/en/ops.json'

const state = reactive({
  budget: { usedToday: 1200, dailyLimit: 100000, usedMonth: 34000, monthlyLimit: 1000000 },
  alerts: [],
  report: null,
  routes: [],
  loading: false,
  error: ''
})

const fetchBudget = vi.fn().mockResolvedValue()
const fetchAlerts = vi.fn().mockResolvedValue()
const fetchReport = vi.fn().mockResolvedValue()
const fetchRoutes = vi.fn().mockResolvedValue()
const updateBudget = vi.fn().mockResolvedValue()
const generateMarkdownReport = vi.fn().mockResolvedValue('')

vi.mock('@/i18n', () => ({
  t: (key) => key,
  currentLang: { value: 'zh' },
  setLang: vi.fn(),
  initLang: vi.fn(),
  default: {}
}))

vi.mock('@/stores/cost', () => ({
  useCostStore: () => ({
    get budget() { return state.budget },
    get alerts() { return state.alerts },
    get report() { return state.report },
    get routes() { return state.routes },
    get loading() { return state.loading },
    get error() { return state.error },
    fetchBudget,
    fetchAlerts,
    fetchReport,
    fetchRoutes,
    updateBudget,
    generateMarkdownReport
  })
}))

vi.mock('@/components/MetricsCard.vue', () => ({
  default: { name: 'MetricsCard', props: ['title', 'icon'], template: '<div class="metrics-mock"><slot /></div>' }
}))
vi.mock('@/components/DataTable.vue', () => ({
  default: { name: 'DataTable', props: ['columns', 'rows', 'rowKey'], template: '<div class="datatable-mock" />' }
}))

import CostView from '@/views/CostView.vue'

const mockT = (key) => key

function mountView() {
  return mount(CostView, { global: { mocks: { $t: mockT } } })
}

describe('CostView 预算口径提示', () => {
  beforeEach(() => {
    state.budget = { usedToday: 1200, dailyLimit: 100000, usedMonth: 34000, monthlyLimit: 1000000 }
    state.alerts = []
    state.report = null
    state.routes = []
    state.error = ''
    vi.clearAllMocks()
  })

  it('渲染口径提示且可见（不是只写了个键）', async () => {
    const w = mountView()
    await nextTick()
    const note = w.find('[data-testid="cost-budget-note"]')
    expect(note.exists()).toBe(true)
    expect(note.isVisible()).toBe(true)
    expect(note.text()).toBe('cost.budget_note')
  })

  it('预算未加载（budget=null）时提示仍在——口径不该藏在数据后面', async () => {
    state.budget = null
    const w = mountView()
    await nextTick()
    expect(w.find('[data-testid="cost-budget-note"]').exists()).toBe(true)
  })

  it('双语都能解析，且三个要素说全', () => {
    const cases = [
      { locale: 'zh', text: zhOps.cost.budget_note, want: [/进程内/, /重启/, /副本/] },
      { locale: 'en', text: enOps.cost.budget_note, want: [/in-process/i, /restart/i, /replica/i] }
    ]
    for (const { locale, text, want } of cases) {
      expect(text, `${locale} 缺 cost.budget_note`).toBeTypeOf('string')
      for (const re of want) {
        expect(text, `${locale} 提示未覆盖 ${re}`).toMatch(re)
      }
    }
  })

  it('提示不得反向过度宣称（不能写成已实现持久化/跨副本硬限）', () => {
    for (const text of [zhOps.cost.budget_note, enOps.cost.budget_note]) {
      expect(text).not.toMatch(/持久化已|已支持多副本|persisted across replicas/i)
    }
  })
})
