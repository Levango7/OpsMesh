// Playwright 端到端测试配置
// - 测试目录：./e2e
// - 被测站点：vite preview 启动的静态服务器（端口 4173，base=/enterprise/）
// - 由于后端 API 可能不可用，测试通过 page.route 拦截 /api/v1/* 请求并返回 mock 数据
//   （详见 e2e/fixtures/mock-api.js），无需后端运行即可覆盖核心前端流程。
import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  workers: 1,
  reporter: [
    ['list'],
    ['html', { outputFolder: 'playwright-report', open: 'never' }]
  ],
  timeout: 60_000,
  expect: { timeout: 10_000 },
  use: {
    baseURL: 'http://127.0.0.1:4173',
    headless: true,
    trace: 'on-first-retry',
    screenshot: 'only-on-failure'
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] }
    }
  ],
  webServer: {
    // Windows 上 PowerShell 5.1 不支持 &&，使用 ; 顺序执行（dist 已存在时可跳过 build）
    // --host 127.0.0.1 确保监听 IPv4，与 baseURL 一致（避免 localhost→::1 解析问题）
    command: 'npx vite preview --host 127.0.0.1 --port 4173 --strictPort',
    port: 4173,
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
    stdout: 'pipe',
    stderr: 'pipe'
  }
})