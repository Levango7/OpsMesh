// 真实后端 E2E 长链路：agent 注册 → 下发 shell 任务 → agent 执行 → 回执校验。
// 运行前提：CI e2e-real job `docker compose up -d`（controlplane+mysql+redis+agent）。
// compose 中 agent 会自动注册→心跳→拉任务→上报结果，本 spec 验证完整闭环。
// 字段约定（camelCase）：taskID / agentID / exitCode / stdout / status。
//
// 注意：控制面无单任务查询端点（GET /api/v1/tasks/{id} 不存在，路由仅支持
// {id}/cancel|result|approve|reject），状态轮询一律走列表 API（GET /api/v1/tasks），
// 返回数组（不传 page 时）逐项匹配 taskID。
import { test, expect } from '@playwright/test'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'admin'
const ADMIN_PASS = process.env.E2E_ADMIN_PASS || 'admin123'
const E2E_NEW_PASS = process.env.E2E_NEW_PASS || 'E2eRealPass2026'
const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:8080'

// 文件级 token 缓存：每个 spec 只真实登录一次（多用例共享），
// 避免连续登录打满 loginGuard 令牌桶（每 IP 10 突发 + 3s 补 1）触发 429。
let cachedToken = null

// 真实栈下 agent 拉任务间隔约 2s + 执行 + 上报，单任务 5s 内通常完成。
// 给到 30s 总等待，覆盖 CI 慢机器 + agent 首次注册延迟。
const TASK_TIMEOUT = 30_000
const POLL_INTERVAL = 1_000

// login 返回正式 access token（兼容首登强制改密，逻辑与 core.spec.js 同款）。
// 用例间密码状态：第一个改密用例会把 admin 密码永久改为 E2E_NEW_PASS，
// 后续用例用 admin123 登录会 401。helper 先试 E2E_NEW_PASS（已改密场景），
// 401 再回退 admin123 + 首登改密流程（新库场景）。
// 429 = loginGuard 限流（每 IP 10 突发 + 3s 补 1），等令牌桶补充后重试。
async function login(request) {
  if (cachedToken) return cachedToken
  async function tryLogin(pw) {
    let resp = await request.post(`${BASE}/api/v1/auth/login`, {
      data: { username: ADMIN_USER, password: pw }
    })
    // 429 = loginGuard 限流（每 IP 10 突发 + 3s 补 1）。多用例连续登录会打满
    // 令牌桶，最多重试 3 次、每次等 5s（桶补充约需 3s/令牌）。
    for (let attempt = 0; attempt < 3 && resp.status() === 429; attempt++) {
      await new Promise(r => setTimeout(r, 5000))
      resp = await request.post(`${BASE}/api/v1/auth/login`, {
        data: { username: ADMIN_USER, password: pw }
      })
    }
    return resp
  }

  let resp = await tryLogin(E2E_NEW_PASS)
  let body = null

  if (resp.status() === 200) {
    body = await resp.json()
  } else if (resp.status() === 401) {
    // 未改密（新库）：走 admin123 首登强制改密。
    resp = await tryLogin(ADMIN_PASS)
    expect([200, 201]).toContain(resp.status())
    body = await resp.json()
    if (body.mustChangePassword || body.changePasswordToken) {
      const cpt = body.changePasswordToken
      expect(cpt).toBeTruthy()
      const change = await request.post(`${BASE}/api/v1/auth/change-password`, {
        data: {
          oldPassword: ADMIN_PASS,
          newPassword: E2E_NEW_PASS,
          changePasswordToken: cpt
        }
      })
      expect([200, 201]).toContain(change.status())
      resp = await tryLogin(E2E_NEW_PASS)
      expect([200, 201]).toContain(resp.status())
      body = await resp.json()
    }
  } else {
    expect([200, 201]).toContain(resp.status())
  }

  const token = body.token
  expect(token, '登录应返回 access token').toBeTruthy()
  cachedToken = token
  return token
}

// firstAgentID 等待 agent 注册完成（compose 起栈后 agent 需数秒注册+心跳，
// 立即查询会拿到空列表）。最长等 30s（每 2s 轮询），覆盖 CI 慢机器。
async function firstAgentID(request, token) {
  const headers = { Authorization: `Bearer ${token}` }
  const deadline = Date.now() + 30_000
  while (Date.now() < deadline) {
    const resp = await request.get(`${BASE}/api/v1/agents`, { headers }).catch(() => null)
    if (resp && resp.ok()) {
      const body = await resp.json()
      const agents = Array.isArray(body.agents) ? body.agents : (Array.isArray(body) ? body : [])
      if (agents.length > 0) {
        const id = agents[0].agentID || agents[0].agent_id || agents[0].id
        expect(id).toBeTruthy()
        return { id, headers }
      }
    }
    await new Promise(r => setTimeout(r, 2000))
  }
  throw new Error('30s 内未等到 agent 注册（检查 compose 中 agent 容器日志）')
}

// pollTaskStatus 轮询列表 API 直到任务进入终态，返回 { status } 或 null（超时）。
async function pollTaskStatus(request, headers, taskID, timeoutMs = TASK_TIMEOUT) {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    const r = await request.get(`${BASE}/api/v1/tasks`, { headers }).catch(() => null)
    if (r && r.ok()) {
      const arr = await r.json()
      const tasks = Array.isArray(arr) ? arr : (arr.tasks || [])
      const hit = tasks.find(t => (t.taskID || t.task_id || t.id) === taskID)
      if (hit && ['done', 'failed', 'cancelled'].includes(hit.status)) {
        return { status: hit.status }
      }
    }
    await new Promise(res => setTimeout(res, POLL_INTERVAL))
  }
  return null
}

test.describe('真实后端长链路（agent 执行回执闭环）', () => {
  test('agent 在线 → 下发 shell → 轮询至 done → result 含 stdout', async ({ request }) => {
    const token = await login(request)
    const { id: agentID, headers } = await firstAgentID(request, token)

    // 下发一条确定性 shell 任务：echo 固定字符串，exitCode 必为 0。
    const MARKER = 'opsmesh-e2e-real-marker-' + Date.now()
    const create = await request.post(`${BASE}/api/v1/tasks`, {
      headers,
      data: {
        agentID,
        type: 'shell',
        command: `echo ${MARKER}`
      }
    })
    expect([200, 201]).toContain(create.status())
    const created = await create.json()
    const taskID = created.taskID || created.task_id || created.id
    expect(taskID).toBeTruthy()

    // 轮询任务状态（列表 API），直到终态或超时。
    const final = await pollTaskStatus(request, headers, taskID)
    expect(final, '30s 内任务应到达终态（agent 可能未注册成功，见 compose agent 日志）').toBeTruthy()
    expect(final.status).toBe('done')

    // 校验执行回执：exitCode=0、stdout 含 marker。
    const result = await request.get(`${BASE}/api/v1/tasks/${taskID}/result`, { headers })
    expect(result.ok()).toBeTruthy()
    const rc = await result.json()
    expect(rc.exitCode).toBe(0)
    expect(rc.stdout).toContain(MARKER)
    expect(rc.agentID).toBe(agentID)
    expect(rc.durationMs).toBeGreaterThanOrEqual(0)
  })

  test('失败任务回执：exit non-zero → status=failed → stderr 可读', async ({ request }) => {
    const token = await login(request)
    const { id: agentID, headers } = await firstAgentID(request, token)

    // 下发一条必然失败的任务（exit 7），maxRetries=0 使一次失败即终态。
    const create = await request.post(`${BASE}/api/v1/tasks`, {
      headers,
      data: {
        agentID,
        type: 'shell',
        command: 'echo "e2e-fail-stderr" >&2 && exit 7',
        maxRetries: 0
      }
    })
    expect([200, 201]).toContain(create.status())
    const created = await create.json()
    const taskID = created.taskID || created.task_id || created.id
    expect(taskID).toBeTruthy()

    const final = await pollTaskStatus(request, headers, taskID)
    expect(final, '30s 内任务应到达终态').toBeTruthy()
    expect(final.status).toBe('failed')

    const result = await request.get(`${BASE}/api/v1/tasks/${taskID}/result`, { headers })
    expect(result.ok()).toBeTruthy()
    const rc = await result.json()
    // agent 上报的 exitCode 非零（7）；stderr 含我们写入的标记。
    expect(rc.exitCode).not.toBe(0)
    expect(rc.stderr).toContain('e2e-fail-stderr')
  })
})

