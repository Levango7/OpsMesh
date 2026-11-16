// OpsMesh 负载基线（k6）
//
// 用法：
//   k6 run scripts/loadtest/k6-baseline.js
//   k6 run -e SCENARIO=load -e BASE_URL=http://127.0.0.1:8080 -e TOKEN=... -e TENANT=t1 scripts/loadtest/k6-baseline.js
//
// 判据（与 docs/canary-release-and-rollback-plan.md §3.2 对齐）：
//   http_req_failed rate < 0.001
//   http_req_duration p(99) < 300ms
//
// 说明：CI 不跑 k6（需要真实栈），这是发布前的手工门禁；基线记录模板见同目录 README.md。

import http from 'k6/http'
import { check, sleep } from 'k6'
import { Rate } from 'k6/metrics'

const BASE_URL = (__ENV.BASE_URL || 'http://127.0.0.1:8080').replace(/\/+$/, '')
const TOKEN = __ENV.TOKEN || ''
const TENANT = __ENV.TENANT || 'default'
const SCENARIO = (__ENV.SCENARIO || 'smoke').toLowerCase()

const authDenied = new Rate('auth_denied_rate')

const SCENARIOS = {
  smoke: {
    executor: 'constant-vus',
    vus: 2,
    duration: '30s',
    tags: { scenario: 'smoke' }
  },
  load: {
    executor: 'ramping-vus',
    startVUs: 1,
    stages: [
      { duration: '30s', target: 50 },
      { duration: '5m', target: 50 }
    ],
    gracefulRampDown: '30s',
    tags: { scenario: 'load' }
  },
  stress: {
    executor: 'ramping-vus',
    startVUs: 5,
    stages: [
      { duration: '60s', target: 200 },
      { duration: '5m', target: 200 }
    ],
    gracefulRampDown: '30s',
    tags: { scenario: 'stress' }
  }
}

export const options = {
  scenarios: { [SCENARIO]: SCENARIOS[SCENARIO] || SCENARIOS.smoke },
  thresholds: {
    http_req_failed: ['rate<0.001'],
    http_req_duration: ['p(99)<300']
  },
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max']
}

function publicHeaders() {
  return { headers: { Accept: 'application/json' } }
}

function protectedHeaders() {
  const h = { Accept: 'application/json', 'X-Tenant-ID': TENANT }
  if (TOKEN) h.Authorization = `Bearer ${TOKEN}`
  return { headers: h }
}

function get(path, name, authed) {
  const res = http.get(`${BASE_URL}${path}`, authed ? protectedHeaders() : publicHeaders(), {
    tags: { endpoint: name }
  })
  if (authed) {
    authDenied.add(res.status === 401 || res.status === 403, { endpoint: name })
    check(res, {
      [`${name}: 2xx/401/403`]: (r) => (r.status >= 200 && r.status < 300) || r.status === 401 || r.status === 403
    })
  } else {
    check(res, { [`${name}: 200`]: (r) => r.status === 200 })
  }
  return res
}

export default function () {
  // 探活面（无鉴权）
  get('/healthz', 'healthz', false)
  get('/readyz', 'readyz', false)

  // 业务读面（需 token；无 token 时跳过，避免把 401 计入失败率）
  if (TOKEN) {
    get('/api/v1/devices?page=1&pageSize=20', 'devices', true)
    get('/api/v1/tasks?page=1&pageSize=20', 'tasks', true)
    get('/api/v1/tenants?page=1&pageSize=20', 'tenants', true)
  }

  sleep(1)
}
