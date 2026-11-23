import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import DetailDrawer from '@/components/DetailDrawer.vue'

let wrappers = []
afterEach(() => {
  wrappers.forEach(w => w.unmount())
  wrappers = []
  document.body.innerHTML = ''
})

function mountDrawer(props = {}, slots = {}) {
  const wrapper = mount(DetailDrawer, {
    props,
    slots,
    attachTo: document.body,
  })
  wrappers.push(wrapper)
  return wrapper
}

describe('DetailDrawer 无障碍契约', () => {
  it('dialog 的标题/描述引用有效，关闭按钮有可访问名称', () => {
    mountDrawer({ open: true, title: '设备详情' })
    const dialog = document.body.querySelector('[role="dialog"]')
    expect(dialog.getAttribute('aria-modal')).toBe('true')
    expect(document.getElementById(dialog.getAttribute('aria-labelledby')).textContent).toBe('设备详情')
    expect(document.getElementById(dialog.getAttribute('aria-describedby')).classList.contains('drawer-body')).toBe(true)
    expect(document.body.querySelector('.drawer-close').getAttribute('aria-label')).toBe('Close')
  })

  it('初次打开即可 ESC 关闭，Tab 从末项回卷到首项', async () => {
    const wrapper = mountDrawer(
      { open: true, title: '详情' },
      { default: '<button class="last-action">保存</button>' },
    )
    await nextTick()
    const last = document.body.querySelector('.last-action')
    last.focus()
    last.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true }))
    expect(document.activeElement.classList.contains('drawer-close')).toBe(true)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('打开时锁定背景滚动，遮罩关闭后恢复原样式', async () => {
    document.body.style.overflow = 'auto'
    const wrapper = mountDrawer({ open: false })
    await wrapper.setProps({ open: true })
    await nextTick()
    expect(document.body.style.overflow).toBe('hidden')
    document.body.querySelector('.drawer-overlay').dispatchEvent(new MouseEvent('click', { bubbles: true }))
    expect(wrapper.emitted('close')).toBeTruthy()
    await wrapper.setProps({ open: false })
    await nextTick()
    expect(document.body.style.overflow).toBe('auto')
  })

  it('关闭后焦点还原到打开前元素', async () => {
    const trigger = document.createElement('button')
    trigger.textContent = '打开'
    document.body.appendChild(trigger)
    trigger.focus()
    const wrapper = mountDrawer({ open: false })
    await wrapper.setProps({ open: true })
    await nextTick()
    await wrapper.setProps({ open: false })
    await nextTick()
    expect(document.activeElement).toBe(trigger)
  })
})
