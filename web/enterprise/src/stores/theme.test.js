// theme store 单元测试
// 覆盖：初始状态、init/toggle/set 动作、isDark getter、localStorage 持久化、DOM 属性应用。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

import { useThemeStore } from '@/stores/theme'

const STORAGE_KEY = 'opsmesh-theme'

describe('useThemeStore', () => {
  beforeEach(() => {
    // 每个测试前创建新 pinia 实例，隔离状态
    setActivePinia(createPinia())
    // 清空 localStorage 与 DOM 属性，避免测试间污染
    localStorage.clear()
    document.documentElement.removeAttribute('data-theme')
  })

  describe('初始状态', () => {
    it('current 初始为 light', () => {
      const store = useThemeStore()
      expect(store.current).toBe('light')
    })

    it('isDark 初始为 false', () => {
      const store = useThemeStore()
      expect(store.isDark).toBe(false)
    })
  })

  describe('isDark getter', () => {
    it('current 为 dark 时 isDark 为 true', () => {
      const store = useThemeStore()
      store.current = 'dark'
      expect(store.isDark).toBe(true)
    })

    it('current 为 light 时 isDark 为 false', () => {
      const store = useThemeStore()
      store.current = 'light'
      expect(store.isDark).toBe(false)
    })
  })

  describe('init 动作', () => {
    it('localStorage 有有效值时读取并应用', () => {
      localStorage.setItem(STORAGE_KEY, 'dark')

      const store = useThemeStore()
      store.init()

      expect(store.current).toBe('dark')
      expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
    })

    it('localStorage 无值时默认 light', () => {
      const store = useThemeStore()
      store.init()

      expect(store.current).toBe('light')
      expect(document.documentElement.getAttribute('data-theme')).toBe('light')
    })

    it('localStorage 为无效值时回退 light', () => {
      localStorage.setItem(STORAGE_KEY, 'pink')

      const store = useThemeStore()
      store.init()

      expect(store.current).toBe('light')
      expect(document.documentElement.getAttribute('data-theme')).toBe('light')
    })
  })

  describe('toggle 动作', () => {
    it('从 light 切换到 dark', () => {
      const store = useThemeStore()
      store.toggle()

      expect(store.current).toBe('dark')
      expect(store.isDark).toBe(true)
    })

    it('从 dark 切换到 light', () => {
      const store = useThemeStore()
      store.current = 'dark'

      store.toggle()

      expect(store.current).toBe('light')
      expect(store.isDark).toBe(false)
    })

    it('切换后写入 localStorage', () => {
      const store = useThemeStore()
      store.toggle()

      expect(localStorage.getItem(STORAGE_KEY)).toBe('dark')
    })

    it('切换后应用 DOM data-theme 属性', () => {
      const store = useThemeStore()
      store.toggle()

      expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
    })
  })

  describe('set 动作', () => {
    it('设置有效主题时更新 current', () => {
      const store = useThemeStore()
      store.set('dark')

      expect(store.current).toBe('dark')
    })

    it('设置有效主题时持久化到 localStorage', () => {
      const store = useThemeStore()
      store.set('dark')

      expect(localStorage.getItem(STORAGE_KEY)).toBe('dark')
    })

    it('设置有效主题时应用 DOM 属性', () => {
      const store = useThemeStore()
      store.set('dark')

      expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
    })

    it('设置无效主题时不修改 current', () => {
      const store = useThemeStore()
      store.set('pink')

      expect(store.current).toBe('light')
    })

    it('设置无效主题时不写入 localStorage', () => {
      const store = useThemeStore()
      store.set('pink')

      expect(localStorage.getItem(STORAGE_KEY)).toBeNull()
    })
  })
})