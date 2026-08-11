// PromptModal 组件单元测试
// 覆盖：组件渲染、modelValue 显隐、title/message/placeholder、defaultValue 填入、
//       confirm(value)/cancel 事件、update:modelValue、回车确认。
// 组件使用 Teleport to="body"，需通过 document.body 查询实际渲染内容。
import { describe, it, expect, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import PromptModal from '@/components/PromptModal.vue'

// mock $t：返回键本身
const mockT = (key) => key

let wrappers = []
afterEach(() => {
  wrappers.forEach((w) => w.unmount())
  wrappers = []
  document.body.innerHTML = ''
})

function mountModal(props = {}) {
  const w = mount(PromptModal, {
    props,
    global: { mocks: { $t: mockT } },
  })
  wrappers.push(w)
  return w
}

function bodyFind(selector) {
  return document.body.querySelector(selector)
}
function bodyFindAll(selector) {
  return document.body.querySelectorAll(selector)
}

describe('PromptModal 组件', () => {
  describe('组件渲染与显隐', () => {
    it('挂载成功（modelValue=false 时不显示）', () => {
      mountModal({ modelValue: false })
      expect(bodyFind('.modal-overlay')).toBeNull()
    })

    it('modelValue=true 时显示弹窗', () => {
      mountModal({ modelValue: true, title: 'T' })
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

  describe('title / message / placeholder', () => {
    it('title 渲染到 .modal-title', () => {
      mountModal({ modelValue: true, title: '重命名' })
      expect(bodyFind('.modal-title').textContent).toBe('重命名')
    })

    it('message 非空时渲染 .modal-message', () => {
      mountModal({ modelValue: true, message: '请输入新名称' })
      expect(bodyFind('.modal-message').textContent).toBe('请输入新名称')
    })

    it('message 为空时不渲染 .modal-message', () => {
      mountModal({ modelValue: true, message: '' })
      expect(bodyFind('.modal-message')).toBeNull()
    })

    it('placeholder 传递到 input', () => {
      mountModal({ modelValue: true, placeholder: '输入设备名' })
      expect(bodyFind('.modal-input').getAttribute('placeholder')).toBe('输入设备名')
    })
  })

  describe('defaultValue 与输入', () => {
    it('打开时 defaultValue 填入输入框', async () => {
      // watch 在 modelValue 由 false→true 时填入 defaultValue，需先 mount false 再切换
      const wrapper = mountModal({ modelValue: false, defaultValue: '默认值' })
      await nextTick()
      await wrapper.setProps({ modelValue: true })
      await nextTick()
      expect(bodyFind('.modal-input').value).toBe('默认值')
    })

    it('未传 defaultValue 时输入框为空', async () => {
      mountModal({ modelValue: true })
      await nextTick()
      expect(bodyFind('.modal-input').value).toBe('')
    })

    it('用户输入后值更新', async () => {
      mountModal({ modelValue: true })
      await nextTick()
      const input = bodyFind('.modal-input')
      input.value = '新输入'
      input.dispatchEvent(new Event('input', { bubbles: true }))
      await nextTick()
      expect(input.value).toBe('新输入')
    })
  })

  describe('confirm 事件', () => {
    it('点击确认按钮触发 confirm(value) 并关闭', async () => {
      const wrapper = mountModal({ modelValue: false, defaultValue: 'abc' })
      await nextTick()
      await wrapper.setProps({ modelValue: true })
      await nextTick()
      const btn = bodyFind('.modal-actions button.primary')
      btn.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await nextTick()
      expect(wrapper.emitted('confirm')).toBeTruthy()
      expect(wrapper.emitted('confirm')[0][0]).toBe('abc')
      expect(wrapper.emitted('update:modelValue')[0][0]).toBe(false)
    })

    it('输入框回车触发 confirm', async () => {
      const wrapper = mountModal({ modelValue: false, defaultValue: 'xyz' })
      await nextTick()
      await wrapper.setProps({ modelValue: true })
      await nextTick()
      const input = bodyFind('.modal-input')
      input.dispatchEvent(new KeyboardEvent('keyup', { key: 'Enter', bubbles: true }))
      await nextTick()
      expect(wrapper.emitted('confirm')).toBeTruthy()
      expect(wrapper.emitted('confirm')[0][0]).toBe('xyz')
    })

    it('修改输入后 confirm 携带新值', async () => {
      const wrapper = mountModal({ modelValue: true, defaultValue: 'old' })
      await nextTick()
      const input = bodyFind('.modal-input')
      input.value = 'new'
      input.dispatchEvent(new Event('input', { bubbles: true }))
      await nextTick()
      const btn = bodyFind('.modal-actions button.primary')
      btn.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await nextTick()
      expect(wrapper.emitted('confirm')[0][0]).toBe('new')
    })
  })

  describe('cancel 事件', () => {
    it('点击取消按钮触发 cancel 并关闭', async () => {
      const wrapper = mountModal({ modelValue: true })
      await nextTick()
      const btns = bodyFindAll('.modal-actions button')
      btns[0].dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await nextTick()
      expect(wrapper.emitted('cancel')).toBeTruthy()
      expect(wrapper.emitted('update:modelValue')[0][0]).toBe(false)
    })

    it('点击遮罩触发 cancel 并关闭', async () => {
      const wrapper = mountModal({ modelValue: true })
      await nextTick()
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
    })
  })

  describe('自定义按钮文本', () => {
    it('confirmText 覆盖默认确认文本', () => {
      mountModal({ modelValue: true, confirmText: '保存' })
      const btn = bodyFind('.modal-actions button.primary')
      expect(btn.textContent).toBe('保存')
    })

    it('cancelText 覆盖默认取消文本', () => {
      mountModal({ modelValue: true, cancelText: '放弃' })
      const btns = bodyFindAll('.modal-actions button')
      expect(btns[0].textContent).toBe('放弃')
    })
  })
})