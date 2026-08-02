// 认证 store — JWT token + 当前用户，localStorage 持久化 token
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { authApi } from '@/api/auth'
import { TOKEN_KEY } from '@/api/request'

export const useAuthStore = defineStore('auth', () => {
  // JWT token（持久化到 localStorage）
  const token = ref(localStorage.getItem(TOKEN_KEY) || '')
  // 当前用户对象 {id, username, email, status, role_ids, created_at}
  const user = ref(null)
  // 加载/错误状态
  const loading = ref(false)
  const error = ref('')

  // 是否已登录
  const isLoggedIn = computed(() => !!token.value)

  // 设置 token 并持久化
  function setToken(t) {
    token.value = t || ''
    if (t) localStorage.setItem(TOKEN_KEY, t)
    else localStorage.removeItem(TOKEN_KEY)
  }

  // 登录
  async function login(username, password) {
    loading.value = true
    error.value = ''
    try {
      const { j } = await authApi.login(username, password)
      setToken(j.token)
      user.value = j.user || null
      return j
    } catch (e) {
      error.value = e.j?.error || '登录失败'
      throw e
    } finally {
      loading.value = false
    }
  }

  // 注册
  async function register(username, password, email) {
    loading.value = true
    error.value = ''
    try {
      const { j } = await authApi.register(username, password, email)
      setToken(j.token)
      user.value = j.user || null
      return j
    } catch (e) {
      error.value = e.j?.error || '注册失败'
      throw e
    } finally {
      loading.value = false
    }
  }

  // 拉取当前用户信息
  async function fetchMe() {
    if (!token.value) return null
    try {
      const me = await authApi.me()
      user.value = me
      return me
    } catch (e) {
      // 401 已由拦截器处理，这里仅清空 user
      if (e.s === 401) user.value = null
      return null
    }
  }

  // 退出登录
  function logout() {
    setToken('')
    user.value = null
  }

  return { token, user, loading, error, isLoggedIn, login, register, fetchMe, logout, setToken }
})