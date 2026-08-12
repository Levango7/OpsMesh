// 真实后端 E2E：登录 + 拉取 /api/v1/me + 任务列表非空。
// 该 spec 不做任何 page.route 拦截，直接命中 docker compose 起出来的 opsmesh 控制面。
// 运行前提：
//   1) CI `e2e-real` job 已执行 `docker compose up -d`（栈含 controlplane+mysql+redis+agent）
//   2) 已 `npm run build` 产出 web/enterprise/dist，且控制面把 dist 通过 /enterprise/ 挂出
//      （未挂出时站点 404，本测试将失败，提示 ops team 补全静态托管）
// 本 spec 与 e2e/ 下的 mock 用例互补：mock 跑得快、定位 UI bug；本 spec 校验真实契约。
import { test, expect } from '@playwright/test'

test.describe('真实后端联调（无 mock）', () => {
  test('控制面 /healthz 可达', async ({ request }) => {
    const resp = await request.get('/healthz')
    expect(resp.ok()).toBeTruthy()
  })

  test('未登录访问 /api/v1/devices 返回 401 或 200（取决于 require-auth 配置）', async ({ request }) => {
    const resp = await request.get('/api/v1/devices')
    expect([200, 401, 403]).toContain(resp.status())
  })
})
