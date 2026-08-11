// ProgressRing 组件单元测试
// 覆盖：组件渲染、value 显示与边界限制、label、size、strokeWidth、
//       颜色阈值切换（ok/warn/fail）、自定义 color、SVG 几何参数。
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import ProgressRing from '@/components/ProgressRing.vue'

describe('ProgressRing 组件', () => {
  describe('组件渲染', () => {
    it('挂载成功，包含 .ring 容器', () => {
      const wrapper = mount(ProgressRing)
      expect(wrapper.find('.ring').exists()).toBe(true)
    })

    it('渲染 svg 与两个 circle（背景 + 进度）', () => {
      const wrapper = mount(ProgressRing)
      expect(wrapper.find('svg').exists()).toBe(true)
      expect(wrapper.findAll('circle')).toHaveLength(2)
    })

    it('渲染百分比文字 .ring-val', () => {
      const wrapper = mount(ProgressRing)
      expect(wrapper.find('.ring-val').exists()).toBe(true)
    })
  })

  describe('value 显示与边界', () => {
    it('默认 value=0 显示 0.0%', () => {
      const wrapper = mount(ProgressRing)
      expect(wrapper.find('.ring-val').text()).toContain('0.0')
    })

    it('value=50 显示 50.0%', () => {
      const wrapper = mount(ProgressRing, { props: { value: 50 } })
      expect(wrapper.find('.ring-val').text()).toContain('50.0')
    })

    it('value=100 显示 100%（toFixed(0)）', () => {
      const wrapper = mount(ProgressRing, { props: { value: 100 } })
      expect(wrapper.find('.ring-val').text()).toContain('100')
      expect(wrapper.find('.ring-val').text()).not.toContain('100.0')
    })

    it('value=85.5 显示 85.5%', () => {
      const wrapper = mount(ProgressRing, { props: { value: 85.5 } })
      expect(wrapper.find('.ring-val').text()).toContain('85.5')
    })

    it('value > 100 限制为 100', () => {
      const wrapper = mount(ProgressRing, { props: { value: 150 } })
      expect(wrapper.find('.ring-val').text()).toContain('100')
    })

    it('value < 0 限制为 0', () => {
      const wrapper = mount(ProgressRing, { props: { value: -20 } })
      expect(wrapper.find('.ring-val').text()).toContain('0.0')
    })

    it('value 为 null/undefined 时按 0 处理', () => {
      const wrapper = mount(ProgressRing, { props: { value: null } })
      expect(wrapper.find('.ring-val').text()).toContain('0.0')
    })
  })

  describe('label prop', () => {
    it('label 设置时渲染 .ring-label', () => {
      const wrapper = mount(ProgressRing, { props: { label: 'CPU' } })
      expect(wrapper.find('.ring-label').exists()).toBe(true)
      expect(wrapper.find('.ring-label').text()).toBe('CPU')
    })

    it('label 为空时不渲染 .ring-label', () => {
      const wrapper = mount(ProgressRing)
      expect(wrapper.find('.ring-label').exists()).toBe(false)
    })
  })

  describe('size 与 strokeWidth', () => {
    it('size 控制 .ring 宽高与 svg 宽高', () => {
      const wrapper = mount(ProgressRing, { props: { size: 200 } })
      const ring = wrapper.find('.ring')
      expect(ring.attributes('style')).toContain('width: 200px')
      expect(ring.attributes('style')).toContain('height: 200px')
      expect(wrapper.find('svg').attributes('width')).toBe('200')
      expect(wrapper.find('svg').attributes('height')).toBe('200')
    })

    it('默认 size=120', () => {
      const wrapper = mount(ProgressRing)
      expect(wrapper.find('svg').attributes('width')).toBe('120')
    })

    it('strokeWidth 传递到 circle stroke-width', () => {
      const wrapper = mount(ProgressRing, { props: { strokeWidth: 8 } })
      const circles = wrapper.findAll('circle')
      expect(circles[0].attributes('stroke-width')).toBe('8')
      expect(circles[1].attributes('stroke-width')).toBe('8')
    })
  })

  describe('颜色阈值切换', () => {
    it('value < warnAt(60) → var(--ok)', () => {
      const wrapper = mount(ProgressRing, { props: { value: 30 } })
      const circles = wrapper.findAll('circle')
      expect(circles[1].attributes('stroke')).toBe('var(--ok)')
    })

    it('value >= warnAt(60) 且 < dangerAt(85) → var(--warn)', () => {
      const wrapper = mount(ProgressRing, { props: { value: 70 } })
      const circles = wrapper.findAll('circle')
      expect(circles[1].attributes('stroke')).toBe('var(--warn)')
    })

    it('value >= dangerAt(85) → var(--fail)', () => {
      const wrapper = mount(ProgressRing, { props: { value: 90 } })
      const circles = wrapper.findAll('circle')
      expect(circles[1].attributes('stroke')).toBe('var(--fail)')
    })

    it('value=85 边界 → var(--fail)', () => {
      const wrapper = mount(ProgressRing, { props: { value: 85 } })
      const circles = wrapper.findAll('circle')
      expect(circles[1].attributes('stroke')).toBe('var(--fail)')
    })

    it('value=60 边界 → var(--warn)', () => {
      const wrapper = mount(ProgressRing, { props: { value: 60 } })
      const circles = wrapper.findAll('circle')
      expect(circles[1].attributes('stroke')).toBe('var(--warn)')
    })

    it('自定义 color 覆盖阈值判定', () => {
      const wrapper = mount(ProgressRing, {
        props: { value: 90, color: '#ff00ff' },
      })
      const circles = wrapper.findAll('circle')
      expect(circles[1].attributes('stroke')).toBe('#ff00ff')
    })

    it('自定义 color 优先于 ok 阈值', () => {
      const wrapper = mount(ProgressRing, {
        props: { value: 10, color: '#123456' },
      })
      const circles = wrapper.findAll('circle')
      expect(circles[1].attributes('stroke')).toBe('#123456')
    })

    it('背景轨道色为 var(--surface-3)', () => {
      const wrapper = mount(ProgressRing)
      const circles = wrapper.findAll('circle')
      expect(circles[0].attributes('stroke')).toBe('var(--surface-3)')
    })
  })
})