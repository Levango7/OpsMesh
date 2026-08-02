// 主题 store — light/dark 切换，localStorage 持久化
// 通过 document.documentElement.setAttribute('data-theme', theme) 切换 CSS 变量
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'

const STORAGE_KEY = 'opsmesh-theme'
const VALID = ['light', 'dark']

export const useThemeStore = defineStore('theme', () => {
  // 当前主题：'light' | 'dark'
  const current = ref('light')

  // 是否暗色
  const isDark = computed(() => current.value === 'dark')

  // 应用主题到 DOM
  function apply(theme) {
    document.documentElement.setAttribute('data-theme', theme)
  }

  // 初始化：从 localStorage 读取，默认 light
  function init() {
    const saved = localStorage.getItem(STORAGE_KEY)
    current.value = VALID.includes(saved) ? saved : 'light'
    apply(current.value)
  }

  // 切换主题
  function toggle() {
    current.value = current.value === 'light' ? 'dark' : 'light'
    localStorage.setItem(STORAGE_KEY, current.value)
    apply(current.value)
  }

  // 直接设置主题
  function set(theme) {
    if (!VALID.includes(theme)) return
    current.value = theme
    localStorage.setItem(STORAGE_KEY, theme)
    apply(theme)
  }

  return { current, isDark, init, toggle, set }
})