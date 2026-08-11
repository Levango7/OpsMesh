// E2E: K8s 集群管理 — 集群列表 / 添加 / 删除 / 测试连接 / 资源查看 / 错误处理
import { test, expect } from '@playwright/test'
import { mockApi, setAuthCookies } from './fixtures/mock-api.js'
import { routes } from './fixtures/helpers.js'

// helper：通过文本定位 select 元素（Vue label 未用 for/id 关联）
function selectByLabel(page, labelText) {
  return page.locator('.field', { has: page.locator('label', { hasText: labelText }) }).locator('select')
}
function inputByLabel(page, labelText) {
  return page.locator('.field', { has: page.locator('label', { hasText: labelText }) }).locator('input')
}
function textareaByLabel(page, labelText) {
  return page.locator('.field', { has: page.locator('label', { hasText: labelText }) }).locator('textarea')
}

test.describe('K8s 管理', () => {

  test('页面正常加载，显示集群列表', async ({ page, context }) => {
    await setAuthCookies(context)
    await mockApi(page, { authed: true })
    await page.goto(routes.k8s)
    await expect(page.getByRole('heading', { name: 'K8s 管理' })).toBeVisible()
    // mock 集群名称应出现（b 标签中，用 exact 匹配避免与 option 冲突）
    await expect(page.getByText('prod-cluster', { exact: true })).toBeVisible()
    await expect(page.getByText('staging-cluster', { exact: true })).toBeVisible()
    // 集群 ID
    await expect(page.getByText('c-prod', { exact: true })).toBeVisible()
    await expect(page.getByText('c-staging', { exact: true })).toBeVisible()
  })

  test('集群表格显示 server / 状态 / 操作列', async ({ page, context }) => {
    await setAuthCookies(context)
    await mockApi(page, { authed: true })
    await page.goto(routes.k8s)
    // API Server
    await expect(page.getByText('https://10.10.0.1:6443', { exact: true })).toBeVisible()
    // 状态：在线/离线
    await expect(page.getByText('在线', { exact: true })).toBeVisible()
    await expect(page.getByText('离线', { exact: true })).toBeVisible()
    // 操作按钮
    await expect(page.getByRole('button', { name: '测试' })).toHaveCount(2)
    await expect(page.getByRole('button', { name: '删除' })).toHaveCount(2)
  })

  test.describe('添加集群', () => {
    test('点击"添加集群"打开添加对话框', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, { authed: true })
      await page.goto(routes.k8s)
      await page.getByRole('button', { name: '添加集群' }).click()
      // 对话框标题（modal-mask 内的 modal-head h3）
      await expect(page.locator('.modal-mask h3', { hasText: '添加集群' })).toBeVisible({ timeout: 5_000 })
      // 表单字段
      await expect(inputByLabel(page, '集群名称')).toBeVisible()
      await expect(inputByLabel(page, 'API Server')).toBeVisible()
      await expect(textareaByLabel(page, 'Kubeconfig')).toBeVisible()
    })

    test('完整填写并提交，调用 POST /k8s/clusters', async ({ page, context }) => {
      await setAuthCookies(context)
      let createCalled = false
      let capturedBody = null
      await mockApi(page, {
        authed: true,
        overrides: {
          'POST /k8s/clusters': (route) => {
            createCalled = true
            capturedBody = route.request().postDataJSON()
            return { status: 200, body: { id: 'c-new', name: 'new-cluster', server: 'https://1.2.3.4:6443', status: 'online', createdAt: '2026-08-11T00:00:00Z' } }
          }
        }
      })
      await page.goto(routes.k8s)
      await page.getByRole('button', { name: '添加集群' }).click()
      await inputByLabel(page, '集群名称').fill('new-cluster')
      await inputByLabel(page, 'API Server').fill('https://1.2.3.4:6443')
      await textareaByLabel(page, 'Kubeconfig').fill('apiVersion: v1\nclusters:\n- cluster:\n    server: https://1.2.3.4:6443')
      // 点击对话框中的确认按钮
      const modal = page.locator('.modal-mask').first()
      await modal.getByRole('button', { name: '确认' }).click()
      await expect.poll(() => createCalled, { timeout: 5_000 }).toBe(true)
      expect(capturedBody).toMatchObject({ name: 'new-cluster', server: 'https://1.2.3.4:6443' })
    })

    test('表单未填完整时显示提示', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, { authed: true })
      await page.goto(routes.k8s)
      await page.getByRole('button', { name: '添加集群' }).click()
      const modal = page.locator('.modal-mask').first()
      await modal.getByRole('button', { name: '确认' }).click()
      // 应显示"请填写完整"提示
      await expect(page.getByText(/请填写完整/)).toBeVisible({ timeout: 5_000 })
    })

    test('添加失败（后端 400）显示错误消息', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, {
        authed: true,
        overrides: {
          'POST /k8s/clusters': () => ({ status: 400, body: { error: 'kubeconfig 无效' } })
        }
      })
      await page.goto(routes.k8s)
      await page.getByRole('button', { name: '添加集群' }).click()
      await inputByLabel(page, '集群名称').fill('test')
      await inputByLabel(page, 'API Server').fill('https://1.2.3.4:6443')
      await textareaByLabel(page, 'Kubeconfig').fill('invalid')
      const modal = page.locator('.modal-mask').first()
      await modal.getByRole('button', { name: '确认' }).click()
      await expect(page.locator('.msg.err')).toBeVisible({ timeout: 5_000 })
    })
  })

  test.describe('删除集群', () => {
    test('点击删除按钮弹出原生确认对话框，确认后调用 DELETE API', async ({ page, context }) => {
      await setAuthCookies(context)
      let deleteCalled = false
      await mockApi(page, {
        authed: true,
        overrides: {
          'DELETE /k8s/clusters/c-staging': () => {
            deleteCalled = true
            return { status: 204, body: null }
          }
        }
      })
      await page.goto(routes.k8s)
      // 开始监听 dialog
      page.once('dialog', async (dialog) => {
        expect(dialog.message()).toContain('确认删除')
        await dialog.accept()
      })
      // staging 集群的删除按钮
      const stagingRow = page.locator('tr', { hasText: 'staging-cluster' })
      await stagingRow.getByRole('button', { name: '删除' }).click()
      await expect.poll(() => deleteCalled, { timeout: 5_000 }).toBe(true)
    })

    test('取消删除对话框时不调用 DELETE API', async ({ page, context }) => {
      await setAuthCookies(context)
      let deleteCalled = false
      await mockApi(page, {
        authed: true,
        overrides: {
          'DELETE /k8s/clusters/c-staging': () => {
            deleteCalled = true
            return { status: 204, body: null }
          }
        }
      })
      await page.goto(routes.k8s)
      page.once('dialog', async (dialog) => { await dialog.dismiss() })
      const stagingRow = page.locator('tr', { hasText: 'staging-cluster' })
      await stagingRow.getByRole('button', { name: '删除' }).click()
      // 等待一小段时间确认未调用
      await page.waitForTimeout(1000)
      expect(deleteCalled).toBe(false)
    })
  })

  test.describe('测试连接', () => {
    test('在线集群测试返回成功', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, { authed: true })
      await page.goto(routes.k8s)
      page.once('dialog', async (dialog) => {
        expect(dialog.message()).toContain('连接成功')
        await dialog.accept()
      })
      const prodRow = page.locator('tr', { hasText: 'prod-cluster' })
      await prodRow.getByRole('button', { name: '测试' }).click()
      await page.waitForTimeout(500)
    })

    test('离线集群测试返回失败', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, { authed: true })
      await page.goto(routes.k8s)
      page.once('dialog', async (dialog) => {
        expect(dialog.message()).toContain('连接失败')
        await dialog.accept()
      })
      const stagingRow = page.locator('tr', { hasText: 'staging-cluster' })
      await stagingRow.getByRole('button', { name: '测试' }).click()
      await page.waitForTimeout(500)
    })
  })

  test.describe('资源管理', () => {
    test('选择集群后加载 namespace 和默认资源', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, { authed: true })
      await page.goto(routes.k8s)
      // 选择 prod-cluster
      await selectByLabel(page, '选择集群').selectOption('c-prod')
      // 等待资源加载
      await expect(page.getByText('nginx-7b8f-x4k2z', { exact: true })).toBeVisible({ timeout: 5_000 })
      await expect(page.getByText('api-6c9d-mn8pq', { exact: true })).toBeVisible()
    })

    test('资源类型 tab 切换显示不同资源', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, { authed: true })
      await page.goto(routes.k8s)
      await selectByLabel(page, '选择集群').selectOption('c-prod')
      // 默认 pods
      await expect(page.getByText('nginx-7b8f-x4k2z', { exact: true })).toBeVisible({ timeout: 5_000 })
      // 切换到 deployments
      await page.getByRole('button', { name: 'Deployment', exact: true }).click()
      await expect(page.getByText('nginx-deploy', { exact: true })).toBeVisible({ timeout: 5_000 })
      await expect(page.getByText('api-deploy', { exact: true })).toBeVisible()
      // 切换到 nodes
      await page.getByRole('button', { name: 'Node', exact: true }).click()
      await expect(page.getByText('node-1', { exact: true })).toBeVisible({ timeout: 5_000 })
      await expect(page.getByText('node-2', { exact: true })).toBeVisible()
    })

    test('Pod 行显示"查看日志"和"删除"按钮', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, { authed: true })
      await page.goto(routes.k8s)
      await selectByLabel(page, '选择集群').selectOption('c-prod')
      await expect(page.getByRole('button', { name: '查看日志' })).toHaveCount(3)
      // 删除按钮（红色样式）
      const podRows = page.locator('tr', { hasText: /nginx-7b8f|api-6c9d|worker-failed/ })
      await expect(podRows.first().getByRole('button', { name: '删除' })).toBeVisible({ timeout: 5_000 })
    })

    test('点击"查看日志"打开日志对话框', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, { authed: true })
      await page.goto(routes.k8s)
      await selectByLabel(page, '选择集群').selectOption('c-prod')
      await page.getByRole('button', { name: '查看日志' }).first().click()
      // 日志对话框（modal-mask 内 h3 含"查看日志"）
      await expect(page.locator('.modal-mask h3', { hasText: '查看日志' })).toBeVisible({ timeout: 5_000 })
      // 应显示日志内容
      await expect(page.locator('.logs-block')).toBeVisible()
    })

    test('Deployment 行显示"扩缩容"和"重启"按钮', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, { authed: true })
      await page.goto(routes.k8s)
      await selectByLabel(page, '选择集群').selectOption('c-prod')
      await page.getByRole('button', { name: 'Deployment', exact: true }).click()
      await expect(page.getByRole('button', { name: '扩缩容' })).toHaveCount(2)
      await expect(page.getByRole('button', { name: '重启' })).toHaveCount(2)
    })

    test('点击"扩缩容"打开对话框，提交后调用 scale API', async ({ page, context }) => {
      await setAuthCookies(context)
      let scaleCalled = false
      let capturedBody = null
      await mockApi(page, {
        authed: true,
        overrides: {
          'POST /k8s/clusters/c-prod/deployments/default/nginx-deploy/scale': (route) => {
            scaleCalled = true
            capturedBody = route.request().postDataJSON()
            return { status: 200, body: { name: 'nginx-deploy', replicas: 5 } }
          }
        }
      })
      await page.goto(routes.k8s)
      await selectByLabel(page, '选择集群').selectOption('c-prod')
      await page.getByRole('button', { name: 'Deployment', exact: true }).click()
      await expect(page.getByText('nginx-deploy', { exact: true })).toBeVisible({ timeout: 5_000 })
      // 点击 nginx-deploy 行的扩缩容
      const nginxRow = page.locator('tr', { hasText: 'nginx-deploy' })
      await nginxRow.getByRole('button', { name: '扩缩容' }).click()
      // 对话框中输入副本数
      await expect(inputByLabel(page, '目标副本数')).toBeVisible({ timeout: 5_000 })
      await inputByLabel(page, '目标副本数').fill('5')
      const modal = page.locator('.modal-mask').first()
      await modal.getByRole('button', { name: '确认' }).click()
      await expect.poll(() => scaleCalled, { timeout: 5_000 }).toBe(true)
      expect(capturedBody).toMatchObject({ replicas: 5 })
    })

    test('点击"重启"触发 restart API（带原生确认）', async ({ page, context }) => {
      await setAuthCookies(context)
      let restartCalled = false
      await mockApi(page, {
        authed: true,
        overrides: {
          'POST /k8s/clusters/c-prod/deployments/default/nginx-deploy/restart': () => {
            restartCalled = true
            return { status: 200, body: { status: 'ok', restartedAt: '2026-08-11T00:00:00Z' } }
          }
        }
      })
      await page.goto(routes.k8s)
      await selectByLabel(page, '选择集群').selectOption('c-prod')
      await page.getByRole('button', { name: 'Deployment', exact: true }).click()
      await expect(page.getByText('nginx-deploy', { exact: true })).toBeVisible({ timeout: 5_000 })
      page.once('dialog', async (dialog) => { await dialog.accept() })
      const nginxRow = page.locator('tr', { hasText: 'nginx-deploy' })
      await nginxRow.getByRole('button', { name: '重启' }).click()
      await expect.poll(() => restartCalled, { timeout: 5_000 }).toBe(true)
    })

    test('命名空间过滤输入后回车触发资源刷新', async ({ page, context }) => {
      await setAuthCookies(context)
      let lastNsParam = null
      await mockApi(page, {
        authed: true,
        overrides: {
          'GET /k8s/clusters/c-prod/pods': (route) => {
            const url = new URL(route.request().url())
            lastNsParam = url.searchParams.get('namespace')
            return { status: 200, body: { pods: [{ name: 'ns-pod', namespace: 'opsmesh', status: 'Running', podIP: '10.244.0.10', nodeIP: '10.10.0.2', restarts: 0, age: '1h' }] } }
          }
        }
      })
      await page.goto(routes.k8s)
      await selectByLabel(page, '选择集群').selectOption('c-prod')
      // 等待初始 pods 加载（override 返回 ns-pod）
      await expect(page.getByText('ns-pod', { exact: true })).toBeVisible({ timeout: 5_000 })
      // 输入命名空间并回车
      await page.getByPlaceholder(/输入命名空间/).fill('opsmesh')
      await page.getByPlaceholder(/输入命名空间/).press('Enter')
      await expect.poll(() => lastNsParam, { timeout: 5_000 }).toBe('opsmesh')
    })
  })

  test.describe('错误处理', () => {
    test('集群列表加载失败时显示错误提示', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, {
        authed: true,
        overrides: {
          'GET /k8s/clusters': () => ({ status: 500, body: { error: '集群列表加载失败' } })
        }
      })
      await page.goto(routes.k8s)
      await expect(page.locator('.poll-err')).toBeVisible({ timeout: 5_000 })
    })

    test('集群列表为空时显示"暂无集群"', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, {
        authed: true,
        overrides: {
          'GET /k8s/clusters': () => ({ status: 200, body: { clusters: [] } })
        }
      })
      await page.goto(routes.k8s)
      await expect(page.getByText(/暂无集群/)).toBeVisible({ timeout: 5_000 })
    })

    test('资源加载失败时显示错误提示', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, {
        authed: true,
        overrides: {
          'GET /k8s/clusters/c-prod/pods': () => ({ status: 500, body: { error: 'Pod 列表加载失败' } })
        }
      })
      await page.goto(routes.k8s)
      await selectByLabel(page, '选择集群').selectOption('c-prod')
      await expect(page.locator('.poll-err')).toBeVisible({ timeout: 5_000 })
    })

    test('网络错误时显示错误提示', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, {
        authed: true,
        overrides: {
          'GET /k8s/clusters': () => { throw new Error('network error') }
        }
      })
      await page.goto(routes.k8s)
      await expect(page.locator('.poll-err')).toBeVisible({ timeout: 5_000 })
    })
  })
})
