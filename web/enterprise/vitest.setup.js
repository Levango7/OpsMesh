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