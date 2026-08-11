// DetailDrawer 组件单元测试
// 覆盖：组件渲染、open prop 控制显隐、title 渲染、默认插槽、close 事件。
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import DetailDrawer from '@/components/DetailDrawer.vue'

describe('DetailDrawer 组件', () => {
  describe('组件渲染与 open prop', () => {
    it('挂载成功（默认 open=false 不渲染 drawer）', () => {
      const wrapper = mount(DetailDrawer)
      expect(wrapper.find('.drawer').exists()).toBe(false)
    })

    it('open=true 时渲染 .drawer 元素', () => {
      const wrapper = mount(DetailDrawer, { props: { open: true } })
      expect(wrapper.find('.drawer').exists()).toBe(true)
    })

    it('open=false 时不渲染 .drawer 元素', () => {
      const wrapper = mount(DetailDrawer, { props: { open: false } })
      expect(wrapper.find('.drawer').exists()).toBe(false)
    })

    it('从 open=false 切换到 open=true 后渲染 drawer', async () => {
      const wrapper = mount(DetailDrawer, { props: { open: false } })
      expect(wrapper.find('.drawer').exists()).toBe(false)
      await wrapper.setProps({ open: true })
      expect(wrapper.find('.drawer').exists()).toBe(true)
    })
  })

  describe('title prop', () => {
    it('title 渲染到 h3', () => {
      const wrapper = mount(DetailDrawer, {
        props: { open: true, title: '设备详情' },
      })
      expect(wrapper.find('.drawer-head h3').text()).toBe('设备详情')
    })

    it('默认 title 为空字符串', () => {
      const wrapper = mount(DetailDrawer, { props: { open: true } })
      expect(wrapper.find('.drawer-head h3').text()).toBe('')
    })
  })

  describe('默认插槽', () => {
    it('插槽内容渲染到 .drawer-body', () => {
      const wrapper = mount(DetailDrawer, {
        props: { open: true },
        slots: { default: '<p class="content">内容</p>' },
      })
      expect(wrapper.find('.drawer-body .content').exists()).toBe(true)
      expect(wrapper.find('.drawer-body .content').text()).toBe('内容')
    })

    it('插槽可包含多个元素', () => {
      const wrapper = mount(DetailDrawer, {
        props: { open: true },
        slots: { default: '<div class="a">A</div><div class="b">B</div>' },
      })
      expect(wrapper.find('.drawer-body .a').exists()).toBe(true)
      expect(wrapper.find('.drawer-body .b').exists()).toBe(true)
    })
  })

  describe('close 事件', () => {
    it('点击关闭按钮触发 close 事件', async () => {
      const wrapper = mount(DetailDrawer, { props: { open: true } })
      await wrapper.find('.drawer-head button').trigger('click')
      expect(wrapper.emitted('close')).toBeTruthy()
      expect(wrapper.emitted('close')).toHaveLength(1)
    })

    it('关闭按钮文本为 ✕', () => {
      const wrapper = mount(DetailDrawer, { props: { open: true } })
      expect(wrapper.find('.drawer-head button').text()).toBe('✕')
    })

    it('多次点击多次触发 close', async () => {
      const wrapper = mount(DetailDrawer, { props: { open: true } })
      const btn = wrapper.find('.drawer-head button')
      await btn.trigger('click')
      await btn.trigger('click')
      expect(wrapper.emitted('close')).toHaveLength(2)
    })
  })
})