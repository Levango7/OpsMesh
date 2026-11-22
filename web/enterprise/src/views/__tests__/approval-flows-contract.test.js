import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const { store, successToast } = vi.hoisted(() => {
  const { reactive } = require('vue')
  return {
    store: reactive({
      flows: [],
      loading: false,
      error: '',
      fetchFlows: vi.fn().mockResolvedValue(),
      createFlow: vi.fn().mockResolvedValue({}),
      updateFlow: vi.fn().mockResolvedValue({}),
      removeFlow: vi.fn().mockResolvedValue({}),
    }),
    successToast: vi.fn(),
  }
})

vi.mock('@/stores/approval', () => ({ useApprovalStore: () => store }))
vi.mock('@/i18n', () => ({ t: (key) => key }))
vi.mock('@/utils/toast', () => ({ toast: { success: successToast } }))

import ApprovalFlowsView from '@/views/ApprovalFlowsView.vue'

function mountView() {
  return mount(ApprovalFlowsView, {
    global: {
      mocks: { $t: (key) => key },
      stubs: {
        PageShell: { template: '<div><slot /></div>' },
        Icon: true,
        ConfirmModal: true,
      },
    },
  })
}

describe('ApprovalFlowsView 审批流契约', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    store.flows = []
    store.loading = false
    store.error = ''
  })

  it('列表展示名称、描述、步骤和创建时间，不再只有空行', async () => {
    store.flows = [{
      id: 'flow-1',
      name: '高危审批',
      description: '双人复核',
      triggerType: 'shell',
      enabled: true,
      createdAt: '2026-11-22T01:00:00Z',
      steps: [{ id: 's1', name: '经理审批', order: 1, mode: 'sequential', approvers: ['alice'] }],
    }]
    const wrapper = mountView()
    await flushPromises()
    const text = wrapper.text()
    expect(text).toContain('高危审批')
    expect(text).toContain('双人复核')
    expect(text).toContain('2026-11-22T01:00:00Z')
    expect(text).toContain('s1')
    expect(text).toContain('sequential')
    expect(text).not.toContain('innerText')
  })

  it('创建时补 triggerType/enabled 与步骤必填字段', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.find('[data-testid="approval-flow-create-btn"]').trigger('click')
    const inputs = wrapper.findAll('input')
    await inputs[0].setValue('生产变更审批')
    await inputs[1].setValue('双人复核后执行')
    await wrapper.find('textarea').setValue('[{"name":"Manager Approval","approvers":["alice"],"order":1}]')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(store.createFlow).toHaveBeenCalledTimes(1)
    const body = store.createFlow.mock.calls[0][0]
    expect(body.name).toBe('生产变更审批')
    expect(body.description).toBe('双人复核后执行')
    expect(body.triggerType).toBe('shell')
    expect(body.enabled).toBe(true)
    expect(body.steps).toHaveLength(1)
    expect(body.steps[0]).toMatchObject({
      id: 'step-1',
      name: 'Manager Approval',
      order: 1,
      mode: 'sequential',
      approvers: ['alice'],
    })
    expect(successToast).toHaveBeenCalled()
  })
})
