// LoginView 视图单元测试
// 覆盖：
//   - 组件挂载与基本结构（auth-page / auth-card / form）
//   - 表单元素渲染（username/password input、submit button）
//   - 提交验证（空用户名/空密码显示错误）
//   - 登录成功跳转 /overview
//   - 登录失败显示错误提示
//   - loading 态禁用按钮
//
// mock 策略：
//   - @/i18n：返回键本身
//   - @/stores/auth：useAuthStore 返回带 login 的可控对象
//   - @/stores/theme：useThemeStore 返回带 toggle/isDark 的可控对象
//   - vue-router：useRouter 返回带 push 的 vi.fn
//   - @/components/Icon.vue：stub 为 span
//   - 全局 $t：通过 global.mocks 注入
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'

vi.mock('@/i18n', () => ({
  t: (key) => key,
  currentLang: { value: 'zh' },
  setLang: vi.fn(),
  initLang: vi.fn(),
  default: {},
}))

const mockLogin = vi.fn()
const mockPush = vi.fn()

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    login: mockLogin,
    user: null,
    isLoggedIn: false,
  }),
}))

vi.mock('@/stores/theme', () => ({
  useThemeStore: () => ({
    isDark: false,
    toggle: vi.fn(),
  }),
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: mockPush }),
  useRoute: () => ({}),
}))

vi.mock('@/components/Icon.vue', () => ({
  default: {
    name: 'Icon',
    props: ['name', 'size'],
    template: '<span class="icon-mock" />',
  },
}))

import LoginView from '@/views/LoginView.vue'

const mockT = (key) => key

function mountLogin() {
  return mount(LoginView, { global: { mocks: { $t: mockT } } })
}

describe('LoginView 视图', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPush.mockReset()
    mockLogin.mockReset()
  })

  describe('组件挂载', () => {
    it('挂载成功，包含 .auth-page 容器', () => {
      const wrapper = mountLogin()
      expect(wrapper.find('.auth-page').exists()).toBe(true)
    })

    it('包含 .auth-card 卡片', () => {
      const wrapper = mountLogin()
      expect(wrapper.find('.auth-card').exists()).toBe(true)
    })

    it('包含品牌标题 h1', () => {
      const wrapper = mountLogin()
      expect(wrapper.find('h1').exists()).toBe(true)
    })

    it('包含 data-testid="login-form" 表单', () => {
      const wrapper = mountLogin()
      expect(wrapper.find('[data-testid="login-form"]').exists()).toBe(true)
    })
  })

  describe('表单元素渲染', () => {
    it('包含用户名输入框 data-testid="login-username"', () => {
      const wrapper = mountLogin()
      expect(wrapper.find('[data-testid="login-username"]').exists()).toBe(true)
    })

    it('包含密码输入框 data-testid="login-password"', () => {
      const wrapper = mountLogin()
      const pw = wrapper.find('[data-testid="login-password"]')
      expect(pw.exists()).toBe(true)
      expect(pw.attributes('type')).toBe('password')
    })

    it('包含提交按钮 data-testid="login-submit"', () => {
      const wrapper = mountLogin()
      expect(wrapper.find('[data-testid="login-submit"]').exists()).toBe(true)
    })

    it('初始无错误提示', () => {
      const wrapper = mountLogin()
      expect(wrapper.find('[data-testid="login-error"]').exists()).toBe(false)
    })

    it('提交按钮初始未禁用', () => {
      const wrapper = mountLogin()
      expect(wrapper.find('[data-testid="login-submit"]').attributes('disabled')).toBeFalsy()
    })
  })

  describe('提交验证', () => {
    it('空用户名提交时显示 need_username 错误', async () => {
      const wrapper = mountLogin()
      await wrapper.find('form').trigger('submit')
      await nextTick()
      expect(wrapper.find('[data-testid="login-error"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="login-error"]').text()).toContain('login.need_username')
      expect(mockLogin).not.toHaveBeenCalled()
    })

    it('仅填用户名、空密码提交时显示 need_password 错误', async () => {
      const wrapper = mountLogin()
      await wrapper.find('[data-testid="login-username"]').setValue('admin')
      await wrapper.find('form').trigger('submit')
      await nextTick()
      expect(wrapper.find('[data-testid="login-error"]').text()).toContain('login.need_password')
      expect(mockLogin).not.toHaveBeenCalled()
    })
  })

  describe('登录成功跳转', () => {
    it('登录成功（非首登改密）跳转 /overview', async () => {
      mockLogin.mockResolvedValue({ mustChangePassword: false })
      const wrapper = mountLogin()
      await wrapper.find('[data-testid="login-username"]').setValue('admin')
      await wrapper.find('[data-testid="login-password"]').setValue('pass123')
      await wrapper.find('form').trigger('submit')
      await nextTick()
      await nextTick()
      expect(mockLogin).toHaveBeenCalledWith('admin', 'pass123')
      expect(mockPush).toHaveBeenCalledWith('/overview')
    })

    it('登录成功且需首登改密时跳转 /change-password', async () => {
      mockLogin.mockResolvedValue({ mustChangePassword: true })
      const wrapper = mountLogin()
      await wrapper.find('[data-testid="login-username"]').setValue('admin')
      await wrapper.find('[data-testid="login-password"]').setValue('pass123')
      await wrapper.find('form').trigger('submit')
      await nextTick()
      await nextTick()
      expect(mockPush).toHaveBeenCalledWith('/change-password')
    })
  })

  describe('登录失败提示', () => {
    it('登录失败时显示错误提示', async () => {
      mockLogin.mockRejectedValue({ j: { error: '密码错误' } })
      const wrapper = mountLogin()
      await wrapper.find('[data-testid="login-username"]').setValue('admin')
      await wrapper.find('[data-testid="login-password"]').setValue('wrong')
      await wrapper.find('form').trigger('submit')
      await nextTick()
      await nextTick()
      expect(wrapper.find('[data-testid="login-error"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="login-error"]').text()).toContain('密码错误')
    })

    it('429 限流时显示 tooManyRequests 提示', async () => {
      mockLogin.mockRejectedValue({ s: 429 })
      const wrapper = mountLogin()
      await wrapper.find('[data-testid="login-username"]').setValue('admin')
      await wrapper.find('[data-testid="login-password"]').setValue('pw')
      await wrapper.find('form').trigger('submit')
      await nextTick()
      await nextTick()
      expect(wrapper.find('[data-testid="login-error"]').text()).toContain('error.tooManyRequests')
    })

    it('未知错误回退 invalid_credentials 提示', async () => {
      mockLogin.mockRejectedValue({})
      const wrapper = mountLogin()
      await wrapper.find('[data-testid="login-username"]').setValue('admin')
      await wrapper.find('[data-testid="login-password"]').setValue('pw')
      await wrapper.find('form').trigger('submit')
      await nextTick()
      await nextTick()
      expect(wrapper.find('[data-testid="login-error"]').text()).toContain('login.invalid_credentials')
    })
  })

  describe('loading 态', () => {
    it('提交后登录期间按钮禁用', async () => {
      let resolveLogin
      mockLogin.mockReturnValue(new Promise((r) => { resolveLogin = r }))
      const wrapper = mountLogin()
      await wrapper.find('[data-testid="login-username"]').setValue('admin')
      await wrapper.find('[data-testid="login-password"]').setValue('pw')
      wrapper.find('form').trigger('submit')
      await nextTick()
      expect(wrapper.find('[data-testid="login-submit"]').attributes('disabled')).toBeDefined()
      resolveLogin({ mustChangePassword: false })
      await nextTick()
      await nextTick()
      expect(wrapper.find('[data-testid="login-submit"]').attributes('disabled')).toBeFalsy()
    })
  })

  describe('顶栏工具', () => {
    it('包含主题切换按钮', () => {
      const wrapper = mountLogin()
      expect(wrapper.findAll('.tool-btn').length).toBeGreaterThanOrEqual(1)
    })

    it('包含语言切换按钮', () => {
      const wrapper = mountLogin()
      // 语言按钮内有 "中" 或 "EN" 文本
      const btns = wrapper.findAll('.tool-btn')
      const langBtn = btns.find((b) => b.text().includes('中') || b.text().includes('EN'))
      expect(langBtn).toBeTruthy()
    })
  })
})