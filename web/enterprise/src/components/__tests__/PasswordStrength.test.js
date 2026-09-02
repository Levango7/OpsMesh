// PasswordStrength 组件单元测试
// 覆盖：
//   - 组件挂载与基本结构
//   - password 为空时不渲染
//   - 不同强度密码的评分（弱/中/强）
//   - 进度条宽度（strengthPercent）
//   - strengthClass 变化（weak/medium/strong）
//   - 需求列表项 met 状态
//
// mock 策略：
//   - @/i18n：返回键本身，便于断言
//   - @/components/Icon.vue：替换为简单 span，避免图标渲染细节
//   - 全局 $t：通过 global.mocks 注入
import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'

// mock @/i18n：返回键本身
vi.mock('@/i18n', () => ({
  t: (key) => key,
  currentLang: { value: 'zh' },
  setLang: vi.fn(),
  initLang: vi.fn(),
  default: {},
}))

// mock Icon 组件：渲染为 span.icon 带 name 属性，便于断言
vi.mock('@/components/Icon.vue', () => ({
  default: {
    name: 'Icon',
    props: ['name', 'size'],
    template: '<span class="icon-mock" :data-name="name" :data-size="size" />',
  },
}))

import PasswordStrength from '@/components/PasswordStrength.vue'

const mockT = (key) => key

describe('PasswordStrength 组件', () => {
  describe('组件渲染', () => {
    it('password 为空时不渲染', () => {
      const wrapper = mount(PasswordStrength, {
        props: { password: '' },
        global: { mocks: { $t: mockT } },
      })
      expect(wrapper.find('.password-strength').exists()).toBe(false)
    })

    it('password 非空时渲染 .password-strength', () => {
      const wrapper = mount(PasswordStrength, {
        props: { password: 'abc' },
        global: { mocks: { $t: mockT } },
      })
      expect(wrapper.find('.password-strength').exists()).toBe(true)
    })

    it('包含强度条 .strength-bar 与填充 .strength-fill', () => {
      const wrapper = mount(PasswordStrength, {
        props: { password: 'abc' },
        global: { mocks: { $t: mockT } },
      })
      expect(wrapper.find('.strength-bar').exists()).toBe(true)
      expect(wrapper.find('.strength-fill').exists()).toBe(true)
    })

    it('包含 4 个需求项', () => {
      const wrapper = mount(PasswordStrength, {
        props: { password: 'abc' },
        global: { mocks: { $t: mockT } },
      })
      expect(wrapper.findAll('.requirements li')).toHaveLength(4)
    })
  })

  describe('强度评分 — 弱（weak）', () => {
    it('仅小写字母短密码 → weak', () => {
      const wrapper = mount(PasswordStrength, {
        props: { password: 'abcdef' },
        global: { mocks: { $t: mockT } },
      })
      // 长度<8，仅 lowercase → score=1 → weak
      expect(wrapper.find('.strength-fill').classes()).toContain('weak')
      expect(wrapper.find('.strength-label').classes()).toContain('weak')
    })

    it('小写+数字但长度不足 → weak', () => {
      const wrapper = mount(PasswordStrength, {
        props: { password: 'abc123' },
        global: { mocks: { $t: mockT } },
      })
      // 长度<8，lowercase+number → score=2 → weak
      expect(wrapper.find('.strength-fill').classes()).toContain('weak')
    })

    it('弱密码进度条宽度为 score*20%', () => {
      const wrapper = mount(PasswordStrength, {
        props: { password: 'abc123' },
        global: { mocks: { $t: mockT } },
      })
      // score=2 → 40%
      const style = wrapper.find('.strength-fill').attributes('style') || ''
      expect(style).toContain('width: 40%')
    })
  })

  describe('强度评分 — 中（medium）', () => {
    it('大小写+数字长度8 → medium', () => {
      const wrapper = mount(PasswordStrength, {
        props: { password: 'Abc12345' },
        global: { mocks: { $t: mockT } },
      })
      // length>=8, uppercase, lowercase, number → score=4 → medium
      expect(wrapper.find('.strength-fill').classes()).toContain('medium')
      expect(wrapper.find('.strength-label').classes()).toContain('medium')
    })

    it('中密码进度条宽度为 80%（score=4）', () => {
      const wrapper = mount(PasswordStrength, {
        props: { password: 'Abc12345' },
        global: { mocks: { $t: mockT } },
      })
      const style = wrapper.find('.strength-fill').attributes('style') || ''
      expect(style).toContain('width: 80%')
    })

    it('大小写+数字长度10 → medium（score=4，长度<12 不加分）', () => {
      const wrapper = mount(PasswordStrength, {
        props: { password: 'Abcdef1234' },
        global: { mocks: { $t: mockT } },
      })
      expect(wrapper.find('.strength-fill').classes()).toContain('medium')
    })
  })

  describe('强度评分 — 强（strong）', () => {
    it('大小写+数字长度>=12 → strong', () => {
      const wrapper = mount(PasswordStrength, {
        props: { password: 'Abcdefgh1234' },
        global: { mocks: { $t: mockT } },
      })
      // length>=8, uppercase, lowercase, number, length>=12 → score=5 → strong
      expect(wrapper.find('.strength-fill').classes()).toContain('strong')
      expect(wrapper.find('.strength-label').classes()).toContain('strong')
    })

    it('强密码进度条宽度为 100%（score=5 → min(100, 100)）', () => {
      const wrapper = mount(PasswordStrength, {
        props: { password: 'Abcdefgh1234' },
        global: { mocks: { $t: mockT } },
      })
      const style = wrapper.find('.strength-fill').attributes('style') || ''
      expect(style).toContain('width: 100%')
    })
  })

  describe('需求项 met 状态', () => {
    it('长度>=8 时第一项 met', () => {
      const wrapper = mount(PasswordStrength, {
        props: { password: 'longenough' },
        global: { mocks: { $t: mockT } },
      })
      const items = wrapper.findAll('.requirements li')
      expect(items[0].classes()).toContain('met')
    })

    it('长度<8 时第一项未 met', () => {
      const wrapper = mount(PasswordStrength, {
        props: { password: 'short' },
        global: { mocks: { $t: mockT } },
      })
      const items = wrapper.findAll('.requirements li')
      expect(items[0].classes()).not.toContain('met')
    })

    it('包含大写字母时第二项 met', () => {
      const wrapper = mount(PasswordStrength, {
        props: { password: 'Alowercase' },
        global: { mocks: { $t: mockT } },
      })
      const items = wrapper.findAll('.requirements li')
      expect(items[1].classes()).toContain('met')
    })

    it('包含小写字母时第三项 met', () => {
      const wrapper = mount(PasswordStrength, {
        props: { password: 'UPPERlower' },
        global: { mocks: { $t: mockT } },
      })
      const items = wrapper.findAll('.requirements li')
      expect(items[2].classes()).toContain('met')
    })

    it('包含数字时第四项 met', () => {
      const wrapper = mount(PasswordStrength, {
        props: { password: 'password1' },
        global: { mocks: { $t: mockT } },
      })
      const items = wrapper.findAll('.requirements li')
      expect(items[3].classes()).toContain('met')
    })

    it('全数字密码大小写项均未 met', () => {
      const wrapper = mount(PasswordStrength, {
        props: { password: '12345678' },
        global: { mocks: { $t: mockT } },
      })
      const items = wrapper.findAll('.requirements li')
      expect(items[1].classes()).not.toContain('met') // uppercase
      expect(items[2].classes()).not.toContain('met') // lowercase
      expect(items[3].classes()).toContain('met') // number
    })
  })

  describe('强度标签文案', () => {
    it('弱密码标签为 password_strength.weak', () => {
      const wrapper = mount(PasswordStrength, {
        props: { password: 'abc' },
        global: { mocks: { $t: mockT } },
      })
      expect(wrapper.find('.strength-label').text()).toBe('password_strength.weak')
    })

    it('强密码标签为 password_strength.strong', () => {
      const wrapper = mount(PasswordStrength, {
        props: { password: 'Abcdefgh1234' },
        global: { mocks: { $t: mockT } },
      })
      expect(wrapper.find('.strength-label').text()).toBe('password_strength.strong')
    })
  })
})