// ToastHost 组件单元测试
// 覆盖：
//   - 组件挂载与基本结构
//   - toast 显示/消失（toasts 队列驱动）
//   - 不同类型（success/warn/error/info）渲染对应 class
//   - 关闭按钮点击触发 dismiss
//   - 空队列时无 toast 渲染
//
// mock 策略：
//   - @/utils/toast：替换 toasts 为本地 reactive 数组，dismiss 为 vi.fn
//   - 全局 $t：通过 global.mocks 注入
//
// 注意：vi.mock 工厂被提升到文件顶部，不能引用普通顶层变量。
// 使用 vi.hoisted 创建可在 mock 工厂中安全引用的 reactive 队列与 dismiss。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'

// vi.hoisted 保证变量在 vi.mock 提升之前初始化
const { mockToasts, mockDismiss } = vi.hoisted(() => {
  const { reactive } = require('vue')
  return {
    mockToasts: reactive([]),
    mockDismiss: vi.fn((id) => {
      const idx = mockToasts.findIndex((t) => t.id === id)
      if (idx !== -1) mockToasts.splice(idx, 1)
    }),
  }
})

vi.mock('@/utils/toast', () => ({
  toasts: mockToasts,
  dismiss: mockDismiss,
  push: vi.fn(),
  clearAll: vi.fn(),
  toast: { error: vi.fn(), warn: vi.fn(), success: vi.fn() },
  default: {},
}))

import ToastHost from '@/components/ToastHost.vue'

const mockT = (key) => key

describe('ToastHost 组件', () => {
  beforeEach(() => {
    mockToasts.splice(0, mockToasts.length)
    vi.clearAllMocks()
  })

  describe('组件渲染', () => {
    it('挂载成功，包含 .toast-host 容器', () => {
      const wrapper = mount(ToastHost, { global: { mocks: { $t: mockT } } })
      expect(wrapper.find('.toast-host').exists()).toBe(true)
    })

    it('容器有 aria-live="polite"', () => {
      const wrapper = mount(ToastHost, { global: { mocks: { $t: mockT } } })
      expect(wrapper.find('.toast-host').attributes('aria-live')).toBe('polite')
    })

    it('容器有 data-testid="toast-host"', () => {
      const wrapper = mount(ToastHost, { global: { mocks: { $t: mockT } } })
      expect(wrapper.find('[data-testid="toast-host"]').exists()).toBe(true)
    })
  })

  describe('空队列', () => {
    it('无 toast 时不渲染 .toast 元素', () => {
      const wrapper = mount(ToastHost, { global: { mocks: { $t: mockT } } })
      expect(wrapper.findAll('.toast')).toHaveLength(0)
    })
  })

  describe('toast 显示', () => {
    it('队列有 1 条时渲染 1 个 .toast', () => {
      mockToasts.push({ id: 1, type: 'error', title: '出错了' })
      const wrapper = mount(ToastHost, { global: { mocks: { $t: mockT } } })
      expect(wrapper.findAll('.toast')).toHaveLength(1)
    })

    it('队列多条时渲染多个 .toast', () => {
      mockToasts.push({ id: 1, type: 'error', title: '错误1' })
      mockToasts.push({ id: 2, type: 'success', title: '成功1' })
      mockToasts.push({ id: 3, type: 'warn', title: '警告1' })
      const wrapper = mount(ToastHost, { global: { mocks: { $t: mockT } } })
      expect(wrapper.findAll('.toast')).toHaveLength(3)
    })

    it('toast 文案渲染到 .toast-msg', () => {
      mockToasts.push({ id: 1, type: 'error', title: '操作失败' })
      const wrapper = mount(ToastHost, { global: { mocks: { $t: mockT } } })
      expect(wrapper.find('.toast-msg').text()).toBe('操作失败')
    })
  })

  describe('toast 类型 class', () => {
    it('type=error 时 .toast 包含 error class', () => {
      mockToasts.push({ id: 1, type: 'error', title: 'err' })
      const wrapper = mount(ToastHost, { global: { mocks: { $t: mockT } } })
      expect(wrapper.find('.toast').classes()).toContain('error')
    })

    it('type=warn 时 .toast 包含 warn class', () => {
      mockToasts.push({ id: 1, type: 'warn', title: 'w' })
      const wrapper = mount(ToastHost, { global: { mocks: { $t: mockT } } })
      expect(wrapper.find('.toast').classes()).toContain('warn')
    })

    it('type=success 时 .toast 包含 success class', () => {
      mockToasts.push({ id: 1, type: 'success', title: 'ok' })
      const wrapper = mount(ToastHost, { global: { mocks: { $t: mockT } } })
      expect(wrapper.find('.toast').classes()).toContain('success')
    })

    it('type=info 时 .toast 包含 info class', () => {
      mockToasts.push({ id: 1, type: 'info', title: 'i' })
      const wrapper = mount(ToastHost, { global: { mocks: { $t: mockT } } })
      expect(wrapper.find('.toast').classes()).toContain('info')
    })

    it('不同类型 toast 同时渲染各自 class', () => {
      mockToasts.push({ id: 1, type: 'error', title: 'e' })
      mockToasts.push({ id: 2, type: 'success', title: 's' })
      const wrapper = mount(ToastHost, { global: { mocks: { $t: mockT } } })
      const toasts = wrapper.findAll('.toast')
      expect(toasts[0].classes()).toContain('error')
      expect(toasts[1].classes()).toContain('success')
    })
  })

  describe('toast data-testid', () => {
    it('每条 toast 有 data-testid="toast-{type}"', () => {
      mockToasts.push({ id: 1, type: 'error', title: 'e' })
      mockToasts.push({ id: 2, type: 'success', title: 's' })
      const wrapper = mount(ToastHost, { global: { mocks: { $t: mockT } } })
      expect(wrapper.find('[data-testid="toast-error"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="toast-success"]').exists()).toBe(true)
    })
  })

  describe('关闭按钮', () => {
    it('每条 toast 包含关闭按钮 .toast-close', () => {
      mockToasts.push({ id: 1, type: 'error', title: 'e' })
      const wrapper = mount(ToastHost, { global: { mocks: { $t: mockT } } })
      expect(wrapper.find('.toast-close').exists()).toBe(true)
    })

    it('关闭按钮 aria-label 为 common.close', () => {
      mockToasts.push({ id: 1, type: 'error', title: 'e' })
      const wrapper = mount(ToastHost, { global: { mocks: { $t: mockT } } })
      expect(wrapper.find('.toast-close').attributes('aria-label')).toBe('common.close')
    })

    it('点击关闭按钮调用 dismiss(id)', async () => {
      mockToasts.push({ id: 42, type: 'error', title: 'e' })
      const wrapper = mount(ToastHost, { global: { mocks: { $t: mockT } } })
      await wrapper.find('.toast-close').trigger('click')
      expect(mockDismiss).toHaveBeenCalledWith(42)
    })
  })

  describe('toast 消失', () => {
    it('队列移除后 toast 不再渲染', async () => {
      mockToasts.push({ id: 1, type: 'error', title: 'e' })
      const wrapper = mount(ToastHost, { global: { mocks: { $t: mockT } } })
      expect(wrapper.findAll('.toast')).toHaveLength(1)
      // 模拟 dismiss 从队列移除
      mockToasts.splice(0, 1)
      await wrapper.vm.$nextTick()
      expect(wrapper.findAll('.toast')).toHaveLength(0)
    })
  })

  describe('toast 结构', () => {
    it('toast 包含 dot、msg、close 三部分', () => {
      mockToasts.push({ id: 1, type: 'error', title: 'msg' })
      const wrapper = mount(ToastHost, { global: { mocks: { $t: mockT } } })
      expect(wrapper.find('.toast-dot').exists()).toBe(true)
      expect(wrapper.find('.toast-msg').exists()).toBe(true)
      expect(wrapper.find('.toast-close').exists()).toBe(true)
    })

    it('toast 有 role="alert"', () => {
      mockToasts.push({ id: 1, type: 'error', title: 'msg' })
      const wrapper = mount(ToastHost, { global: { mocks: { $t: mockT } } })
      expect(wrapper.find('.toast').attributes('role')).toBe('alert')
    })
  })
})
