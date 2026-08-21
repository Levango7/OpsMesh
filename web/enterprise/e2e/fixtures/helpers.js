// E2E 测试共享 helpers — 登录、导航、等待
import { mockApi, setAuthCookies } from './mock-api.js'

// 应用根 URL（vite base = /enterprise/）
export const APP_BASE = '/enterprise/'

// 各路由的完整 path
export const routes = {
  login: APP_BASE + 'login',
  register: APP_BASE + 'register',
  overview: APP_BASE + 'overview',
  devices: APP_BASE + 'devices',
  deviceDetail: (id) => APP_BASE + 'devices/' + id,
  tasks: APP_BASE + 'tasks',
  alerts: APP_BASE + 'alerts',
  k8s: APP_BASE + 'k8s'
}

// 初始化一个已登录的 page：注入 cookie + mock API + 跳转到指定路由
// 用法：const { page } = await loggedInPage(browser, routes.devices)
export async function loggedInPage(browser, target = routes.overview) {
  const context = await browser.newContext()
  await setAuthCookies(context)
  const page = await context.newPage()
  await mockApi(page, { authed: true })
  await page.goto(target)
  return { context, page }
}

// 初始化一个未登录的 page（仅 mock API）
export async function anonymousPage(browser) {
  const context = await browser.newContext()
  const page = await context.newPage()
  await mockApi(page, { authed: false })
  return { context, page }
}

// 通过 UI 执行登录流程（用于 auth.spec.js 测试登录本身）
// 使用 data-testid 定位（语言无关，防 i18n 切换导致测试失败）
export async function loginViaUI(page, { username = 'admin', password = 'admin123' } = {}) {
  await page.goto(routes.login)
  await page.getByTestId('login-username').fill(username)
  await page.getByTestId('login-password').fill(password)
  await page.getByTestId('login-submit').click()
}