// StatusBadge 组件单元测试
// 覆盖：组件渲染、status → class 映射、text prop、slot 覆盖、边界条件。
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import StatusBadge from '@/components/StatusBadge.vue'

describe('StatusBadge 组件', () => {
  describe('组件渲染', () => {
    it('挂载成功，包含 .badge 元素', () => {
      const wrapper = mount(StatusBadge)
      expect(wrapper.find('.badge').exists()).toBe(true)
    })

    it('渲染为 span 元素', () => {
      const wrapper = mount(StatusBadge)
      expect(wrapper.find('span.badge').exists()).toBe(true)
    })
  })

  describe('status → class 映射', () => {
    it('默认 status 为空时 class 为 info', () => {
      const wrapper = mount(StatusBadge)
      expect(wrapper.find('.badge').classes()).toContain('info')
    })

    it('status=ok/success/done/managed/acknowledged → ok', () => {
      for (const s of ['ok', 'success', 'done', 'managed', 'acknowledged']) {
        const wrapper = mount(StatusBadge, { props: { status: s } })
        expect(wrapper.find('.badge').classes()).toContain('ok')
        expect(wrapper.find('.badge').classes()).not.toContain('fail')
      }
    })

    it('status=fail/failed/error/critical → fail', () => {
      for (const s of ['fail', 'failed', 'error', 'critical']) {
        const wrapper = mount(StatusBadge, { props: { status: s } })
        expect(wrapper.find('.badge').classes()).toContain('fail')
      }
    })

    it('status=warn/warning/running/rolledback → warn', () => {
      for (const s of ['warn', 'warning', 'running', 'rolledback']) {
        const wrapper = mount(StatusBadge, { props: { status: s } })
        expect(wrapper.find('.badge').classes()).toContain('warn')
      }
    })

    it('status=info/pending/created/draft/discovered/silenced → info', () => {
      for (const s of ['info', 'pending', 'created', 'draft', 'discovered', 'silenced']) {
        const wrapper = mount(StatusBadge, { props: { status: s } })
        expect(wrapper.find('.badge').classes()).toContain('info')
      }
    })

    it('未知 status 回退到 info', () => {
      const wrapper = mount(StatusBadge, { props: { status: 'unknown-xyz' } })
      expect(wrapper.find('.badge').classes()).toContain('info')
    })
  })

  describe('text prop 与 slot', () => {
    it('text prop 渲染为徽章内容', () => {
      const wrapper = mount(StatusBadge, { props: { text: '运行中' } })
      expect(wrapper.find('.badge').text()).toBe('运行中')
    })

    it('默认 text 为空字符串', () => {
      const wrapper = mount(StatusBadge)
      expect(wrapper.find('.badge').text()).toBe('')
    })

    it('slot 内容覆盖 text prop', () => {
      const wrapper = mount(StatusBadge, {
        props: { text: 'prop-text' },
        slots: { default: 'slot-text' },
      })
      expect(wrapper.find('.badge').text()).toBe('slot-text')
    })

    it('slot 可包含 HTML 元素', () => {
      const wrapper = mount(StatusBadge, {
        slots: { default: '<strong class="custom">重要</strong>' },
      })
      expect(wrapper.find('.badge .custom').exists()).toBe(true)
      expect(wrapper.find('.badge .custom').text()).toBe('重要')
    })
  })
})