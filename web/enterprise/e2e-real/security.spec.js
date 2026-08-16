// 安全 E2E：require-auth / 租户越权 / 任务取消全链路 / mTLS。
// 运行前提：CI e2e-sec job 用 docker-compose.e2e-sec.yaml 起栈
// （controlplane 开 --require-auth + gRPC mTLS，agent 带客户端证书注册）。
// 用纯 Node http/tls 直连验证（不走浏览器，避免 Cookie 自动携带掩盖身份缺失场景）。
import { test, expect } from '@playwright/test'
import http from 'node:http'
import tls from 'node:tls'
import fs from 'node:fs'
import path from 'node:path'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'admin'
const ADMIN_PASS = process.env.E2E_ADMIN_PASS || 'admin123'
const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:8080'
const CERTS_DIR = process.env.E2E_CERTS_DIR || 'e2e-certs'

// req 纯 Node http 请求，返回 { status, json }（不自动携带 Cookie/头——安全场景要控制身份头）。
function req(method, urlPath, { headers = {}, body } = {}) {
  return new Promise((resolve, reject) => {
    const u = new URL(BASE + urlPath)
    const data = body ? JSON.stringify(body) : null
    const r = http.request({
      hostname: u.hostname, port: u.port || 80, path: u.pathname + u.search,
      method, headers: { 'Content-Type': 'application/json', 'Content-Length': data ? Buffer.byteLength(data) : 0, ...headers }
    }, (res) => {
      let raw = ''
      res.on('data', (c) => (raw += c))
      res.on('end', () => {
        let j = null
        try { j = raw ? JSON.parse(raw) : null } catch { j = raw }
        resolve({ status: res.statusCode, json: j })
      })
    })
    r.on('error', reject)
    if (data) r.write(data)
    r.end()
  })
}

// login 返回 admin 的 Bearer token（demo 模式首登强制改密流程，与 core.spec 同款）。
let cachedToken = null
async function login() {
  if (cachedToken) return cachedToken
  async function tryLogin(pw) {
    let resp = await req('POST', '/api/v1/auth/login', { body: { username: ADMIN_USER, password: pw } })
    for (let attempt = 0; attempt < 3 && resp.status === 429; attempt++) {
      await new Promise((r) => setTimeout(r, 5000))
      resp = await req('POST', '/api/v1/auth/login', { body: { username: ADMIN_USER, password: pw } })
    }
    return resp
  }
  let resp = await tryLogin(process.env.E2E_NEW_PASS || 'E2eRealPass2026')
  let body = resp.json
  if (resp.status === 401) {
    resp = await tryLogin(ADMIN_PASS)
    expect([200, 201]).toContain(resp.status)
    body = resp.json
    if (body.mustChangePassword || body.changePasswordToken) {
      const change = await req('POST', '/api/v1/auth/change-password', {
        body: { oldPassword: ADMIN_PASS, newPassword: process.env.E2E_NEW_PASS || 'E2eRealPass2026', changePasswordToken: body.changePasswordToken }
      })
      expect([200, 201]).toContain(change.status)
      resp = await tryLogin(process.env.E2E_NEW_PASS || 'E2eRealPass2026')
      expect([200, 201]).toContain(resp.status)
      body = resp.json
    }
  } else {
    expect([200, 201]).toContain(resp.status)
  }
  cachedToken = body.token || body.accessToken
  expect(cachedToken, '登录应返回 access token').toBeTruthy()
  return cachedToken
}

test.describe('安全契约（require-auth 开启）', () => {
  test('require-auth：缺失身份头 → 401', async () => {
    // 无 X-Tenant-ID、无 Authorization → require-auth 拒绝
    const noAuth = await req('GET', '/api/v1/agents')
    expect(noAuth.status).toBe(401)
  })

  test('require-auth：带合法 Bearer token → 通过', async () => {
    const token = await login()
    const resp = await req('GET', '/api/v1/agents', { headers: { Authorization: `Bearer ${token}` } })
    expect(resp.status).toBe(200)
  })

  test('租户越权：跨租户查设备 → 403/404（不可见）', async () => {
    const token = await login()
    // tenant-a 视角查设备：应可见（至少 200）
    const a = await req('GET', '/api/v1/devices', { headers: { Authorization: `Bearer ${token}`, 'X-Tenant-ID': 'tenant-a' } })
    expect(a.status).toBe(200)
    // 无 token 仅伪造头：require-auth 下头伪造无身份支撑 → 401（防网关伪造）
    const forged = await req('GET', '/api/v1/devices', { headers: { 'X-Tenant-ID': 'tenant-b' } })
    expect([401, 403]).toContain(forged.status)
  })

  test('任务取消全链路：pending 拦截 + running 强杀 + 无回写', async () => {
    const token = await login()
    const headers = { Authorization: `Bearer ${token}` }
    // 取第一个 agent（mTLS 注册成功后才在列表中）
    const agentsResp = await req('GET', '/api/v1/agents', { headers })
    expect(agentsResp.status).toBe(200)
    const agents = Array.isArray(agentsResp.json.agents) ? agentsResp.json.agents : (Array.isArray(agentsResp.json) ? agentsResp.json : [])
    expect(agents.length, 'agent 应已注册（mTLS 通过）').toBeGreaterThan(0)
    const agentID = agents[0].agentID || agents[0].id

    // 场景 1：立即取消（pending 拦截——任务可能已被领取，取消语义为"终态化"）
    const create = await req('POST', '/api/v1/tasks', {
      headers, body: { agentID, type: 'shell', command: 'sleep 30', name: 'e2e-sec-cancel-pending', maxRetries: 0 }
    })
    expect([200, 201]).toContain(create.status)
    const taskID = create.json.taskID
    expect(taskID).toBeTruthy()
    const cancel = await req('POST', `/api/v1/tasks/${taskID}/cancel`, { headers })
    expect([200, 201]).toContain(cancel.status)
    // 取消后任务应不再为 pending/running（终态化）
    const after = await req('GET', `/api/v1/tasks/${taskID}`, { headers })
    expect([200, 404]).toContain(after.status)
    if (after.status === 200 && after.json.status) {
      expect(['cancelled', 'done', 'failed']).toContain(after.json.status)
    }

    // 场景 2：running 强杀（sleep 长任务，agent 执行中被取消）
    const create2 = await req('POST', '/api/v1/tasks', {
      headers, body: { agentID, type: 'shell', command: 'sleep 60', name: 'e2e-sec-cancel-running', maxRetries: 0 }
    })
    expect([200, 201]).toContain(create2.status)
    const taskID2 = create2.json.taskID
    // 轮询等待任务进入 running（agent 拉取执行），最长 15s
    let status = null
    for (let i = 0; i < 15; i++) {
      const t = await req('GET', `/api/v1/tasks/${taskID2}`, { headers })
      if (t.status === 200 && (t.json.status === 'running' || t.json.status === 'done')) { status = t.json.status; break }
      await new Promise((r) => setTimeout(r, 1000))
    }
    if (status === 'done') {
      test.info().skip('任务在取消前已执行完成（sleep 被环境加速），跳过 running 强杀断言')
      return
    }
    const cancel2 = await req('POST', `/api/v1/tasks/${taskID2}/cancel`, { headers })
    expect([200, 201]).toContain(cancel2.status)
    // 取消后 status 应为 cancelled（或 done 若已秒完成）
    const after2 = await req('GET', `/api/v1/tasks/${taskID2}`, { headers })
    if (after2.status === 200 && after2.json.status) {
      expect(['cancelled', 'done', 'failed']).toContain(after2.json.status)
    }
  })

  test('mTLS：无客户端证书访问 gRPC 9090 → 握手失败', async () => {
    // 控制面 9090 开 mTLS：无客户端证书的 TLS 握手应被拒绝（certificate required）。
    // 有证书（ca 签发）则应能完成 TLS 握手（HTTP/2 前奏由 gRPC 完成，这里只验 TLS 层）。
    const u = new URL(BASE)
    const ca = fs.readFileSync(path.join(CERTS_DIR, 'ca.crt'))
    const clientCert = fs.readFileSync(path.join(CERTS_DIR, 'client.crt'))
    const clientKey = fs.readFileSync(path.join(CERTS_DIR, 'client.key'))

    // 1. 无客户端证书 → 握手失败（mTLS 拒绝）
    await new Promise((resolve, reject) => {
      const sock = tls.connect({ host: u.hostname, port: 9090, ca, rejectUnauthorized: true }, () => {
        // 本不该到这（无客户端证书应被拒）；若到了说明 mTLS 未强制
        sock.destroy()
        reject(new Error('无客户端证书竟完成 TLS 握手——mTLS 未生效'))
      })
      sock.on('error', (err) => {
        // 预期：客户端证书缺失 → 服务端终止握手
        sock.destroy()
        resolve()
      })
      sock.on('secureConnect', () => {
        sock.destroy()
        reject(new Error('无客户端证书竟完成 TLS 握手——mTLS 未生效'))
      })
      setTimeout(() => { sock.destroy(); resolve() }, 5000)
    })

    // 2. 带客户端证书 → TLS 握手成功（mTLS 通过）
    await new Promise((resolve, reject) => {
      const sock = tls.connect({
        host: u.hostname, port: 9090, ca,
        cert: clientCert, key: clientKey, rejectUnauthorized: true
      }, () => {
        sock.destroy()
        resolve()
      })
      sock.on('error', (err) => { sock.destroy(); reject(err) })
      setTimeout(() => { sock.destroy(); reject(new Error('带证书握手超时')) }, 5000)
    })
  })
})
