import { describe, it, expect } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'

const src = fs.readFileSync(path.resolve(__dirname, '../../public/sw.js'), 'utf8')

describe('Service Worker 源码契约', () => {
  it('API 一律 NetworkOnly，不再存在 TTL/API_CACHE 或网络失败伪装成功', () => {
    expect(src).toContain("if (isAPIRequest(url))")
    expect(src).toContain('event.respondWith(fetch(request))')
    expect(src).not.toContain('networkFirstWithTTL')
    expect(src).not.toContain('API_CACHE')
    expect(src).not.toContain('API_TTL')
  })

  it('导航 HTML 用 NetworkFirst，缓存写入必须 clone 而非消费原始 body', () => {
    expect(src).toContain("if (request.mode === 'navigate')")
    expect(src).toContain('event.respondWith(networkFirst(request, DYNAMIC_CACHE))')
    expect(src).toContain('await cache.put(request, response.clone())')
    expect(src).not.toContain('new Response(response.body')
  })

  it('缓存版本已升级以清理旧的身份/旧 HTML 缓存', () => {
    expect(src).toContain("const CACHE_VERSION = 'v2'")
    expect(src).not.toContain("const CACHE_VERSION = 'v1'")
  })
})
