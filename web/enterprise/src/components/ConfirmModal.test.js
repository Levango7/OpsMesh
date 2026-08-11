// ConfirmModal 组件单元测试
// 覆盖：组件渲染、modelValue 显隐、title/message、普通/info 模式按钮、
//       confirm/cancel 事件、update:modelValue、遮罩点击、ESC 关闭、自定义按钮文本。
// 组件使用 Teleport to="body"，需通过 document.body 查询实际渲染内容。
import { describe, it, expect, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import ConfirmModal from '@/components/ConfirmModal.vue'

// mock $t：返回键本身，便于断言
const mockT = (key) => key

// 收集所有 wrapper，测试后统一清理（Teleport 内容挂载到 body，需 unmount 移除）
let wrappers = []
afterEach(() => {
  wrappers.forEach((w) => w.unmount())
  wrappers = []
  document.body.innerHTML = ''
})

function mountModal(props = {}) {
  const w = mount(ConfirmModal, {
    props,
    global: { mocks: { $t: mockT } },
  })
  wrappers.push(w)
  return w
}

// 从 document.body 查询元素（Teleport 目标）
function bodyFind(selector) {
  return document.body.querySelector(selector)
}
function bodyFindAll(selector) {
  return document.body.querySelectorAll(selector)
}

describe('ConfirmModal 组件', () => {
  describe('组件渲染与显隐', () => {
    it('挂载成功（modelValue=false 时不显示遮罩）', () => {
      mountModal({ modelValue: false })
      expect(bodyFind('.modal-overlay')).toBeNull()
    })

    it('modelValue=true 时显示遮罩与弹窗', () => {
      mountModal({ modelValue: true, title: 'T', message: 'M' })
      expect(bodyFind('.modal-overlay')).not.toBeNull()
      expect(bodyFind('.modal-box')).not.toBeNull()
    })

    it('从 false 切换到 true 后显示', async () => {
      const wrapper = mountModal({ modelValue: false })
      expect(bodyFind('.modal-overlay')).toBeNull()
      await wrapper.setProps({ modelValue: true })
      expect(bodyFind('.modal-overlay')).not.toBeNull()
    })
  })

  describe('title 与 message', () => {
    it('title 渲染到 .modal-title', () => {
      mountModal({ modelValue: true, title: '确认删除' })
      expect(bodyFind('.modal-title').textContent).toBe('确认删除')
    })

    it('message 渲染到 .modal-message', () => {
      mountModal({ modelValue: true, message: '确定要删除这条记录吗？' })
      expect(bodyFind('.modal-message').textContent).toBe('确定要删除这条记录吗？')
    })

    it('默认 title/message 为空字符串', () => {
      mountModal({ modelValue: true })
      expect(bodyFind('.modal-title').textContent).toBe('')
      expect(bodyFind('.modal-message').textContent).toBe('')
    })
  })

  describe('普通模式（info=false）', () => {
    it('显示取消和确认两个按钮', () => {
      mountModal({ modelValue: true })
      const btns = bodyFindAll('.modal-actions button')
      expect(btns).toHaveLength(2)
    })

    it('默认按钮文本使用 $t（common.cancel / common.confirm）', () => {
      mountModal({ modelValue: true })
      const btns = bodyFindAll('.modal-actions button')
      expect(btns[0].textContent).toBe('common.cancel')
      expect(btns[1].textContent).toBe('common.confirm')
    })

    it('点击确认按钮触发 confirm 并关闭', async () => {
      const wrapper = mountModal({ modelValue: true })
      const confirmBtn = bodyFind('.modal-actions button.primary')
      confirmBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await nextTick()
      expect(wrapper.emitted('confirm')).toBeTruthy()
      expect(wrapper.emitted('update:modelValue')).toBeTruthy()
      expect(wrapper.emitted('update:modelValue')[0][0]).toBe(false)
    })

    it('点击取消按钮触发 cancel 并关闭', async () => {
      const wrapper = mountModal({ modelValue: true })
      const btns = bodyFindAll('.modal-actions button')
      btns[0].dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await nextTick()
      expect(wrapper.emitted('cancel')).toBeTruthy()
      expect(wrapper.emitted('update:modelValue')[0][0]).toBe(false)
    })

    it('点击遮罩触发 cancel 并关闭', async () => {
      const wrapper = mountModal({ modelValue: true })
      const overlay = bodyFind('.modal-overlay')
      overlay.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await nextTick()
      expect(wrapper.emitted('cancel')).toBeTruthy()
    })

    it('ESC 键触发 cancel 并关闭', async () => {
      // watch 在 modelValue 由 false→true 时注册 ESC 监听，需先 mount false 再切换
      const wrapper = mountModal({ modelValue: false })
      await nextTick()
      await wrapper.setProps({ modelValue: true })
      await nextTick()
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
      await nextTick()
      expect(wrapper.emitted('cancel')).toBeTruthy()
      expect(wrapper.emitted('update:modelValue')).toBeTruthy()
    })
  })

  describe('info 模式', () => {
    it('只显示确定按钮', () => {
      mountModal({ modelValue: true, info: true })
      const btns = bodyFindAll('.modal-actions button')
      expect(btns).toHaveLength(1)
      expect(btns[0].className).toContain('primary')
    })

    it('info 模式按钮文本使用 $t(common.ok)', () => {
      mountModal({ modelValue: true, info: true })
      const btn = bodyFind('.modal-actions button.primary')
      expect(btn.textContent).toBe('common.ok')
    })

    it('info 模式点击遮罩不关闭', async () => {
      const wrapper = mountModal({ modelValue: true, info: true })
      const overlay = bodyFind('.modal-overlay')
      overlay.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await nextTick()
      expect(wrapper.emitted('cancel')).toBeFalsy()
      expect(wrapper.emitted('update:modelValue')).toBeFalsy()
    })

    it('info 模式点击确定触发 confirm 并关闭', async () => {
      const wrapper = mountModal({ modelValue: true, info: true })
      const btn = bodyFind('.modal-actions button.primary')
      btn.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await nextTick()
      expect(wrapper.emitted('confirm')).toBeTruthy()
      expect(wrapper.emitted('update:modelValue')[0][0]).toBe(false)
    })

    it('info 模式 ESC 不关闭', async () => {
      const wrapper = mountModal({ modelValue: true, info: true })
      await nextTick()
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
      await nextTick()
      expect(wrapper.emitted('cancel')).toBeFalsy()
    })
  })

  describe('自定义按钮文本', () => {
    it('confirmText 覆盖默认确认文本', () => {
      mountModal({ modelValue: true, confirmText: '删除' })
      const btn = bodyFind('.modal-actions button.primary')
      expect(btn.textContent).toBe('删除')
    })

    it('cancelText 覆盖默认取消文本', () => {
      mountModal({ modelValue: true, cancelText: '再想想' })
      const btns = bodyFindAll('.modal-actions button')
      expect(btns[0].textContent).toBe('再想想')
    })
  })
})