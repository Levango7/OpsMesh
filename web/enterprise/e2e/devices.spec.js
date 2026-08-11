// E2E: 设备纳管流程 — 列表 / 详情 / 纳管 / 错误处理
import { test, expect } from '@playwright/test'
import { mockApi, setAuthCookies } from './fixtures/mock-api.js'
import { routes } from './fixtures/helpers.js'

test.describe('设备纳管', () => {

  test('页面正常加载，显示设备列表（按网段分组）', async ({ page, context }) => {
    await setAuthCookies(context)
    await mockApi(page, { authed: true })
    await page.goto(routes.devices)
    // 标题
    await expect(page.getByRole('heading', { name: '设备纳管' })).toBeVisible()
    // 网段分组标题（h3）
    await expect(page.getByRole('heading', { name: /10\.0\.1\.0\/24/ })).toBeVisible()
    await expect(page.getByRole('heading', { name: /10\.0\.2\.0\/24/ })).toBeVisible()
    // 设备主机名出现在表格中
    await expect(page.getByText('web-node-1')).toBeVisible()
    await expect(page.getByText('web-node-2')).toBeVisible()
    await expect(page.getByText('db-node-1')).toBeVisible()
  })

  test('表格显示设备关键字段（DeviceID / IP / 状态 / OS）', async ({ page, context }) => {
    await setAuthCookies(context)
    await mockApi(page, { authed: true })
    await page.goto(routes.devices)
    // DeviceID 列
    await expect(page.getByText('dev-001')).toBeVisible()
    await expect(page.getByText('dev-003')).toBeVisible()
    // IP 列
    await expect(page.getByText('10.0.1.11')).toBeVisible()
    await expect(page.getByText('10.0.2.21')).toBeVisible()
    // OS 列
    await expect(page.getByText('CentOS 7')).toBeVisible()
    await expect(page.getByText('Ubuntu 22.04')).toBeVisible()
  })

  test('点击"详情"按钮跳转到设备详情页', async ({ page, context }) => {
    await setAuthCookies(context)
    await mockApi(page, { authed: true })
    await page.goto(routes.devices)
    // 第一行的"详情"按钮
    const detailBtn = page.getByRole('button', { name: '详情' }).first()
    await detailBtn.click()
    await expect(page).toHaveURL(/\/devices\/dev-001$/)
  })

  test('点击"下发任务"按钮跳转到任务页（带 device query）', async ({ page, context }) => {
    await setAuthCookies(context)
    await mockApi(page, { authed: true })
    await page.goto(routes.devices)
    const dispatchBtn = page.getByRole('button', { name: '下发任务' }).first()
    await dispatchBtn.click()
    await expect(page).toHaveURL(/\/tasks\?device=dev-001$/)
  })

  test('点击表格行打开详情抽屉', async ({ page, context }) => {
    await setAuthCookies(context)
    await mockApi(page, { authed: true })
    await page.goto(routes.devices)
    // 点击第一行（含 web-node-1 的行）
    await page.getByText('web-node-1').click()
    // 抽屉应出现，标题前缀"设备 "
    await expect(page.getByText(/设备\s*dev-001/)).toBeVisible({ timeout: 5_000 })
    // 抽屉中显示 IP / 采集端 / 租户（完整文本）
    await expect(page.getByText(/IP:\s*10\.0\.1\.11/)).toBeVisible({ timeout: 5_000 })
  })

  test.describe('纳管 discovered 设备', () => {
    test('discovered 设备抽屉显示"推送 Agent 纳管"按钮', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, { authed: true })
      await page.goto(routes.devices)
      // web-node-2 是 discovered 状态，点击对应行打开抽屉
      await page.getByText('web-node-2').click()
      await expect(page.getByRole('button', { name: /推送 Agent 纳管/ })).toBeVisible({ timeout: 5_000 })
    })

    test('点击"推送 Agent 纳管"触发 provision API', async ({ page, context }) => {
      await setAuthCookies(context)
      let provisionCalled = false
      await mockApi(page, {
        authed: true,
        overrides: {
          'POST /devices/dev-002/provision': () => {
            provisionCalled = true
            return { status: 200, body: { ok: true, message: 'provisioning started' } }
          }
        }
      })
      await page.goto(routes.devices)
      await page.getByText('web-node-2').click()
      await page.getByRole('button', { name: /推送 Agent 纳管/ }).click()
      // 等待 provision 调用
      await expect.poll(() => provisionCalled, { timeout: 5_000 }).toBe(true)
    })

    test('managed 设备抽屉不显示"推送 Agent 纳管"按钮', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, { authed: true })
      await page.goto(routes.devices)
      // web-node-1 是 managed 状态
      await page.getByText('web-node-1').click()
      await expect(page.getByRole('button', { name: /推送 Agent 纳管/ })).toHaveCount(0)
    })
  })

  test.describe('错误处理', () => {
    test('设备列表加载失败时显示错误提示', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, {
        authed: true,
        overrides: {
          'GET /devices': () => ({ status: 500, body: { error: '设备列表加载失败' } })
        }
      })
      await page.goto(routes.devices)
      // store.error 显示在 .poll-err
      await expect(page.locator('.poll-err')).toBeVisible({ timeout: 5_000 })
    })

    test('设备列表为空时显示空状态', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, {
        authed: true,
        overrides: {
          'GET /devices': () => ({ status: 200, body: {} })
        }
      })
      await page.goto(routes.devices)
      await expect(page.getByText(/暂无纳管设备/)).toBeVisible({ timeout: 5_000 })
    })

    test('网络错误（连接拒绝）时显示错误提示', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, {
        authed: true,
        overrides: {
          'GET /devices': () => { throw new Error('network error') }
        }
      })
      await page.goto(routes.devices)
      await expect(page.locator('.poll-err')).toBeVisible({ timeout: 5_000 })
    })
  })

  test.describe('权限不足', () => {
    test('无 device:read 权限的用户被重定向到总览', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, {
        authed: true,
        overrides: {
          'GET /auth/me': () => ({
            status: 200,
            body: {
              id: 'u-low', username: 'low', email: 'low@b.c', status: 'active',
              role_ids: ['r-low'], permissions: ['some:other']  // 非空但无 device:read
            }
          })
        }
      })
      await page.goto(routes.devices)
      await expect(page).toHaveURL(/\/overview$/)
    })
  })
})
