// 真实后端 E2E：控制面 /healthz + /readyz 探活。
// 该 spec 不做任何 page.route 拦截，直接命中 docker compose 起出来的 opsmesh 控制面。
// 本目录已与 vitest 隔离（见 vitest.config.js exclude）。
import { test, expect } from '@playwright/test'

const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:8080'

test.describe('真实后端探活', () => {
  test('控制面 /healthz 可达', async ({ request }) => {
    const resp = await request.get(BASE + '/healthz')
    expect(resp.ok()).toBeTruthy()
  })

  test('控制面 /readyz 就绪（mysql+redis ready 时 200）', async ({ request }) => {
    const resp = await request.get(BASE + '/readyz')
    expect(resp.ok()).toBeTruthy()
  })
})
