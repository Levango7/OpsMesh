// Icon 组件单元测试
// 覆盖：组件渲染、name → path 映射、size、未知图标回退、SVG 属性（viewBox/fill/aria-hidden）。
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import Icon from '@/components/Icon.vue'

// 已知图标名（来自组件内 icons 字典）
const knownIcons = [
  'home', 'ops', 'cmdb', 'deploy', 'flow', 'logs', 'alerts', 'users',
  'roles', 'permissions', 'login', 'register',
  'theme-light', 'theme-dark', 'lang', 'logout', 'settings',
  'device', 'task', 'success', 'fail', 'warning',
  'refresh', 'search', 'add', 'edit', 'delete', 'close',
  'mute', 'save', 'clock', 'arrow-left', 'arrow-right', 'clipboard', 'brand',
]

describe('Icon 组件', () => {
  describe('组件渲染', () => {
    it('挂载成功，渲染 svg 元素', () => {
      const wrapper = mount(Icon, { props: { name: 'home' } })
      expect(wrapper.find('svg').exists()).toBe(true)
    })

    it('svg 内含 path 元素', () => {
      const wrapper = mount(Icon, { props: { name: 'home' } })
      expect(wrapper.find('svg path').exists()).toBe(true)
    })

    it('svg viewBox 固定为 0 0 24 24', () => {
      const wrapper = mount(Icon, { props: { name: 'home' } })
      expect(wrapper.find('svg').attributes('viewBox')).toBe('0 0 24 24')
    })

    it('svg fill 为 currentColor（继承文字颜色）', () => {
      const wrapper = mount(Icon, { props: { name: 'home' } })
      expect(wrapper.find('svg').attributes('fill')).toBe('currentColor')
    })

    it('svg aria-hidden 为 true（装饰性图标）', () => {
      const wrapper = mount(Icon, { props: { name: 'home' } })
      expect(wrapper.find('svg').attributes('aria-hidden')).toBe('true')
    })

    it('svg class 包含 icon', () => {
      const wrapper = mount(Icon, { props: { name: 'home' } })
      expect(wrapper.find('svg').classes()).toContain('icon')
    })
  })

  describe('size prop', () => {
    it('默认 size=18', () => {
      const wrapper = mount(Icon, { props: { name: 'home' } })
      expect(wrapper.find('svg').attributes('width')).toBe('18')
      expect(wrapper.find('svg').attributes('height')).toBe('18')
    })

    it('自定义 size 传递到 svg width/height', () => {
      const wrapper = mount(Icon, { props: { name: 'home', size: 24 } })
      expect(wrapper.find('svg').attributes('width')).toBe('24')
      expect(wrapper.find('svg').attributes('height')).toBe('24')
    })

    it('size=1 也能正确传递', () => {
      const wrapper = mount(Icon, { props: { name: 'home', size: 1 } })
      expect(wrapper.find('svg').attributes('width')).toBe('1')
    })
  })

  describe('name → path 映射', () => {
    it('已知图标渲染非空 path d 属性', () => {
      for (const name of knownIcons) {
        const wrapper = mount(Icon, { props: { name } })
        const d = wrapper.find('svg path').attributes('d')
        expect(d, `图标 ${name} 的 path d 不应为空`).toBeTruthy()
        expect(d.length, `图标 ${name} 的 path d 长度应 > 0`).toBeGreaterThan(0)
      }
    })

    it('不同图标渲染不同 path', () => {
      const w1 = mount(Icon, { props: { name: 'home' } })
      const w2 = mount(Icon, { props: { name: 'ops' } })
      expect(w1.find('svg path').attributes('d')).not.toBe(
        w2.find('svg path').attributes('d')
      )
    })

    it('未知图标回退到 home 的 path', () => {
      const wUnknown = mount(Icon, { props: { name: 'non-existent-icon' } })
      const wHome = mount(Icon, { props: { name: 'home' } })
      expect(wUnknown.find('svg path').attributes('d')).toBe(
        wHome.find('svg path').attributes('d')
      )
    })

    it('空字符串 name 回退到 home', () => {
      const wEmpty = mount(Icon, { props: { name: '' } })
      const wHome = mount(Icon, { props: { name: 'home' } })
      expect(wEmpty.find('svg path').attributes('d')).toBe(
        wHome.find('svg path').attributes('d')
      )
    })
  })
})