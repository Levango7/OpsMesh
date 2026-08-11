// E2E: 监控告警流程 — 列表 / 统计 / ack / silence / 错误处理
import { test, expect } from '@playwright/test'
import { mockApi, setAuthCookies } from './fixtures/mock-api.js'
import { routes } from './fixtures/helpers.js'

test.beforeEach(async ({ page, context }) => {
  await setAuthCookies(context)
  await mockApi(page, { authed: true })
})

test.describe('监控告警', () => {

  test('页面正常加载，显示告警列表', async ({ page }) => {
    await page.goto(routes.alerts)
    await expect(page.getByRole('heading', { name: '监控告警' })).toBeVisible()
    // mock 告警的消息文本应出现
    await expect(page.getByText('磁盘使用率 > 90%')).toBeVisible()
    await expect(page.getByText('CPU 使用率 > 80%')).toBeVisible()
    await expect(page.getByText('设备离线')).toBeVisible()
  })

  test('统计卡片显示正确数值（严重/警告/活跃/已处理）', async ({ page }) => {
    await page.goto(routes.alerts)
    // 严重 Critical: 1（a-001）
    // 警告 Warning: 2（a-002, a-003）
    // 活跃总数: 3
    // 已处理: 1（a-003 acknowledged）
    const stats = page.locator('.stat-v')
    await expect(stats.nth(0)).toHaveText('1')   // critical
    await expect(stats.nth(1)).toHaveText('2')   // warning
    await expect(stats.nth(2)).toHaveText('3')   // active total
    await expect(stats.nth(3)).toHaveText('1')   // handled
  })

  test('告警卡片显示设备/Agent/消息/时间', async ({ page }) => {
    await page.goto(routes.alerts)
    // 设备标识
    await expect(page.getByText(/dev-003/)).toBeVisible()
    await expect(page.getByText(/dev-001/)).toBeVisible()
    // Agent 标识
    await expect(page.getByText(/agent-002/)).toBeVisible()
    await expect(page.getByText(/agent-001/)).toBeVisible()
  })

  test('firing 状态告警显示"确认"和"静默"按钮', async ({ page }) => {
    await page.goto(routes.alerts)
    // a-001 / a-002 是 firing，应有确认+静默
    await expect(page.getByRole('button', { name: /确认/ })).toHaveCount(2)
    await expect(page.getByRole('button', { name: /静默/ })).toHaveCount(2)
  })

  test('acknowledged 状态告警不显示操作按钮，显示处理人', async ({ page }) => {
    await page.goto(routes.alerts)
    // a-003 是 acknowledged，应显示处理人 ops-li
    await expect(page.getByText(/ops-li/)).toBeVisible()
  })

  test.describe('确认告警 (ack)', () => {
    test('点击"确认"按钮调用 ack API', async ({ page, context }) => {
      await setAuthCookies(context)
      let ackCalled = false
      await mockApi(page, {
        authed: true,
        overrides: {
          'POST /alerts/a-001/ack': () => {
            ackCalled = true
            return { status: 200, body: { ok: true } }
          }
        }
      })
      await page.goto(routes.alerts)
      // a-001 是 critical，对应第一个确认按钮
      await page.getByRole('button', { name: /确认/ }).first().click()
      await expect.poll(() => ackCalled, { timeout: 5_000 }).toBe(true)
    })

    test('ack 失败时显示错误弹窗', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, {
        authed: true,
        overrides: {
          'POST /alerts/a-001/ack': () => ({ status: 500, body: { error: '确认失败' } })
        }
      })
      await page.goto(routes.alerts)
      await page.getByRole('button', { name: /确认/ }).first().click()
      // 应弹出错误对话框（ConfirmModal .modal-overlay），message 含"确认失败"
      await expect(page.locator('.modal-message', { hasText: '确认失败' })).toBeVisible({ timeout: 5_000 })
    })
  })

  test.describe('静默告警 (silence)', () => {
    test('点击"静默"打开时长输入对话框', async ({ page }) => {
      await page.goto(routes.alerts)
      await page.getByRole('button', { name: /静默/ }).first().click()
      // 应出现静默时长输入对话框
      await expect(page.getByText(/静默时长/)).toBeVisible({ timeout: 5_000 })
    })

    test('静默流程：输入时长 → 输入备注 → 调用 silence API', async ({ page, context }) => {
      await setAuthCookies(context)
      let silenceCalled = false
      let capturedBody = null
      await mockApi(page, {
        authed: true,
        overrides: {
          'POST /alerts/a-001/silence': (route) => {
            silenceCalled = true
            capturedBody = route.request().postDataJSON()
            return { status: 200, body: { ok: true } }
          }
        }
      })
      await page.goto(routes.alerts)
      // 1. 点击静默 → 打开时长对话框
      await page.getByRole('button', { name: /静默/ }).first().click()
      await expect(page.locator('.modal-overlay .modal-title', { hasText: '静默告警' })).toBeVisible({ timeout: 5_000 })
      // 2. 时长对话框：默认 1440，直接确认
      await page.locator('.modal-overlay .modal-actions button.primary').first().click()
      // 3. 备注对话框出现：直接确认（空备注）
      await expect(page.locator('.modal-overlay .modal-title', { hasText: '处理备注' })).toBeVisible({ timeout: 5_000 })
      await page.locator('.modal-overlay .modal-actions button.primary').first().click()
      // 4. 应调用 silence API
      await expect.poll(() => silenceCalled, { timeout: 5_000 }).toBe(true)
      expect(capturedBody).toMatchObject({ durationMinutes: 1440 })
    })

    test('静默失败显示错误弹窗', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, {
        authed: true,
        overrides: {
          'POST /alerts/a-001/silence': () => ({ status: 500, body: { error: '静默失败' } })
        }
      })
      await page.goto(routes.alerts)
      await page.getByRole('button', { name: /静默/ }).first().click()
      await page.locator('.modal-overlay .modal-actions button.primary').first().click()
      await expect(page.locator('.modal-overlay .modal-title', { hasText: '处理备注' })).toBeVisible({ timeout: 5_000 })
      await page.locator('.modal-overlay .modal-actions button.primary').first().click()
      await expect(page.locator('.modal-message', { hasText: '静默失败' })).toBeVisible({ timeout: 5_000 })
    })
  })

  test.describe('刷新', () => {
    test('点击刷新按钮重新加载告警列表', async ({ page, context }) => {
      await setAuthCookies(context)
      let fetchCount = 0
      await mockApi(page, {
        authed: true,
        overrides: {
          'GET /alerts': () => {
            fetchCount++
            return {
              status: 200,
              body: [
                { alertID: 'a-new', severity: 'critical', status: 'firing', deviceID: 'dev-x', agentID: 'agent-x', message: '新告警', createdAt: '2026-08-11T00:00:00Z' }
              ]
            }
          }
        }
      })
      await page.goto(routes.alerts)
      // 初次加载应已 fetch
      const initialCount = fetchCount
      // 点击刷新
      await page.getByRole('button', { name: /刷新/ }).click()
      await expect.poll(() => fetchCount, { timeout: 5_000 }).toBeGreaterThan(initialCount)
      // 新告警应出现
      await expect(page.getByText('新告警')).toBeVisible()
    })
  })

  test.describe('错误处理', () => {
    test('告警列表加载失败时显示错误提示', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, {
        authed: true,
        overrides: {
          'GET /alerts': () => ({ status: 500, body: { error: '告警加载失败' } })
        }
      })
      await page.goto(routes.alerts)
      await expect(page.locator('.poll-err')).toBeVisible({ timeout: 5_000 })
    })

    test('告警列表为空时显示"暂无告警"空状态', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, {
        authed: true,
        overrides: {
          'GET /alerts': () => ({ status: 200, body: [] })
        }
      })
      await page.goto(routes.alerts)
      await expect(page.getByText(/暂无告警/)).toBeVisible({ timeout: 5_000 })
    })

    test('网络错误时显示错误提示', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, {
        authed: true,
        overrides: {
          'GET /alerts': () => { throw new Error('network error') }
        }
      })
      await page.goto(routes.alerts)
      await expect(page.locator('.poll-err')).toBeVisible({ timeout: 5_000 })
    })
  })
})