// Playwright 真实后端端到端配置（与 playwright.config.js 的 mock 版本互补）。
// 使用在 CI 中由 docker compose 起整栈后对 http://127.0.0.1:8080 的 /enterprise/ 进行真正联调。
// 本配置不启用任何 mock；只跑专门标记为 real 的 spec（testDir=./e2e-real）。
import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './e2e-real',
  fullyParallel: false,
  forbidOnly: true,
  workers: 1,
  reporter: [['list']],
  timeout: 60_000,
  expect: { timeout: 15_000 },
  use: {
    // 真实后端：控制面 8080 + 同路径托管的企业版前端
    baseURL: process.env.E2E_BASE_URL || 'http://127.0.0.1:8080',
    headless: true,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure'
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }]
})
