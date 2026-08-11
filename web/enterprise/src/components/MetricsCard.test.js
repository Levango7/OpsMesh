// MetricsCard 组件单元测试
// 覆盖：组件渲染、title 必填、icon 显隐、accent 内联样式、默认插槽、actions 插槽。
// MetricsCard 内部使用 Icon 组件，这里真实渲染（非 stub），便于验证 icon prop 传递。
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import MetricsCard from '@/components/MetricsCard.vue'

describe('MetricsCard 组件', () => {
  describe('组件渲染', () => {
    it('挂载成功，包含 .metrics-card', () => {
      const wrapper = mount(MetricsCard, { props: { title: '卡片' } })
      expect(wrapper.find('.metrics-card').exists()).toBe(true)
    })

    it('渲染为 section 元素', () => {
      const wrapper = mount(MetricsCard, { props: { title: '卡片' } })
      expect(wrapper.find('section.metrics-card').exists()).toBe(true)
    })
  })

  describe('title prop', () => {
    it('title 渲染到 h3', () => {
      const wrapper = mount(MetricsCard, { props: { title: '设备总数' } })
      expect(wrapper.find('.mc-title h3').text()).toBe('设备总数')
    })

    it('title 为空字符串时 h3 也为空', () => {
      const wrapper = mount(MetricsCard, { props: { title: '' } })
      expect(wrapper.find('.mc-title h3').text()).toBe('')
    })
  })

  describe('icon prop', () => {
    it('icon 设置时渲染 .mc-icon 容器', () => {
      const wrapper = mount(MetricsCard, {
        props: { title: '卡片', icon: 'device' },
      })
      expect(wrapper.find('.mc-icon').exists()).toBe(true)
    })

    it('icon 为空时不渲染 .mc-icon', () => {
      const wrapper = mount(MetricsCard, {
        props: { title: '卡片', icon: '' },
      })
      expect(wrapper.find('.mc-icon').exists()).toBe(false)
    })

    it('icon 设置时内部渲染 svg（Icon 组件）', () => {
      const wrapper = mount(MetricsCard, {
        props: { title: '卡片', icon: 'home' },
      })
      expect(wrapper.find('.mc-icon svg').exists()).toBe(true)
    })

    it('默认 icon 为空，不渲染图标', () => {
      const wrapper = mount(MetricsCard, { props: { title: '卡片' } })
      expect(wrapper.find('.mc-icon').exists()).toBe(false)
    })
  })

  describe('accent prop', () => {
    it('accent 设置时 .mc-icon 有内联 color 样式', () => {
      const wrapper = mount(MetricsCard, {
        props: { title: '卡片', icon: 'home', accent: '--indigo' },
      })
      const style = wrapper.find('.mc-icon').attributes('style')
      expect(style).toContain('color')
      expect(style).toContain('var(--indigo)')
    })

    it('accent 为空时 .mc-icon 无内联 color 样式', () => {
      const wrapper = mount(MetricsCard, {
        props: { title: '卡片', icon: 'home', accent: '' },
      })
      const style = wrapper.find('.mc-icon').attributes('style')
      expect(style).toBeUndefined()
    })
  })

  describe('默认插槽', () => {
    it('插槽内容渲染到 .mc-body', () => {
      const wrapper = mount(MetricsCard, {
        props: { title: '卡片' },
        slots: { default: '<p class="body-content">内容</p>' },
      })
      expect(wrapper.find('.mc-body .body-content').exists()).toBe(true)
      expect(wrapper.find('.mc-body .body-content').text()).toBe('内容')
    })

    it('未提供插槽时 .mc-body 为空', () => {
      const wrapper = mount(MetricsCard, { props: { title: '卡片' } })
      expect(wrapper.find('.mc-body').text()).toBe('')
    })
  })

  describe('actions 插槽', () => {
    it('actions 插槽渲染到 .mc-actions', () => {
      const wrapper = mount(MetricsCard, {
        props: { title: '卡片' },
        slots: { actions: '<button class="act-btn">操作</button>' },
      })
      expect(wrapper.find('.mc-actions .act-btn').exists()).toBe(true)
      expect(wrapper.find('.mc-actions .act-btn').text()).toBe('操作')
    })

    it('未提供 actions 插槽时不渲染 .mc-actions', () => {
      const wrapper = mount(MetricsCard, { props: { title: '卡片' } })
      expect(wrapper.find('.mc-actions').exists()).toBe(false)
    })
  })
})