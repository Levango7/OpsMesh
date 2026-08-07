// vitest.config.js — 前端最小测试配置
// 使用 jsdom 提供完整浏览器环境（document / localStorage / CustomEvent / window），
// fetch 与 alert 在测试用例内通过 vi.stubGlobal 按 case mock。
import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    // jsdom 浏览器环境，覆盖 api.js 对 document/localStorage/CustomEvent 的依赖
    environment: 'jsdom',
    // 仅收集 tests/ 下的 *.test.js
    include: ['tests/**/*.test.js'],
    // 开启 vitest 全局 API（describe/it/expect/...）
    globals: true,
    // 隔离模块状态：每个测试文件独立模块图，避免 memoryToken/authErrorShown 跨文件串扰
    isolate: true,
  },
});
