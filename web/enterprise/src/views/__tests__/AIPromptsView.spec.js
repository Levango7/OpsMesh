// AIPromptsView 视图单元测试（2026-11-02）
// 覆盖：列表渲染（读 templates 字段）、表单可用性、删除确认流。
// 说明：保存/删除的**动作分流**（无 id 走 create、有 id 走 update）由 store 单测
// ai-prompts.test.js 逐条钉死；这里只测视图 wiring，不重复断言 store 行为。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const st = vi.hoisted(() => {
  const { reactive } = require('vue')
  return {
    state: reactive({ templates: [], loading: false, error: '' }),
    saveTemplate: vi.fn().mockResolvedValue(undefined),
    removeTemplate: vi.fn().mockResolvedValue(undefined),
    fetchTemplates: vi.fn().mockResolvedValue(undefined)
  }
})

vi.mock('@/i18n', () => ({
  t: (k) => k,
  currentLang: { value: 'zh' },
  setLang: vi.fn()
}))

vi.mock('@/stores/ai-prompts', () => ({
  useAIPromptsStore: () => ({
    templates: st.state.templates,
    loading: st.state.loading,
    error: st.state.error,
    saveTemplate: st.saveTemplate,
    removeTemplate: st.removeTemplate,
    fetchTemplates: st.fetchTemplates
  })
}))

import AIPromptsView from '../AIPromptsView.vue'
const mockT = (k) => k

// DataTable 的真实实现依赖列配置与插槽协作，本测试只关心行内按钮 wiring，
// 故 stub 成「逐行渲染 actions 插槽 + 透传相识内容」，让 delete/edit 按钮可触发。
const DataTableStub = {
  name: 'DataTable',
  props: ['columns', 'rows', 'rowKey', 'loading', 'emptyText'],
  template: `<div class="dt-stub"><tr v-for="(row, i) in rows" :key="row[rowKey] || i"><td v-for="col in columns" :key="col.key"><template v-if="col.key === 'actions'"><slot :name="'cell-actions'" :row="row" /></template><template v-else><slot :name="'cell-' + col.key" :value="row[col.key]" :row="row" />{{ row[col.key] }}</template></td></tr></div>`
}

describe('AIPromptsView', () => {
  beforeEach(() => {
    st.state.templates = []
    st.state.loading = false
    st.state.error = ''
    vi.clearAllMocks()
    st.saveTemplate.mockResolvedValue(undefined)
    st.removeTemplate.mockResolvedValue(undefined)
    st.fetchTemplates.mockResolvedValue(undefined)
  })

  it('挂载即拉取列表', async () => {
    mount(AIPromptsView, { global: { mocks: { $t: mockT }, stubs: { DataTable: DataTableStub } } })
    await flushPromises()
    expect(st.fetchTemplates).toHaveBeenCalled()
  })

  it('渲染模板名与分类', async () => {
    st.state.templates = [{ id: 'p1', name: '默认提示词', category: 'ops', content: 'x' }]
    const wrapper = mount(AIPromptsView, { global: { mocks: { $t: mockT }, stubs: { DataTable: DataTableStub } } })
    await flushPromises()
    expect(wrapper.text()).toContain('默认提示词')
    expect(wrapper.text()).toContain('ops')
  })

  it('点「新建」后表单可用（抽屉打开 + 保存按钮可点）', async () => {
    const wrapper = mount(AIPromptsView, { global: { mocks: { $t: mockT }, stubs: { DataTable: DataTableStub } } })
    await flushPromises()
    await wrapper.find('[data-testid="ai-prompts-create-btn"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('.drawer').exists()).toBe(true)
    const saveBtn = wrapper.find('[data-testid="ai-prompts-save-btn"]')
    expect(saveBtn.exists()).toBe(true)
    expect(saveBtn.attributes('aria-disabled')).toBe('false')
  })

  it('删除需先确认，确认后才 removeTemplate', async () => {
    st.state.templates = [{ id: 'p1', name: 'x', category: '', content: '' }]
    const wrapper = mount(AIPromptsView, { global: { mocks: { $t: mockT }, stubs: { DataTable: DataTableStub } } })
    await flushPromises()
    await wrapper.find('[data-testid="ai-prompts-delete-btn"]').trigger('click')
    await flushPromises()
    expect(st.removeTemplate).not.toHaveBeenCalled()
    await wrapper.findComponent({ name: 'ConfirmModal' }).vm.$emit('confirm')
    await flushPromises()
    expect(st.removeTemplate).toHaveBeenCalledWith('p1')
  })

  it('编辑已有模板时抽屉带出该行', async () => {
    st.state.templates = [{ id: 'p1', name: '已存在', category: 'chat', content: 'abc' }]
    const wrapper = mount(AIPromptsView, { global: { mocks: { $t: mockT }, stubs: { DataTable: DataTableStub } } })
    await flushPromises()
    await wrapper.find('[data-testid="ai-prompts-edit-btn"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('.drawer').exists()).toBe(true)
  })
})
