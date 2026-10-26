// E2E: K8s 管理 — 拆分为「集群管理 / 资源管理」两个子页面
// 断言策略：优先 data-testid（语言无关），数据值（集群名/资源名）保留文本断言。
import { test, expect } from '@playwright/test'
import { mockApi, setAuthCookies } from './fixtures/mock-api.js'
import { routes } from './fixtures/helpers.js'

test.describe('K8s 管理', () => {

  test.describe('集群管理子页', () => {
    test('页面正常加载，显示集群列表', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, { authed: true })
      await page.goto(routes.k8sClusters)
      await expect(page.getByTestId('k8s-title')).toBeVisible()
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
      await page.goto(routes.k8sClusters)
      // API Server
      await expect(page.getByText('https://10.10.0.1:6443', { exact: true })).toBeVisible()
      // 状态：在线/离线
      await expect(page.getByText('在线', { exact: true })).toBeVisible()
      await expect(page.getByText('离线', { exact: true })).toBeVisible()
      // 操作按钮（data-testid）
      await expect(page.getByTestId('k8s-test-btn')).toHaveCount(2)
      await expect(page.getByTestId('k8s-delete-cluster-btn')).toHaveCount(2)
    })

    test('子页签可跳转到资源管理子页', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, { authed: true })
      await page.goto(routes.k8sClusters)
      await page.getByTestId('k8s-subnav-resources').click()
      await expect(page).toHaveURL(/k8s\/resources/)
      await expect(page.getByTestId('k8s-resources-title')).toBeVisible()
    })

    test('点击"资源管理"按钮跳转到资源子页并选中该集群', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, { authed: true })
      await page.goto(routes.k8sClusters)
      const prodRow = page.locator('tr', { hasText: 'prod-cluster' })
      await prodRow.getByTestId('k8s-manage-btn').click()
      await expect(page).toHaveURL(/k8s\/resources/)
      // 集群下拉应已选中 c-prod
      await expect(page.getByTestId('k8s-cluster-select')).toHaveValue('c-prod')
    })

    test.describe('添加集群', () => {
      test('点击"添加集群"打开添加对话框', async ({ page, context }) => {
        await setAuthCookies(context)
        await mockApi(page, { authed: true })
        await page.goto(routes.k8sClusters)
        await page.getByTestId('k8s-add-cluster-btn').click()
        // 对话框（data-testid）与表单字段
        await expect(page.getByTestId('k8s-add-modal')).toBeVisible({ timeout: 5_000 })
        await expect(page.getByTestId('k8s-add-name')).toBeVisible()
        await expect(page.getByTestId('k8s-add-server')).toBeVisible()
        await expect(page.getByTestId('k8s-add-kubeconfig')).toBeVisible()
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
        await page.goto(routes.k8sClusters)
        await page.getByTestId('k8s-add-cluster-btn').click()
        await page.getByTestId('k8s-add-name').fill('new-cluster')
        await page.getByTestId('k8s-add-server').fill('https://1.2.3.4:6443')
        await page.getByTestId('k8s-add-kubeconfig').fill('apiVersion: v1\nclusters:\n- cluster:\n    server: https://1.2.3.4:6443')
        // 点击对话框中的确认按钮
        await page.getByTestId('k8s-add-confirm').click()
        await expect.poll(() => createCalled, { timeout: 5_000 }).toBe(true)
        expect(capturedBody).toMatchObject({ name: 'new-cluster', server: 'https://1.2.3.4:6443' })
      })

      test('表单未填完整时显示提示', async ({ page, context }) => {
        await setAuthCookies(context)
        await mockApi(page, { authed: true })
        await page.goto(routes.k8sClusters)
        await page.getByTestId('k8s-add-cluster-btn').click()
        await page.getByTestId('k8s-add-confirm').click()
        // 应显示"请填写完整"提示（文案本身即断言对象）
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
        await page.goto(routes.k8sClusters)
        await page.getByTestId('k8s-add-cluster-btn').click()
        await page.getByTestId('k8s-add-name').fill('test')
        await page.getByTestId('k8s-add-server').fill('https://1.2.3.4:6443')
        await page.getByTestId('k8s-add-kubeconfig').fill('invalid')
        await page.getByTestId('k8s-add-confirm').click()
        await expect(page.locator('.msg.err')).toBeVisible({ timeout: 5_000 })
      })
    })

    test.describe('删除集群', () => {
      test('点击删除按钮弹出确认对话框，确认后调用 DELETE API', async ({ page, context }) => {
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
        await page.goto(routes.k8sClusters)
        // 原生 confirm 已改 ConfirmModal；组件 Teleport 到 body，
        // 组件标签上的 data-testid 不随内容渲染（attrs fallthrough 在 Teleport 场景不可靠），
        // 用内部稳定的 confirm-modal overlay + 对话框标题定位。
        const stagingRow = page.locator('tr', { hasText: 'staging-cluster' })
        await stagingRow.getByTestId('k8s-delete-cluster-btn').click()
        const modal = page.getByTestId('confirm-modal')
        await expect(modal).toBeVisible({ timeout: 5_000 })
        await expect(modal.locator('.modal-title')).toContainText('删除')
        await page.getByTestId('confirm-modal-confirm').click()
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
        await page.goto(routes.k8sClusters)
        const stagingRow = page.locator('tr', { hasText: 'staging-cluster' })
        await stagingRow.getByTestId('k8s-delete-cluster-btn').click()
        // 等待 ConfirmModal 出现后点取消
        const modal = page.getByTestId('confirm-modal')
        await expect(modal).toBeVisible({ timeout: 5_000 })
        await page.getByTestId('confirm-modal-cancel').click()
        // 等待一小段时间确认未调用
        await page.waitForTimeout(1000)
        expect(deleteCalled).toBe(false)
      })
    })

    test.describe('测试连接', () => {
      test('在线集群测试返回成功提示', async ({ page, context }) => {
        await setAuthCookies(context)
        await mockApi(page, { authed: true })
        await page.goto(routes.k8sClusters)
        const prodRow = page.locator('tr', { hasText: 'prod-cluster' })
        await prodRow.getByTestId('k8s-test-btn').click()
        // onTest 使用 toast 而非原生 dialog，断言 toast 文案
        await expect(page.getByText(/连接成功/)).toBeVisible({ timeout: 5_000 })
      })

      test('离线集群测试返回失败提示', async ({ page, context }) => {
        await setAuthCookies(context)
        await mockApi(page, { authed: true })
        await page.goto(routes.k8sClusters)
        const stagingRow = page.locator('tr', { hasText: 'staging-cluster' })
        await stagingRow.getByTestId('k8s-test-btn').click()
        await expect(page.getByText(/连接失败/)).toBeVisible({ timeout: 5_000 })
      })
    })
  })

  test.describe('资源管理子页', () => {
    test('选择集群后加载 namespace 和默认资源', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, { authed: true })
      await page.goto(routes.k8sResources)
      // 选择 prod-cluster（data-testid）
      await page.getByTestId('k8s-cluster-select').selectOption('c-prod')
      // 等待资源加载
      await expect(page.getByText('nginx-7b8f-x4k2z', { exact: true })).toBeVisible({ timeout: 5_000 })
      await expect(page.getByText('api-6c9d-mn8pq', { exact: true })).toBeVisible()
    })

    test('资源类型 tab 切换显示不同资源', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, { authed: true })
      await page.goto(routes.k8sResources)
      await page.getByTestId('k8s-cluster-select').selectOption('c-prod')
      // 默认 pods
      await expect(page.getByText('nginx-7b8f-x4k2z', { exact: true })).toBeVisible({ timeout: 5_000 })
      // 切换到 deployments（data-testid tab）
      await page.getByTestId('k8s-tab-deployments').click()
      await expect(page.getByText('nginx-deploy', { exact: true })).toBeVisible({ timeout: 5_000 })
      await expect(page.getByText('api-deploy', { exact: true })).toBeVisible()
      // 切换到 nodes
      await page.getByTestId('k8s-tab-nodes').click()
      await expect(page.getByText('node-1', { exact: true })).toBeVisible({ timeout: 5_000 })
      await expect(page.getByText('node-2', { exact: true })).toBeVisible()
    })

    test('Pod 行显示"查看日志"和"删除"按钮', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, { authed: true })
      await page.goto(routes.k8sResources)
      await page.getByTestId('k8s-cluster-select').selectOption('c-prod')
      await expect(page.getByTestId('k8s-view-logs-btn')).toHaveCount(3)
      // 删除按钮（红色样式）
      const podRows = page.locator('tr', { hasText: /nginx-7b8f|api-6c9d|worker-failed/ })
      await expect(podRows.first().getByTestId('k8s-delete-pod-btn')).toBeVisible({ timeout: 5_000 })
    })

    test('点击"查看日志"打开日志对话框', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, { authed: true })
      await page.goto(routes.k8sResources)
      await page.getByTestId('k8s-cluster-select').selectOption('c-prod')
      await page.getByTestId('k8s-view-logs-btn').first().click()
      // 日志对话框（data-testid）
      await expect(page.getByTestId('k8s-logs-modal')).toBeVisible({ timeout: 5_000 })
      // 应显示日志内容
      await expect(page.locator('.logs-block')).toBeVisible()
    })

    test('Deployment 行显示"扩缩容"和"重启"按钮', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, { authed: true })
      await page.goto(routes.k8sResources)
      await page.getByTestId('k8s-cluster-select').selectOption('c-prod')
      await page.getByTestId('k8s-tab-deployments').click()
      await expect(page.getByTestId('k8s-scale-btn')).toHaveCount(2)
      await expect(page.getByTestId('k8s-restart-btn')).toHaveCount(2)
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
      await page.goto(routes.k8sResources)
      await page.getByTestId('k8s-cluster-select').selectOption('c-prod')
      await page.getByTestId('k8s-tab-deployments').click()
      await expect(page.getByText('nginx-deploy', { exact: true })).toBeVisible({ timeout: 5_000 })
      // 点击 nginx-deploy 行的扩缩容
      const nginxRow = page.locator('tr', { hasText: 'nginx-deploy' })
      await nginxRow.getByTestId('k8s-scale-btn').click()
      // 对话框中输入副本数（data-testid）
      await expect(page.getByTestId('k8s-scale-replicas')).toBeVisible({ timeout: 5_000 })
      await page.getByTestId('k8s-scale-replicas').fill('5')
      await page.getByTestId('k8s-scale-confirm').click()
      await expect.poll(() => scaleCalled, { timeout: 5_000 }).toBe(true)
      expect(capturedBody).toMatchObject({ replicas: 5 })
    })

    test('点击"重启"触发 restart API（带确认对话框）', async ({ page, context }) => {
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
      await page.goto(routes.k8sResources)
      await page.getByTestId('k8s-cluster-select').selectOption('c-prod')
      await page.getByTestId('k8s-tab-deployments').click()
      await expect(page.getByText('nginx-deploy', { exact: true })).toBeVisible({ timeout: 5_000 })
      // 重启确认已改 ConfirmModal（Teleport 到 body，用内部 confirm-modal 定位）
      const nginxRow = page.locator('tr', { hasText: 'nginx-deploy' })
      await nginxRow.getByTestId('k8s-restart-btn').click()
      const modal = page.getByTestId('confirm-modal')
      await expect(modal).toBeVisible({ timeout: 5_000 })
      await page.getByTestId('confirm-modal-confirm').click()
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
      await page.goto(routes.k8sResources)
      await page.getByTestId('k8s-cluster-select').selectOption('c-prod')
      // 等待初始 pods 加载（override 返回 ns-pod）
      await expect(page.getByText('ns-pod', { exact: true })).toBeVisible({ timeout: 5_000 })
      // 输入命名空间并回车（data-testid）
      await page.getByTestId('k8s-namespace-input').fill('opsmesh')
      await page.getByTestId('k8s-namespace-input').press('Enter')
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
      await page.goto(routes.k8sClusters)
      await expect(page.locator('.poll-err')).toBeVisible({ timeout: 5_000 })
    })

    test('集群列表为空时显示空状态', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, {
        authed: true,
        overrides: {
          'GET /k8s/clusters': () => ({ status: 200, body: { clusters: [] } })
        }
      })
      await page.goto(routes.k8sClusters)
      await expect(page.getByTestId('dt-empty')).toBeVisible({ timeout: 5_000 })
    })

    test('资源加载失败时显示错误提示', async ({ page, context }) => {
      await setAuthCookies(context)
      await mockApi(page, {
        authed: true,
        overrides: {
          'GET /k8s/clusters/c-prod/pods': () => ({ status: 500, body: { error: 'Pod 列表加载失败' } })
        }
      })
      await page.goto(routes.k8sResources)
      await page.getByTestId('k8s-cluster-select').selectOption('c-prod')
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
      await page.goto(routes.k8sClusters)
      await expect(page.locator('.poll-err')).toBeVisible({ timeout: 5_000 })
    })
  })
})
