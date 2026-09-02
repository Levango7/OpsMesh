// Vitest 全局 setup — 在所有测试前运行
// 解决 jsdom 25.x 在 vitest 2.x 下 localStorage 不可用的问题
// （jsdom 25.x 需要 --localstorage-file 参数才启用持久化 localStorage，
//   vitest 未传入有效路径，导致 localStorage.getItem 不存在）。
// 这里提供一个符合 Storage 接口的内存实现作为兜底。
if (!globalThis.localStorage || typeof globalThis.localStorage.getItem !== 'function') {
  const store = new Map()
  globalThis.localStorage = {
    getItem(key) { return store.has(key) ? store.get(key) : null },
    setItem(key, value) { store.set(key, String(value)) },
    removeItem(key) { store.delete(key) },
    clear() { store.clear() },
    key(i) { return Array.from(store.keys())[i] ?? null },
    get length() { return store.size },
  }
}

// 同步预加载所有 i18n 域翻译，确保单元测试中 t() 能返回完整翻译文本
// 生产环境按需加载（loadDomain / loadRouteDomain），此处仅在测试 setup 中执行
// 使用动态 import 避免 ES 模块静态提升导致 localStorage 未初始化时加载 i18n 模块
const { __injectDomainMessages } = await import('./src/i18n/index.js')
const eagerLocales = import.meta.glob('./src/i18n/locales/*/*.json', { eager: true })
for (const [path, mod] of Object.entries(eagerLocales)) {
  const match = path.match(/.*\/locales\/(zh|en)\/(.+)\.json/)
  if (match) {
    const [, lang, domain] = match
    __injectDomainMessages(lang, domain, mod.default || mod)
  }
}