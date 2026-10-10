// e2e/fixtures/selfcheck.js — 端口串台自检
//
// 背景（2026-10-10 实测踩坑）：playwright.config.js 的 webServer 用 --strictPort
// 绑 4173，但**若该端口已被别的项目占用**（本机曾撞上 `node server.js
// F:/Nexus/levee/web/dist 4173`），Playwright 会静默复用现有服务，于是整套
// e2e 都在测别人家的 UI：报错形态是「getByTestId('alerts-title') not found」，
// 极易误判成 OpsMesh 前端回归。本模块在跑用例前先确认对面真的是本应用。
//
// 判据（两条同时满足，缺一即判红）：
//   1. HTML <title> 恰为 OpsMesh 企业版（LEVEE 那套是「LEVEE 变更治理控制台」）；
//   2. 页面存在 <div id="app"> 挂载点（Vite SPA 入口）。
import { APP_BASE } from './helpers.js'

export const OWN_TITLE = 'OpsMesh 企业版'

// assertOwnServer 校验 baseURL 对面是本应用的 dev/preview 服务。
// 失败时抛出带可操作信息的错误（提示端口被谁占用、如何规避）。
export async function assertOwnServer(request) {
  let resp
  try {
    resp = await request.get(APP_BASE, { timeout: 10_000 })
  } catch (e) {
    throw new Error(
      `[selfcheck] 无法访问 ${APP_BASE}：${String(e)}\n` +
      `  webServer 未就绪？若 4173 已被其他项目占用，Playwright 会静默复用它——\n` +
      `  请先释放 4173（或改 playwright.config.js 的 webServer.port）再跑 e2e。`
    )
  }
  const html = await resp.text()
  if (!html.includes(OWN_TITLE)) {
    throw new Error(
      `[selfcheck] ${APP_BASE} 返回的页面不是 OpsMesh 企业版（未找到 <title>${OWN_TITLE}</title>）。\n` +
      `  baseURL 上跑的很可能是别的项目——典型原因是 4173 被其他服务占用，\n` +
      `  Playwright 复用了它。请释放该端口后重跑，否则整套 e2e 都在误测他人 UI。\n` +
      `  实际返回片段：${html.slice(0, 200).replace(/\s+/g, ' ')}`
    )
  }
  if (!/id="app"/.test(html)) {
    throw new Error(
      `[selfcheck] ${APP_BASE} 页面缺少 <div id="app"> 挂载点，不像企业版前端产物。\n` +
      `  可能是 dist/ 陈旧或构建产物不完整——重跑 npm run build 后重试。\n` +
      `  实际返回片段：${html.slice(0, 200).replace(/\s+/g, ' ')}`
    )
  }
}
