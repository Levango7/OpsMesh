// E2E: 认证流程 — 登录 / 注册 / 退出 / 路由守卫 / 错误处理
import { test, expect } from '@playwright/test'
import { mockApi, setAuthCookies } from './fixtures/mock-api.js'
import { routes, loginViaUI } from './fixtures/helpers.js'

test.describe('认证流程', () => {

  test.describe('登录页', () => {
    test('页面正常加载，显示登录表单', async ({ page }) => {
      await mockApi(page, { authed: false })
      await page.goto(routes.login)
      // 标题
      await expect(page.getByRole('heading', { name: 'OpsMesh 企业版' })).toBeVisible()
      // 用户名/密码输入框
      await expect(page.getByPlaceholder('用户名')).toBeVisible()
      await expect(page.getByPlaceholder('密码')).toBeVisible()
      // 登录按钮
      await expect(page.getByRole('button', { name: /登录/ })).toBeVisible()
    })

    test('空用户名提交时显示前端校验提示', async ({ page }) => {
      await mockApi(page, { authed: false })
      await page.goto(routes.login)
      await page.getByPlaceholder('密码').fill('any')
      await page.getByRole('button', { name: /登录/ }).click()
      // 应显示"请输入用户名"
      await expect(page.getByText('请输入用户名')).toBeVisible()
    })

    test('空密码提交时显示前端校验提示', async ({ page }) => {
      await mockApi(page, { authed: false })
      await page.goto(routes.login)
      await page.getByPlaceholder('用户名').fill('admin')
      await page.getByRole('button', { name: /登录/ }).click()
      await expect(page.getByText('请输入密码')).toBeVisible()
    })

    test('正确账号登录后跳转到总览页', async ({ page }) => {
      await mockApi(page, { authed: true })
      await loginViaUI(page)
      // 等待跳转到 /overview
      await page.waitForURL('**/overview', { timeout: 10_000 })
      await expect(page).toHaveURL(/\/overview$/)
      // 顶栏应显示用户名 admin
      await expect(page.locator('.user-name')).toHaveText('admin')
    })

    test('错误凭证（后端返回 401）显示错误提示', async ({ page }) => {
      await mockApi(page, {
        authed: false,
        overrides: {
          'POST /auth/login': () => ({ status: 401, body: { error: '用户名或密码错误' } })
        }
      })
      await page.goto(routes.login)
      await page.getByPlaceholder('用户名').fill('wrong')
      await page.getByPlaceholder('密码').fill('wrong')
      await page.getByRole('button', { name: /登录/ }).click()
      await expect(page.getByText('用户名或密码错误')).toBeVisible({ timeout: 5_000 })
    })

    test('登录接口网络错误时显示错误提示', async ({ page }) => {
      await mockApi(page, {
        authed: false,
        overrides: {
          'POST /auth/login': () => { throw new Error('network error') }
        }
      })
      await page.goto(routes.login)
      await page.getByPlaceholder('用户名').fill('admin')
      await page.getByPlaceholder('密码').fill('admin123')
      await page.getByRole('button', { name: /登录/ }).click()
      // 应显示某种错误提示（具体文案由 i18n.login.invalid_credentials 决定）
      await expect(page.locator('.err-msg')).toBeVisible({ timeout: 5_000 })
    })
  })

  test.describe('注册页', () => {
    test('页面正常加载，显示注册表单', async ({ page }) => {
      await mockApi(page, { authed: false })
      await page.goto(routes.register)
      await expect(page.getByRole('heading', { name: 'OpsMesh 企业版' })).toBeVisible()
      await expect(page.getByPlaceholder('用户名')).toBeVisible()
      await expect(page.getByPlaceholder('密码')).toBeVisible()
      await expect(page.getByPlaceholder(/邮箱/)).toBeVisible()
      await expect(page.getByRole('button', { name: /注册/ })).toBeVisible()
    })

    test('空用户名提交时显示前端校验提示', async ({ page }) => {
      await mockApi(page, { authed: false })
      await page.goto(routes.register)
      await page.getByPlaceholder('密码').fill('pass')
      await page.getByRole('button', { name: /注册/ }).click()
      await expect(page.getByText('请输入用户名')).toBeVisible()
    })

    test('注册成功后自动登录（切换到主布局）', async ({ page }) => {
      await mockApi(page, { authed: true })
      await page.goto(routes.register)
      await page.getByPlaceholder('用户名').fill('newuser')
      await page.getByPlaceholder('密码').fill('newpass123')
      await page.getByRole('button', { name: /注册/ }).click()
      // 注册成功后 user 被设置 → App.vue 切换到主布局 → 顶栏显示用户名
      // （RegisterView 的 success 消息会因组件卸载而消失，故改测主布局出现）
      await expect(page.locator('.user-name')).toHaveText('newuser', { timeout: 5_000 })
    })

    test('注册失败（用户名已存在）显示错误提示', async ({ page }) => {
      await mockApi(page, {
        authed: false,
        overrides: {
          'POST /auth/register': () => ({ status: 409, body: { error: '用户名已被占用' } })
        }
      })
      await page.goto(routes.register)
      await page.getByPlaceholder('用户名').fill('admin')
      await page.getByPlaceholder('密码').fill('pass')
      await page.getByRole('button', { name: /注册/ }).click()
      await expect(page.getByText('用户名已被占用')).toBeVisible({ timeout: 5_000 })
    })

    test('注册页"去登录"链接可跳转到登录页', async ({ page }) => {
      await mockApi(page, { authed: false })
      await page.goto(routes.register)
      await page.getByRole('link', { name: /登录/ }).click()
      await expect(page).toHaveURL(/\/login$/)
    })
  })

  test.describe('路由守卫', () => {
    test('未登录访问受保护路由时重定向到登录页', async ({ page }) => {
      await mockApi(page, { authed: false })
      await page.goto(routes.devices)
      await expect(page).toHaveURL(/\/login$/)
    })

    test('已登录访问登录页时重定向到总览', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, { authed: true })
      await page.goto(routes.login)
      await expect(page).toHaveURL(/\/overview$/)
    })
  })

  test.describe('退出登录', () => {
    test('点击退出按钮后调用 logout API 并清空会话', async ({ page, context }) => {
      let logoutCalled = false
      await setAuthCookies(context)
      await mockApi(page, {
        authed: true,
        overrides: {
          'POST /auth/logout': () => {
            logoutCalled = true
            return { status: 200, body: {} }
          }
        }
      })
      await page.goto(routes.overview)
      await page.waitForURL('**/overview')
      // 顶栏退出按钮（带 title="退出"）
      await page.locator('button[title="退出"]').click()
      // logout API 应被调用
      await expect.poll(() => logoutCalled, { timeout: 5_000 }).toBe(true)
      // 退出后 user 清空 → 顶栏用户名消失（App.vue 切换到未登录布局）
      await expect(page.locator('.user-name')).toHaveCount(0, { timeout: 5_000 })
    })

    test('退出后再访问受保护路由被重定向到登录页', async ({ page, context }) => {
      await setAuthCookies(context)
      // mock /auth/me：第一次返回用户，logout 后返回 401
      let loggedOut = false
      await mockApi(page, {
        authed: true,
        overrides: {
          'GET /auth/me': () => loggedOut
            ? { status: 401, body: { error: 'unauthorized' } }
            : { status: 200, body: {
                id: 'u-admin', username: 'admin', email: 'a@b.c', status: 'active',
                role_ids: ['r'], permissions: ['device:read', 'task:read', 'alert:read']
              } },
          'POST /auth/logout': () => {
            loggedOut = true
            return { status: 200, body: {} }
          }
        }
      })
      await page.goto(routes.overview)
      await page.waitForURL('**/overview')
      await page.locator('button[title="退出"]').click()
      // 等待 logout 完成（user 清空）
      await expect(page.locator('.user-name')).toHaveCount(0, { timeout: 5_000 })
      // 再访问 devices，应被重定向到 /login
      await page.goto(routes.devices)
      await expect(page).toHaveURL(/\/login$/)
    })
  })
})