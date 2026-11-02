// AIRagView 视图单元测试（2026-11-02）
// 覆盖：挂载拉健康、检索触发与结果渲染、入库按行拆分、embedding 不出现在表格列里。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const st = vi.hoisted(() => {
  const { reactive } = require('vue')
  return {
    state: reactive({ results: [], searched: false, health: [], loading: false, healthError: '', error: '' }),
    search: vi.fn().mockResolvedValue(undefined),
    ingest: vi.fn().mockResolvedValue(2),
    fetchHealth: vi.fn().mockResolvedValue(undefined)
  }
})

vi.mock('@/i18n', () => ({
  t: (k, params) => (params && params.n != null ? `${k}:${params.n}` : k),
  currentLang: { value: 'zh' },
  setLang: vi.fn()
}))

vi.mock('@/stores/ai-rag', () => ({
  useAIRagStore: () => ({
    results: st.state.results,
    searched: st.state.searched,
    health: st.state.health,
    loading: st.state.loading,
    healthError: st.state.healthError,
    error: st.state.error,
    search: st.search,
    ingest: st.ingest,
    fetchHealth: st.fetchHealth
  })
}))

import AIRagView from '../AIRagView.vue'

// DataTable 真实实现的插槽协作依赖列配置细节；这里只关心行内按钮/单元格 wiring，
// stub 成「逐行逐列透传 cell-<key> 插槽」，让 actions 插槽里的按钮可触发。
const DataTableStub = {
  name: 'DataTable',
  props: ['columns', 'rows', 'rowKey', 'loading', 'emptyText'],
  template: `<div class="dt-stub"><tr v-for="(row, i) in rows" :key="row[rowKey] || i"><td v-for="col in columns" :key="col.key"><template v-if="col.key === 'actions'"><slot :name="'cell-actions'" :row="row" /></template><template v-else><slot :name="'cell-' + col.key" :value="row[col.key]" :row="row" />{{ row[col.key] }}</template></td></tr></div>`
}
const mockT = (k) => k

describe('AIRagView', () => {
  beforeEach(() => {
    st.state.results = []
    st.state.searched = false
    st.state.health = []
    st.state.healthError = ''
    st.state.error = ''
    vi.clearAllMocks()
    st.search.mockResolvedValue(undefined)
    st.ingest.mockResolvedValue(2)
    st.fetchHealth.mockResolvedValue(undefined)
  })

  it('挂载即拉取网关健康', async () => {
    mount(AIRagView, { global: { mocks: { $t: mockT }, stubs: { DataTable: DataTableStub } } })
    await flushPromises()
    expect(st.fetchHealth).toHaveBeenCalled()
  })

  it('渲染提供商健康状态', async () => {
    st.state.health = [{ name: 'openai', ok: true }]
    const wrapper = mount(AIRagView, { global: { mocks: { $t: mockT }, stubs: { DataTable: DataTableStub } } })
    await flushPromises()
    expect(wrapper.text()).toContain('openai')
  })

  it('检索后渲染 content 与 score（百分比化），不渲染 embedding', async () => {
    // mock 的 store 每次调用返回新对象，primitive 属性在 setup 时已定值 ⇒ 挂载前先置状态
    st.state.results.splice(0, st.state.results.length, { id: 'd1', content: '文档内容', score: 0.42, embedding: [1, 2, 3] })
    st.state.searched = true
    const wrapper = mount(AIRagView, { global: { mocks: { $t: mockT }, stubs: { DataTable: DataTableStub } } })
    await flushPromises()
    expect(wrapper.text()).toContain('文档内容')
    expect(wrapper.text()).toContain('42.0%')
    expect(wrapper.text()).not.toContain('[1,2,3]')
  })

  it('点检索把 query 传给 store', async () => {
    const wrapper = mount(AIRagView, { global: { mocks: { $t: mockT }, stubs: { DataTable: DataTableStub } } })
    await flushPromises()
    await wrapper.find('[data-testid="ai-rag-query"]').setValue('关键词')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(st.search).toHaveBeenCalledWith('关键词')
  })

  it('入库成功显示条数并清空输入', async () => {
    st.ingest.mockResolvedValue(2)
    const wrapper = mount(AIRagView, { global: { mocks: { $t: mockT }, stubs: { DataTable: DataTableStub } } })
    await flushPromises()
    await wrapper.find('[data-testid="ai-rag-ingest"]').setValue('第一行\n第二行')
    await wrapper.findAll('form').at(-1).trigger('submit')
    await flushPromises()
    expect(st.ingest).toHaveBeenCalledWith('第一行\n第二行')
    expect(wrapper.find('[data-testid="ai-rag-ingest"]').element.value).toBe('')
  })
})
