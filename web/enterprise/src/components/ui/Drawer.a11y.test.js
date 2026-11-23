import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import Drawer from '@/components/ui/Drawer.vue'

let wrappers = []
afterEach(() => {
  wrappers.forEach(w => w.unmount())
  wrappers = []
  document.body.innerHTML = ''
})

describe('ui/Drawer ARIA 树', () => {
  it('遮罩不隐藏内部 dialog，描述 id 指向真实 body', () => {
    const wrapper = mount(Drawer, {
      props: { modelValue: true, title: '详情' },
      slots: { default: '<p>正文</p>' },
      attachTo: document.body,
    })
    wrappers.push(wrapper)
    const mask = document.body.querySelector('.drawer-mask')
    const dialog = document.body.querySelector('[role="dialog"]')
    expect(mask.hasAttribute('aria-hidden')).toBe(false)
    expect(dialog.getAttribute('aria-modal')).toBe('true')
    expect(document.getElementById(dialog.getAttribute('aria-labelledby')).textContent).toBe('详情')
    expect(document.getElementById(dialog.getAttribute('aria-describedby')).classList.contains('drawer__body')).toBe(true)
  })
})
