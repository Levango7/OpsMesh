// Playwright 路由拦截工具 — 模拟后端 /api/v1/* 接口
// 用法：await mockApi(page, { authed: true })  安装所有 mock 路由
//      传 options.authed=false 则 /auth/me 返回 401（未登录场景）
//      传 options.overrides = { '/devices': customData } 可覆盖特定端点
import {
  mockUser, mockDevices, mockDeviceDetail, mockDeviceDetailDiscovered, mockAgents,
  mockTasks, mockAlerts, mockClusters, mockNamespaces,
  mockPods, mockDeployments, mockNodes, mockPodLogs
} from './mock-data.js'

const API = '/api/v1'

// 默认 mock 端点表：path → handler(req)
// handler 返回 { status, body, headers } 或直接返回数据（默认 200 + JSON）
function defaultEndpoints({ authed = true } = {}) {
  return {
    // —— 认证 ——
    'POST /auth/login': (req) => {
      // 简单校验：username/password 非空即放行
      return { status: 200, body: { user: mockUser } }
    },
    'POST /auth/register': () => ({ status: 200, body: { user: { ...mockUser, username: 'newuser' } } }),
    'POST /auth/logout': () => ({ status: 200, body: {} }),
    'POST /auth/refresh': () => ({ status: 200, body: {} }),
    'GET /auth/me': () => authed
      ? { status: 200, body: mockUser }
      : { status: 401, body: { error: 'unauthorized' } },

    // —— 设备 ——
    'GET /devices': () => ({ status: 200, body: mockDevices }),
    'GET /agents': () => ({ status: 200, body: mockAgents }),
    'GET /devices/dev-001': () => ({ status: 200, body: mockDeviceDetail }),
    'GET /devices/dev-002': () => ({ status: 200, body: mockDeviceDetailDiscovered }),
    'POST /devices/dev-002/provision': () => ({
      status: 200, body: { ok: true, message: 'provisioning started' }
    }),
    'GET /devices/dev-001/metrics': () => ({
      status: 200,
      body: {
        hostname: 'web-node-1',
        cpu: { usage: 35.2, model: 'Intel Xeon', cores: 4 },
        memory: { total: 8192, used: 3200, available: 4992 },
        disks: [{ device: 'sda1', total: 100, used: 45, free: 55 }],
        network: [{ nic: 'eth0', status: 'up', rx: 1024, tx: 2048 }],
        services: [{ name: 'nginx', enabled: true, active: true }],
        processCount: 120,
        collectedAt: '2026-08-11T00:00:00Z'
      }
    }),

    // —— 任务 ——
    'GET /tasks': () => ({ status: 200, body: mockTasks }),
    'POST /tasks': () => ({
      status: 200,
      body: { taskID: 't-new', agentID: 'agent-001', type: 'shell', command: 'uptime', status: 'pending' }
    }),
    'POST /tasks/t-002/cancel': () => ({ status: 200, body: { ok: true } }),

    // —— 告警 ——
    'GET /alerts': () => ({ status: 200, body: mockAlerts }),
    'POST /alerts/a-001/ack': () => ({ status: 200, body: { ok: true } }),
    'POST /alerts/a-001/silence': () => ({ status: 200, body: { ok: true } }),
    'POST /alerts/a-002/silence': () => ({ status: 200, body: { ok: true } }),

    // —— K8s ——
    'GET /k8s/clusters': () => ({ status: 200, body: mockClusters }),
    'POST /k8s/clusters': () => ({
      status: 200,
      body: { id: 'c-new', name: 'new-cluster', server: 'https://1.2.3.4:6443', status: 'online', createdAt: '2026-08-11T00:00:00Z' }
    }),
    'DELETE /k8s/clusters/c-staging': () => ({ status: 204, body: null }),
    'POST /k8s/clusters/c-prod/test': () => ({ status: 200, body: { status: 'ok', message: '连接正常' } }),
    'POST /k8s/clusters/c-staging/test': () => ({ status: 200, body: { status: 'fail', message: '连接超时' } }),
    'GET /k8s/clusters/c-prod/namespaces': () => ({ status: 200, body: mockNamespaces }),
    'GET /k8s/clusters/c-prod/pods': () => ({ status: 200, body: mockPods }),
    'GET /k8s/clusters/c-prod/deployments': () => ({ status: 200, body: mockDeployments }),
    'GET /k8s/clusters/c-prod/nodes': () => ({ status: 200, body: mockNodes }),
    'GET /k8s/clusters/c-prod/pods/default/nginx-7b8f-x4k2z/logs': () => ({ status: 200, body: mockPodLogs }),
    'POST /k8s/clusters/c-prod/deployments/default/nginx-deploy/scale': (req) => ({
      status: 200, body: { name: 'nginx-deploy', replicas: 5 }
    }),
    'POST /k8s/clusters/c-prod/deployments/default/nginx-deploy/restart': () => ({
      status: 200, body: { status: 'ok', restartedAt: '2026-08-11T00:00:00Z' }
    })
  }
}

// 解析请求：method + path（去除 /api/v1 前缀 + 去 query）
function parseReq(route) {
  const url = new URL(route.request().url())
  const path = url.pathname.replace(API, '')
  const method = route.request().method()
  return { method, path, fullKey: `${method} ${path}`, url }
}

// 安装 mock 路由
// options:
//   authed: true/false  — /auth/me 是否返回已登录用户
//   overrides: { 'GET /devices': customData | function }  — 覆盖特定端点
//   blockUnhandled: true  — 未匹配的 /api/v1/* 请求返回 404（默认 true）
export async function mockApi(page, options = {}) {
  const { authed = true, overrides = {}, blockUnhandled = true } = options
  const endpoints = { ...defaultEndpoints({ authed }), ...overrides }

  await page.route(`${API}/**`, async (route) => {
    const { fullKey } = parseReq(route)
    let handler = endpoints[fullKey]

    // 未精确匹配时，尝试用通配（如 /devices/:id/provision）
    if (!handler) {
      handler = findWildcard(endpoints, fullKey)
    }

    if (!handler) {
      if (blockUnhandled) {
        return route.fulfill({
          status: 404,
          contentType: 'application/json',
          body: JSON.stringify({ error: `mock not implemented: ${fullKey}` })
        })
      }
      return route.continue()
    }

    let result
    try {
      result = typeof handler === 'function' ? handler(route) : handler
    } catch (e) {
      return route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ error: String(e) }) })
    }

    const status = result.status || 200
    const body = result.body === undefined ? result : result.body
    const headers = {
      'Content-Type': 'application/json',
      'Access-Control-Allow-Origin': '*',
      ...(result.headers || {})
    }
    // 登录/登出/刷新：设置或清除 cookie
    if (fullKey === 'POST /auth/login' || fullKey === 'POST /auth/register') {
      headers['Set-Cookie'] = 'opsmesh_at=mock-at; Path=/; HttpOnly; SameSite=Lax, opsmesh_rt=mock-rt; Path=/; HttpOnly; SameSite=Lax'
    }
    if (fullKey === 'POST /auth/logout') {
      headers['Set-Cookie'] = 'opsmesh_at=; Path=/; Max-Age=0, opsmesh_rt=; Path=/; Max-Age=0'
    }

    return route.fulfill({
      status,
      contentType: 'application/json',
      headers,
      body: body === null ? '' : JSON.stringify(body)
    })
  })
}

// 通配匹配：将端点 key 中的 :id 替换为正则
function findWildcard(endpoints, fullKey) {
  const [method, path] = fullKey.split(' ')
  for (const key of Object.keys(endpoints)) {
    const [m, p] = key.split(' ')
    if (m !== method) continue
    // 将 :id 转为正则
    const pattern = p.replace(/:[^/]+/g, '[^/]+')
    if (new RegExp(`^${pattern}$`).test(path)) {
      return endpoints[key]
    }
  }
  return null
}

// 注入 cookie 让前端 fetchMe 通过（避免每个测试都走登录流程）
export async function setAuthCookies(context) {
  await context.addCookies([
    { name: 'opsmesh_at', value: 'mock-at', domain: '127.0.0.1', path: '/' },
    { name: 'opsmesh_rt', value: 'mock-rt', domain: '127.0.0.1', path: '/' }
  ])
}