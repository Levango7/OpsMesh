// 生产 Service Worker 缓存契约（真实控制面 + 真实登录，非 page.route mock）。
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import path from 'node:path'

const BASE = 'http://127.0.0.1:4190'
const BACKEND_SCRIPT = path.resolve(process.cwd(), '../../deploy/scripts/e2e-service-worker-backend.sh')

// 默认 frontend job 只有 Vite；用固定环回脚本启动真实控制面，避免 ECONNREFUSED 假失败。
test.beforeAll(() => {
  execFileSync('bash', [BACKEND_SCRIPT, 'start'], { stdio: 'inherit' })
})

test.afterAll(() => {
  execFileSync('bash', [BACKEND_SCRIPT, 'stop'], { stdio: 'inherit' })
})

test.setTimeout(180_000)

// 仓内其他 mock 用例全局禁 SW；本专项必须显式启用，测生产真实行为。
test.use({ serviceWorkers: 'allow' })
const ADMIN_USER = 'admin'
const ADMIN_PASS = process.env.E2E_ADMIN_PASS || 'admin123'
const NEW_PASS = process.env.E2E_NEW_PASS || 'Sw-Real-2026!'

async function login(request) {
  async function attempt(password) {
    return request.post(`${BASE}/api/v1/auth/login`, {
      data: { username: ADMIN_USER, password },
    })
  }

  let resp = await attempt(NEW_PASS)
  if (resp.status() === 401) {
    resp = await attempt(ADMIN_PASS)
    expect([200, 201]).toContain(resp.status())
    const body = await resp.json()
    if (body.mustChangePassword || body.changePasswordToken) {
      const change = await request.post(`${BASE}/api/v1/auth/change-password`, {
        data: {
          oldPassword: ADMIN_PASS,
          newPassword: NEW_PASS,
          changePasswordToken: body.changePasswordToken,
        },
      })
      expect([200, 201]).toContain(change.status())
      resp = await attempt(NEW_PASS)
    }
  }
  expect([200, 201]).toContain(resp.status())
  const body = await resp.json()
  expect(body.token, '登录应返回 access token').toBeTruthy()
  return body.token
}

async function fetchMe(page, token) {
  return page.evaluate(async (accessToken) => {
    const response = await fetch('/api/v1/auth/me', {
      headers: { Authorization: `Bearer ${accessToken}` },
    })
    return { status: response.status, text: await response.text() }
  }, token)
}

test('生产 SW：身份与 API 响应不进入 Cache Storage，原始 body 可连续读取', async ({ page, request }) => {
  const token = await login(request)
  await page.context().addCookies([{ name: 'opsmesh_at', value: token, url: BASE }])
  await page.goto(`${BASE}/enterprise/overview`)
  await expect(page).toHaveURL(/\/enterprise\/overview/)
  // 首次 controllerchange 会触发一次重载，先让新 controller 与页面稳定。
  await page.waitForTimeout(2000)
  await page.evaluate(() => navigator.serviceWorker.ready)
  await expect(page).toHaveURL(/\/enterprise\/overview/)

  const first = await fetchMe(page, token)
  expect(first.status).toBe(200)
  expect(first.text).toContain('admin')

  const cachedAPIs = await page.evaluate(async () => {
    const names = await caches.keys()
    const urls = []
    for (const name of names) {
      const cache = await caches.open(name)
      for (const request of await cache.keys()) urls.push(request.url)
    }
    return urls.filter((url) => url.includes('/api/'))
  })
  expect(cachedAPIs).toEqual([])

  const second = await fetchMe(page, token)
  expect(second.status).toBe(200)
  expect(second.text).toContain('admin')
  expect(second.text).toContain('tenantId')
})
