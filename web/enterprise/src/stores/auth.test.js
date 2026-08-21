// auth store 单元测试
// 覆盖：初始状态、login/logout/fetchMe 动作、isLoggedIn computed、hasPerm 权限判断。
// 说明：源码中权限方法名为 hasPerm（按权限字符串判断），并非 hasRole；
//      本测试按实际导出的 hasPerm 进行验证，保持与实现一致。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

// mock @/api/auth：避免真实网络请求，用 vi.fn 控制返回值
vi.mock('@/api/auth', () => ({
  authApi: {
    login: vi.fn(),
    register: vi.fn(),
    me: vi.fn(),
    logout: vi.fn(),
  },
}))

// mock @/api/request 的 setUnauthorizedHandler：auth store 初始化时会调用
vi.mock('@/api/request', () => ({
  setUnauthorizedHandler: vi.fn(),
}))

import { useAuthStore } from '@/stores/auth'
import { authApi } from '@/api/auth'

describe('useAuthStore', () => {
  beforeEach(() => {
    // 每个测试前创建新 pinia 实例，隔离状态
    setActivePinia(createPinia())
    // 重置 mock 调用记录
    vi.clearAllMocks()
    // 清空 cookie，避免 fetchMe 误判会话
    document.cookie = ''
  })

  describe('初始状态', () => {
    it('user 初始为 null', () => {
      const store = useAuthStore()
      expect(store.user).toBeNull()
    })

    it('loading 初始为 false', () => {
      const store = useAuthStore()
      expect(store.loading).toBe(false)
    })

    it('error 初始为空字符串', () => {
      const store = useAuthStore()
      expect(store.error).toBe('')
    })

    it('isLoggedIn 初始为 false（user 为 null）', () => {
      const store = useAuthStore()
      expect(store.isLoggedIn).toBe(false)
    })

    it('permissions 初始为空数组', () => {
      const store = useAuthStore()
      expect(store.permissions).toEqual([])
    })

    it('initialized 初始为 false', () => {
      const store = useAuthStore()
      expect(store.initialized).toBe(false)
    })
  })

  describe('isLoggedIn computed', () => {
    it('user 不为 null 时 isLoggedIn 为 true', () => {
      const store = useAuthStore()
      store.user = { id: 1, username: 'admin' }
      expect(store.isLoggedIn).toBe(true)
    })

    it('user 为 null 时 isLoggedIn 为 false', () => {
      const store = useAuthStore()
      store.user = null
      expect(store.isLoggedIn).toBe(false)
    })
  })

  describe('login 动作', () => {
    it('登录成功后设置 user 并返回响应', async () => {
      const mockUser = { id: 1, username: 'admin', permissions: ['task:write'] }
      authApi.login.mockResolvedValueOnce({ s: 200, j: { user: mockUser, token: 'abc' } })

      const store = useAuthStore()
      const result = await store.login('admin', 'pass')

      expect(authApi.login).toHaveBeenCalledWith('admin', 'pass')
      expect(store.user).toEqual(mockUser)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
      expect(result).toEqual({ user: mockUser, token: 'abc' })
    })

    it('登录中 loading 为 true', async () => {
      let resolveLogin
      authApi.login.mockReturnValueOnce(new Promise((r) => { resolveLogin = r }))

      const store = useAuthStore()
      const promise = store.login('admin', 'pass')

      expect(store.loading).toBe(true)
      resolveLogin({ s: 200, j: { user: { id: 1 } } })
      await promise

      expect(store.loading).toBe(false)
    })

    it('登录失败时设置 error 并抛出异常', async () => {
      const err = { s: 401, j: { error: '用户名或密码错误' } }
      authApi.login.mockRejectedValueOnce(err)

      const store = useAuthStore()
      await expect(store.login('admin', 'wrong')).rejects.toEqual(err)

      expect(store.error).toBe('用户名或密码错误')
      expect(store.loading).toBe(false)
      expect(store.user).toBeNull()
    })

    it('登录失败且无 error 字段时使用默认错误信息', async () => {
      const err = { s: 500, j: {} }
      authApi.login.mockRejectedValueOnce(err)

      const store = useAuthStore()
      await expect(store.login('admin', 'pass')).rejects.toEqual(err)

      expect(store.error).toBe('登录失败')
    })

    it('登录成功但响应无 user 字段时 user 为 null', async () => {
      authApi.login.mockResolvedValueOnce({ s: 200, j: { token: 'abc' } })

      const store = useAuthStore()
      await store.login('admin', 'pass')

      expect(store.user).toBeNull()
    })
  })

  describe('logout 动作', () => {
    it('登出成功后清空 user', async () => {
      authApi.logout.mockResolvedValueOnce({ s: 200, j: {} })

      const store = useAuthStore()
      store.user = { id: 1, username: 'admin' }
      await store.logout()

      expect(authApi.logout).toHaveBeenCalled()
      expect(store.user).toBeNull()
      expect(store.isLoggedIn).toBe(false)
    })

    it('登出 API 失败时仍清空 user（忽略网络错误）', async () => {
      authApi.logout.mockRejectedValueOnce(new Error('network'))

      const store = useAuthStore()
      store.user = { id: 1, username: 'admin' }
      await store.logout()

      expect(store.user).toBeNull()
    })
  })

  describe('fetchMe 动作', () => {
    it('无会话时后端返回 401，清空 user 并标记 initialized', async () => {
      document.cookie = ''
      authApi.me.mockRejectedValueOnce({ s: 401, j: { error: 'unauthorized' } })

      const store = useAuthStore()
      const result = await store.fetchMe()

      expect(authApi.me).toHaveBeenCalled()
      expect(result).toBeNull()
      expect(store.initialized).toBe(true)
      expect(store.user).toBeNull()
    })

    it('有会话时从后端恢复用户信息', async () => {
      document.cookie = 'opsmesh_at=token123; path=/'
      const mockMe = { id: 1, username: 'admin', permissions: ['task:write'] }
      authApi.me.mockResolvedValueOnce(mockMe)

      const store = useAuthStore()
      const result = await store.fetchMe()

      expect(authApi.me).toHaveBeenCalled()
      expect(result).toEqual(mockMe)
      expect(store.user).toEqual(mockMe)
      expect(store.initialized).toBe(true)
    })

    it('会话有效时恢复用户信息（仅 rt 也存在）', async () => {
      document.cookie = 'opsmesh_rt=refresh456; path=/'
      const mockMe = { id: 2, username: 'guest' }
      authApi.me.mockResolvedValueOnce(mockMe)

      const store = useAuthStore()
      const result = await store.fetchMe()

      expect(authApi.me).toHaveBeenCalled()
      expect(result).toEqual(mockMe)
      expect(store.user).toEqual(mockMe)
      expect(store.initialized).toBe(true)
    })

    it('会话过期（401）时清空 user', async () => {
      document.cookie = 'opsmesh_at=expired; path=/'
      authApi.me.mockRejectedValueOnce({ s: 401, j: { error: 'unauthorized' } })

      const store = useAuthStore()
      const result = await store.fetchMe()

      expect(result).toBeNull()
      expect(store.user).toBeNull()
      expect(store.initialized).toBe(true)
    })

    it('其他错误（如 500）时保留现有 user 并标记 initialized', async () => {
      document.cookie = 'opsmesh_at=token; path=/'
      const existingUser = { id: 1, username: 'admin' }
      authApi.me.mockRejectedValueOnce({ s: 500, j: { error: 'server error' } })

      const store = useAuthStore()
      store.user = existingUser
      const result = await store.fetchMe()

      expect(result).toBeNull()
      expect(store.user).toEqual(existingUser)
      expect(store.initialized).toBe(true)
    })
  })

  describe('clearSession 动作', () => {
    it('清空会话状态', () => {
      const store = useAuthStore()
      store.user = { id: 1, username: 'admin' }
      store.clearSession()

      expect(store.user).toBeNull()
      expect(store.isLoggedIn).toBe(false)
    })
  })

  describe('hasPerm 权限判断', () => {
    it('required 为空时始终返回 true（无权限门槛）', () => {
      const store = useAuthStore()
      store.user = null
      expect(store.hasPerm('')).toBe(true)
      expect(store.hasPerm(null)).toBe(true)
      expect(store.hasPerm(undefined)).toBe(true)
    })

    it('用户权限集合为空时一律拒绝返回 false（安全加固，避免权限门控形同虚设）', () => {
      const store = useAuthStore()
      store.user = { id: 1, permissions: [] }
      expect(store.hasPerm('task:write')).toBe(false)
    })

    it('用户拥有所需权限时返回 true', () => {
      const store = useAuthStore()
      store.user = { id: 1, permissions: ['task:write', 'device:read'] }
      expect(store.hasPerm('task:write')).toBe(true)
      expect(store.hasPerm('device:read')).toBe(true)
    })

    it('用户不拥有所需权限时返回 false', () => {
      const store = useAuthStore()
      store.user = { id: 1, permissions: ['device:read'] }
      expect(store.hasPerm('task:write')).toBe(false)
    })

    it('user 为 null 时 permissions 为空数组，hasPerm 拒绝返回 false', () => {
      const store = useAuthStore()
      store.user = null
      // permissions 为 [] → 一律拒绝（安全加固）
      expect(store.hasPerm('task:write')).toBe(false)
    })
  })

  describe('permissions computed', () => {
    it('返回 user.permissions', () => {
      const store = useAuthStore()
      store.user = { id: 1, permissions: ['a', 'b'] }
      expect(store.permissions).toEqual(['a', 'b'])
    })

    it('user 无 permissions 字段时返回空数组', () => {
      const store = useAuthStore()
      store.user = { id: 1 }
      expect(store.permissions).toEqual([])
    })
  })

  describe('ready Promise', () => {
    it('fetchMe 完成后 ready resolve', async () => {
      document.cookie = ''
      const store = useAuthStore()
      await store.fetchMe()
      await expect(store.ready).resolves.toBeUndefined()
    })
  })
})