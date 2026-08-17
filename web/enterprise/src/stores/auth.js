// 认证 store — 双 HttpOnly Cookie（at+rt）会话，前端不持有令牌。
// 登录/注册成功后由服务端下发 Cookie；token 续期由 request.js 静默刷新完成。
// isLoggedIn 以当前 user 是否存在为准（首次加载经 fetchMe 从 Cookie 恢复会话）。
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { authApi } from '@/api/auth'
import { setUnauthorizedHandler } from '@/api/request'
import { t } from '@/i18n'

export const useAuthStore = defineStore('auth', () => {
  // 当前用户对象 {id, username, email, status, role_ids, permissions, ...}
  const user = ref(null)
  // 加载/错误状态
  const loading = ref(false)
  const error = ref('')

  // 是否已登录：以 user 是否存在为准（会话由 Cookie 维持，前端不存令牌）。
  const isLoggedIn = computed(() => !!user.value)

  // 会话初始化就绪标志：fetchMe() 完成后 resolve。
  // 路由守卫需 await ready 后再判断 isLoggedIn，避免冷启动时序竞争
  // （user 初始为 null → 守卫误判未登录 → 已登录用户被重定向到 /login）。
  let _readyResolve
  const ready = new Promise((resolve) => { _readyResolve = resolve })
  // 是否已完成首次会话恢复（同步可读，便于守卫与 App.vue 快速判断）
  const initialized = ref(false)

  // 当前用户有效权限集合（来自 /auth/me 的 permissions 字段，由后端按角色展开）。
  const permissions = computed(() => user.value?.permissions || [])

  // 是否拥有某权限：required 为空表示无权限门槛（始终可见）；
  // 若权限集合为空（如未下发权限/匿名会话）则一律拒绝——避免前端权限门控形同虚设。
  // 与后端 requireProd 闸同源，侧栏/操作按当前用户权限严格过滤（UI 权限门控）。
  function hasPerm(required) {
    if (!required) return true
    const ps = permissions.value
    if (ps.length === 0) return false
    return ps.includes(required)
  }

  // 登录
  async function login(username, password) {
    loading.value = true
    error.value = ''
    try {
      const { j } = await authApi.login(username, password)
      user.value = j.user || null
      return j
    } catch (e) {
      error.value = e.j?.error || t('error.loginFailed')
      throw e
    } finally {
      loading.value = false
    }
  }

  // 注册（仅当后端开放公开注册时可用；企业版默认关闭，入口已隐藏）
  async function register(username, password, email) {
    loading.value = true
    error.value = ''
    try {
      const { j } = await authApi.register(username, password, email)
      user.value = j.user || null
      return j
    } catch (e) {
      error.value = e.j?.error || t('error.registerFailed')
      throw e
    } finally {
      loading.value = false
    }
  }

  // 拉取当前用户信息（从 Cookie 恢复会话；at 过期时由 request.js 静默刷新后重试）。
  // 仅当 at/rt 均不存在（确无会话）时跳过，避免冷启动对匿名用户发起无意义 401。
  // 完成后 resolve ready Promise，路由守卫据此解除阻塞。
  async function fetchMe() {
    const c = document.cookie
    if (!c.includes('opsmesh_at') && !c.includes('opsmesh_rt')) {
      initialized.value = true
      _readyResolve()
      return null
    }
    try {
      const me = await authApi.me()
      user.value = me
      return me
    } catch (e) {
      if (e.s === 401) user.value = null
      return null
    } finally {
      initialized.value = true
      _readyResolve()
    }
  }

  // 清空会话状态（刷新失败/手动登出时调用）
  function clearSession() {
    user.value = null
  }

  // 退出登录（清 Cookie + 吊销 rt 由后端完成）
  async function logout() {
    try {
      await authApi.logout()
    } catch (_) { /* 忽略网络错误，前端仍清状态 */ }
    user.value = null
  }

  // 注册未授权回调：刷新失败 → 清会话并跳登录
  function onUnauthorized() {
    user.value = null
    const base = import.meta.env.BASE_URL || '/'
    if (!/\/(login|register)(\/|$|\?)/.test(window.location.pathname)) {
      window.location.href = base + 'login'
    }
  }
  setUnauthorizedHandler(onUnauthorized)

  return { user, loading, error, isLoggedIn, permissions, hasPerm, login, register, fetchMe, logout, clearSession, ready, initialized }
})
