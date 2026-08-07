import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

// Vitest 配置 — 企业版前端单元测试
// 复用 @vitejs/plugin-vue 解析 .vue SFC；jsdom 提供 DOM 环境；
// alias '@' → src，与 vite.config.js 保持一致，便于测试中 import '@/...'。
export default defineConfig({
  plugins: [vue()],
  test: {
    environment: 'jsdom',
    globals: true,
    // 不 stub 全局变量（localStorage 等），使用 jsdom 提供的真实实现
    unstubGlobals: true,
    // setup 文件：兜底 localStorage（jsdom 25.x 在 vitest 下可能缺失）
    setupFiles: ['./vitest.setup.js'],
    alias: {
      '@': resolve(__dirname, 'src'),
    },
  },
})