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
const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:8080'

// 真实栈下 agent 拉任务间隔约 2s + 执行 + 上报，单任务 5s 内通常完成。
// 给到 30s 总等待，覆盖 CI 慢机器 + agent 首次注册延迟。
const TASK_TIMEOUT = 30_000
const POLL_INTERVAL = 1_000

async function login(request) {
  const resp = await request.post(`${BASE}/api/v1/auth/login`, {
    data: { username: ADMIN_USER, password: ADMIN_PASS }
  })
  expect([200, 201]).toContain(resp.status())
  const body = await resp.json()
  const token = body.accessToken || body.access_token || body.token
  expect(token).toBeTruthy()
  return token
}

async function firstAgentID(request, token) {
  const headers = { Authorization: `Bearer ${token}` }
  const resp = await request.get(`${BASE}/api/v1/agents`, { headers })
  expect(resp.ok()).toBeTruthy()
  const body = await resp.json()
  const agents = Array.isArray(body.agents) ? body.agents : (Array.isArray(body) ? body : [])
  expect(agents.length).toBeGreaterThan(0)
  const id = agents[0].agentID || agents[0].agent_id || agents[0].id
  expect(id).toBeTruthy()
  return { id, headers }
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

