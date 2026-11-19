import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const { mockExecute, mockSuccess, mockError } = vi.hoisted(() => ({
  mockExecute: vi.fn(),
  mockSuccess: vi.fn(),
  mockError: vi.fn(),
}))

vi.mock('@/api/aiops', () => ({
  getAIOpsStatus: vi.fn().mockResolvedValue({ enabled: true }),
  analyzeAIOps: vi.fn(),
  generateRemediation: vi.fn(),
  generateRemediationNL: vi.fn(),
  executeRemediation: mockExecute,
  getAIOpsExecutions: vi.fn().mockResolvedValue({ executions: [] }),
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ hasPerm: () => true }) }))
vi.mock('@/i18n', () => ({ t: (key) => key }))
vi.mock('@/utils/toast', () => ({ toast: { success: mockSuccess, error: mockError } }))

import AIOpsView from '@/views/AIOpsView.vue'

const ButtonStub = {
  props: ['disabled'],
  emits: ['click'],
  template: '<button :disabled="disabled" @click="$emit(\'click\')"><slot /></button>',
}
const ConfirmStub = {
  props: ['modelValue'],
  emits: ['confirm'],
  template: '<div v-if="modelValue"><button data-testid="execute-confirm" @click="$emit(\'confirm\')">confirm</button></div>',
}

function mountView() {
  return mount(AIOpsView, {
    global: {
      mocks: { $t: (key) => key },
      stubs: {
        PageShell: { template: '<div><slot /></div>' },
        MetricsCard: { template: '<div><slot /></div>' },
        ConfirmModal: ConfirmStub,
        Button: ButtonStub,
      },
    },
  })
}

async function openNLExecution(wrapper) {
  wrapper.vm.activeTab = 'nl'
  wrapper.vm.nlPlan = { commands: [] }
  await wrapper.vm.$nextTick()
  await wrapper.find('[data-testid="aiops-execute-nl-btn"]').trigger('click')
  await wrapper.vm.$nextTick()
}

describe('AIOps 执行防重入', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('确认按钮连点只执行一次，pending 禁用，完成后恢复并反馈', async () => {
    let resolveExecute
    mockExecute.mockImplementation(() => new Promise((resolve) => { resolveExecute = resolve }))
    const wrapper = mountView()
    await flushPromises()
    await openNLExecution(wrapper)

    const confirm = wrapper.find('[data-testid="execute-confirm"]')
    await confirm.trigger('click')
    await confirm.trigger('click')
    expect(mockExecute).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="aiops-execute-nl-btn"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('aiops.executing')

    resolveExecute({ results: [], dispatched: 0, total: 0 })
    await flushPromises()
    expect(mockSuccess).toHaveBeenCalledWith('aiops.execute_success')
    // 成功后视图切到执行结果 tab；切回 NL 验证执行锁已释放。
    wrapper.vm.activeTab = 'nl'
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-testid="aiops-execute-nl-btn"]').attributes('disabled')).toBeUndefined()
  })

  it('失败后也释放执行锁并显示错误', async () => {
    mockExecute.mockRejectedValueOnce({ j: { error: 'denied' } })
    const wrapper = mountView()
    await flushPromises()
    await openNLExecution(wrapper)
    await wrapper.find('[data-testid="execute-confirm"]').trigger('click')
    await flushPromises()

    expect(mockError).toHaveBeenCalledWith('denied')
    expect(wrapper.find('[data-testid="aiops-execute-nl-btn"]').attributes('disabled')).toBeUndefined()
  })
})
