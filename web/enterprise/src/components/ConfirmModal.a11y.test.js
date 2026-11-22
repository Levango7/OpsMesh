import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import ConfirmModal from '@/components/ConfirmModal.vue'

let wrappers = []
afterEach(() => {
  wrappers.forEach(w => w.unmount())
  wrappers = []
  document.body.innerHTML = ''
})

function mountConfirm(props = {}, attrs = {}) {
  const wrapper = mount(ConfirmModal, {
    props,
    attrs,
    global: { mocks: { $t: key => key } },
    attachTo: document.body,
  })
  wrappers.push(wrapper)
  return wrapper
}

describe('ConfirmModal 焦点与键盘契约', () => {
  it('初次挂载打开态即可 ESC 取消', async () => {
    const wrapper = mountConfirm({ modelValue: true, title: '确认' })
    await nextTick()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    expect(wrapper.emitted('cancel')).toBeTruthy()
  })

  it('Tab 从最后的确认按钮回卷到第一个取消按钮', async () => {
    mountConfirm({ modelValue: true, title: '确认' })
    await nextTick()
    const last = document.body.querySelector('[data-testid="confirm-modal-confirm"]')
    last.focus()
    last.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true }))
    expect(document.activeElement.getAttribute('data-testid')).toBe('confirm-modal-cancel')
  })

  it('页面可覆盖 testId 以区分多个确认框', () => {
    mountConfirm({ modelValue: true, testId: 'delete-user-confirm' })
    expect(document.body.querySelector('[data-testid="delete-user-confirm"]')).not.toBeNull()
    expect(document.body.querySelector('[data-testid="confirm-modal"]')).toBeNull()
  })

  it('原生 data-testid 也会显式转发到 Teleport 遮罩', () => {
    mountConfirm({ modelValue: true }, { 'data-testid': 'native-delete-confirm' })
    expect(document.body.querySelector('[data-testid="native-delete-confirm"]')).not.toBeNull()
  })

  it('info 模式仍困住焦点但 ESC 不关闭', async () => {
    const wrapper = mountConfirm({ modelValue: true, info: true })
    await nextTick()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    expect(wrapper.emitted('cancel')).toBeFalsy()
    expect(wrapper.emitted('update:modelValue')).toBeFalsy()
  })
})
