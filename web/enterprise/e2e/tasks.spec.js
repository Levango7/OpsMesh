// E2E: 任务下发流程 — 列表 / 下发 / 取消 / 状态过滤 / 错误处理
// 断言策略：优先 data-testid（语言无关），数据值（任务ID/命令）保留文本断言。
import { test, expect } from '@playwright/test'
import { mockApi, setAuthCookies } from './fixtures/mock-api.js'
import { routes } from './fixtures/helpers.js'

test.describe('任务下发', () => {

  test('页面正常加载，显示任务列表', async ({ page, context }) => {
    await setAuthCookies(context)
    await mockApi(page, { authed: true })
    await page.goto(routes.tasks)
    await expect(page.getByTestId('tasks-title')).toBeVisible()
    // 任务表格中出现 mock 任务（用 exact 匹配避免与 agentID 冲突）
    await expect(page.getByText('t-001', { exact: true })).toBeVisible()
    await expect(page.getByText('t-002', { exact: true })).toBeVisible()
    await expect(page.getByText('t-003', { exact: true })).toBeVisible()
    // 命令列
    await expect(page.getByText('uptime', { exact: true })).toBeVisible()
    await expect(page.getByText('df -h', { exact: true })).toBeVisible()
  })

  test('显示下发任务表单（采集端/类型/命令）', async ({ page, context }) => {
    await setAuthCookies(context)
    await mockApi(page, { authed: true })
    await page.goto(routes.tasks)
    await expect(page.getByTestId('task-form-title')).toBeVisible()
    // 采集端 select / 类型 select / 命令 input / 下发按钮（data-testid）
    await expect(page.getByTestId('task-agent-select')).toBeVisible()
    await expect(page.getByTestId('task-type-select')).toBeVisible()
    await expect(page.getByTestId('task-command-input')).toBeVisible()
    await expect(page.getByTestId('task-submit-btn')).toBeVisible()
  })

  test('采集端下拉框加载 agent 列表', async ({ page, context }) => {
    await setAuthCookies(context)
    await mockApi(page, { authed: true })
    await page.goto(routes.tasks)
    // 检查 select 的 option 值（option 在未打开下拉时 Playwright 认为 hidden，故用 evaluate）
    const options = await page.getByTestId('task-agent-select').evaluate((sel) =>
      Array.from(sel.options).map((o) => o.value)
    )
    expect(options).toContain('agent-001')
    expect(options).toContain('agent-002')
  })

  test('类型下拉框包含 shell / file / service', async ({ page, context }) => {
    await setAuthCookies(context)
    await mockApi(page, { authed: true })
    await page.goto(routes.tasks)
    const options = await page.getByTestId('task-type-select').evaluate((sel) =>
      Array.from(sel.options).map((o) => o.value)
    )
    expect(options).toEqual(expect.arrayContaining(['shell', 'file', 'service']))
  })

  test.describe('下发任务', () => {
    test('完整填写表单并提交，显示成功消息', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, { authed: true })
      await page.goto(routes.tasks)
      // 选择采集端
      await page.getByTestId('task-agent-select').selectOption('agent-001')
      // 类型默认 shell
      // 填写命令
      await page.getByTestId('task-command-input').fill('ls -la')
      // 提交
      await page.getByTestId('task-submit-btn').click()
      // 应显示成功消息（包含 [200]）
      await expect(page.locator('.msg.ok')).toBeVisible({ timeout: 5_000 })
    })

    test('提交后调用 POST /tasks', async ({ page, context }) => {
      await setAuthCookies(context)
      let createCalled = false
      let capturedBody = null
      await mockApi(page, {
        authed: true,
        overrides: {
          'POST /tasks': (route) => {
            createCalled = true
            capturedBody = route.request().postDataJSON()
            return { status: 200, body: { taskID: 't-new', agentID: 'agent-001', type: 'shell', command: 'uptime', status: 'pending' } }
          }
        }
      })
      await page.goto(routes.tasks)
      await page.getByTestId('task-agent-select').selectOption('agent-001')
      await page.getByTestId('task-command-input').fill('uptime')
      await page.getByTestId('task-submit-btn').click()
      await expect.poll(() => createCalled, { timeout: 5_000 }).toBe(true)
      expect(capturedBody).toMatchObject({ agentID: 'agent-001', command: 'uptime' })
    })

    test('下发失败（后端 400）显示错误消息', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, {
        authed: true,
        overrides: {
          'POST /tasks': () => ({ status: 400, body: { error: '命令不能为空' } })
        }
      })
      await page.goto(routes.tasks)
      await page.getByTestId('task-agent-select').selectOption('agent-001')
      await page.getByTestId('task-command-input').fill('test')
      await page.getByTestId('task-submit-btn').click()
      await expect(page.locator('.msg.err')).toBeVisible({ timeout: 5_000 })
    })
  })

  test.describe('取消任务', () => {
    test('pending/running 任务行显示"取消"按钮', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, { authed: true })
      await page.goto(routes.tasks)
      // t-002 是 running，应有取消按钮
      const t002Row = page.locator('tr', { hasText: 't-002' })
      await expect(t002Row.getByTestId('task-cancel-btn')).toBeVisible()
      // t-003 是 pending，也应有取消按钮
      const t003Row = page.locator('tr', { hasText: 't-003' })
      await expect(t003Row.getByTestId('task-cancel-btn')).toBeVisible()
    })

    test('completed 任务行不显示"取消"按钮', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, { authed: true })
      await page.goto(routes.tasks)
      const t001Row = page.locator('tr', { hasText: 't-001' })
      await expect(t001Row.getByTestId('task-cancel-btn')).toHaveCount(0)
    })

    test('点击取消按钮调用 cancel API', async ({ page, context }) => {
      await setAuthCookies(context)
      let cancelCalled = false
      await mockApi(page, {
        authed: true,
        overrides: {
          'POST /tasks/t-002/cancel': () => {
            cancelCalled = true
            return { status: 200, body: { ok: true } }
          }
        }
      })
      await page.goto(routes.tasks)
      const t002Row = page.locator('tr', { hasText: 't-002' })
      await t002Row.getByTestId('task-cancel-btn').click()
      // 第六轮迁移：取消已改 ConfirmModal（Teleport 到 body，用内部 confirm-modal 定位），确认后才发请求
      await expect(page.getByTestId('confirm-modal')).toBeVisible({ timeout: 5_000 })
      await page.getByTestId('confirm-modal-confirm').click()
      await expect.poll(() => cancelCalled, { timeout: 5_000 }).toBe(true)
    })
  })

  test.describe('状态过滤', () => {
    test('状态过滤下拉框包含所有状态选项', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, { authed: true })
      await page.goto(routes.tasks)
      const options = await page.getByTestId('task-status-filter').evaluate((sel) =>
        Array.from(sel.options).map((o) => o.value)
      )
      // 实际选项值为 'cancelled'（与后端任务状态机一致，TasksView.vue:62）
      expect(options).toEqual(expect.arrayContaining(['', 'pending', 'running', 'done', 'failed', 'cancelled']))
    })

    test('切换状态过滤触发 API 请求（带 status 参数）', async ({ page, context }) => {
      await setAuthCookies(context)
      let lastStatusParam = null
      await mockApi(page, {
        authed: true,
        overrides: {
          'GET /tasks': (route) => {
            const url = new URL(route.request().url())
            lastStatusParam = url.searchParams.get('status')
            return { status: 200, body: [] }
          }
        }
      })
      await page.goto(routes.tasks)
      await page.getByTestId('task-status-filter').selectOption('running')
      await expect.poll(() => lastStatusParam, { timeout: 5_000 }).toBe('running')
    })
  })

  test.describe('错误处理', () => {
    test('任务列表加载失败时显示错误提示', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, {
        authed: true,
        overrides: {
          'GET /tasks': () => ({ status: 500, body: { error: '任务列表加载失败' } })
        }
      })
      await page.goto(routes.tasks)
      await expect(page.locator('.poll-err')).toBeVisible({ timeout: 5_000 })
    })

    test('任务列表为空时显示空状态', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, {
        authed: true,
        overrides: {
          'GET /tasks': () => ({ status: 200, body: [] })
        }
      })
      await page.goto(routes.tasks)
      await expect(page.getByTestId('dt-empty')).toBeVisible({ timeout: 5_000 })
    })
  })

  test.describe('权限不足', () => {
    test('无 task:write 权限时不显示下发表单，显示提示', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, {
        authed: true,
        overrides: {
          'GET /auth/me': () => ({
            status: 200,
            body: {
              id: 'u-low', username: 'low', email: 'low@b.c', status: 'active',
              role_ids: ['r-low'], permissions: ['task:read']  // 只读，无 task:write
            }
          })
        }
      })
      await page.goto(routes.tasks)
      // 不应显示下发表单与下发按钮
      await expect(page.getByTestId('task-form-title')).toHaveCount(0)
      await expect(page.getByTestId('task-submit-btn')).toHaveCount(0)
    })
  })
})
