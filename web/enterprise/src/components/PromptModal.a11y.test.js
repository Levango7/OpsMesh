import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import PromptModal from '@/components/PromptModal.vue'

let wrappers = []
afterEach(() => {
  wrappers.forEach(w => w.unmount())
  wrappers = []
  document.body.innerHTML = ''
})

function mountPrompt(props = {}) {
  const wrapper = mount(PromptModal, {
    props,
    global: { mocks: { $t: key => key } },
    attachTo: document.body,
  })
  wrappers.push(wrapper)
  return wrapper
}

describe('PromptModal 无障碍契约', () => {
  it('dialog/输入框有名称，初次打开即应用默认值', async () => {
    mountPrompt({ modelValue: true, title: '重命名', message: '新名称', defaultValue: 'node-1' })
    await nextTick()
    const dialog = document.body.querySelector('[role="dialog"]')
    expect(document.getElementById(dialog.getAttribute('aria-labelledby')).textContent).toBe('重命名')
    expect(document.getElementById(dialog.getAttribute('aria-describedby')).textContent).toBe('新名称')
    expect(document.body.querySelector('input').value).toBe('node-1')
    expect(document.body.querySelector('input').getAttribute('aria-label')).toBe('新名称')
  })

  it('初次打开可 ESC 取消，Tab 从确认按钮回卷到输入框', async () => {
    const wrapper = mountPrompt({ modelValue: true, title: '重命名' })
    await nextTick()
    const last = document.body.querySelector('[data-testid="prompt-modal-confirm"]')
    last.focus()
    last.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true }))
    expect(document.activeElement.getAttribute('data-testid')).toBe('prompt-modal-input')
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    expect(wrapper.emitted('cancel')).toBeTruthy()
  })

  it('关闭后焦点还原到触发按钮', async () => {
    const trigger = document.createElement('button')
    document.body.appendChild(trigger)
    trigger.focus()
    const wrapper = mountPrompt({ modelValue: false })
    await wrapper.setProps({ modelValue: true })
    await nextTick()
    await wrapper.setProps({ modelValue: false })
    await nextTick()
    expect(document.activeElement).toBe(trigger)
  })
})
