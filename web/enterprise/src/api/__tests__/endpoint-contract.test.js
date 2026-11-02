// 前端端点 × 动词 vs 后端路由表 一致性守卫（2026-11-02）
//
// 背景：前端 api 层按 helper 语义发请求（getJSON/postJSON/putJSON/deleteJSON…），而
// **HTTP 方法或路径与后端注册不一致时不会在编译期暴露**——线上表现为 405/404，且
// 多数页面只弹一个通用错误，用户看到的就是"点了没反应"。2026-11-02 实测 450 个前端
// 端点里抓到 6 处：清 Agent 记忆发 POST（后端只注册 DELETE）、改/删提示词发 POST
// （后端 PUT/DELETE）、删知识文档与改同步配置发 POST（后端 DELETE/PUT）、自动纳管
// 发 /device-svc/provision/auto（后端是本地 /api/v1/provision/auto）——六个功能
// 此前全部不可用。
//
// 判定口径（与 Go 1.22 ServeMux 语义对齐）：
//   - 后端 `mux.HandleFunc("GET /api/v1/x")` → 精确 + 方法匹配
//   - `{param}` 段与前端模板字面量 `${...}` 段互通（都视为任意单段）
//   - 以 `/` 结尾的模式是**子树前缀**匹配
//   - svcproxy 的 `PublicPrefix`（Rules / DeviceExtras / TaskExtras）是**运行期按表
//     动态注册**的，静态扫不到，必须显式并入 —— 否则会误报 24 处微服务端点。
//
// 棘轮：新增端点若后端没注册（或动词不对）即红。若某路由确实是动态注册，把它加进
// DYNAMIC_PREFIXES / EXEMPT 并写明机制。
import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative, sep } from 'node:path'

// __tests__ → api → src → enterprise → web → 仓库根（5 层）
const REPO_ROOT = join(__dirname, '../../../../..')
const API_DIR = join(__dirname, '..')

/** request.js 的 helper → HTTP 动词 */
const VERBS = {
  getJSON: 'GET',
  postJSON: 'POST',
  putJSON: 'PUT',
  deleteJSON: 'DELETE',
  deleteJSONBody: 'DELETE',
  postEmpty: 'POST'
}

/** 已知的、非本文档可见的动态注册前缀（须写明机制；目前 svcproxy 已自动解析，无需手填） */
const DYNAMIC_PREFIXES = []

function walk(dir, filter) {
  const out = []
  for (const n of readdirSync(dir)) {
    const p = join(dir, n)
    if (statSync(p).isDirectory()) out.push(...walk(p, filter))
    else if (filter(p)) out.push(p)
  }
  return out
}

/** 前端端点：helper 调用的字面量/模板字面量 */
function frontendEndpoints() {
  const out = []
  for (const file of walk(API_DIR, (p) => p.endsWith('.js') && !p.includes('__tests__') && !p.includes('__mocks__'))) {
    const src = readFileSync(file, 'utf-8')
    let base = '/api/v1'
    if (/baseURL:\s*''/.test(src)) base = ''
    const m = src.match(/baseURL:\s*'([^']+)'/)
    if (m) base = m[1]
    const re = new RegExp(`\\b(${Object.keys(VERBS).join('|')})\\(\\s*['"\`]([^'"\`]+)['"\`]`, 'g')
    for (const mm of src.matchAll(re)) {
      // query string 不参与路由匹配（`?sessionId=` 之类），剥掉再比
      const raw = mm[2].split('?')[0]
      out.push({ verb: VERBS[mm[1]], path: base + raw, file: relative(REPO_ROOT, file).split(sep).join('/') })
    }
  }
  return out
}

/** 后端注册：静态 HandleFunc/.Handle（仅 /api/ 命名空间）+ svcproxy 动态前缀 */
function backendRoutes() {
  const routes = []
  const prefixes = [...DYNAMIC_PREFIXES]
  const goFiles = walk(join(REPO_ROOT, 'internal'), (p) => p.endsWith('.go') && !p.endsWith('_test.go'))
  for (const file of goFiles) {
    const src = readFileSync(file, 'utf-8')
    for (const mm of src.matchAll(/(?:HandleFunc|\.Handle)\(\s*"([A-Z]+)?\s*(\/[^"]*)"/g)) {
      if (!mm[2].startsWith('/api/')) continue // 只比 API 命名空间（SPA/静态资源 catch-all 不参与）
      routes.push({ verb: mm[1] || 'ANY', path: mm[2] })
    }
    // svcproxy 表：PublicPrefix 是运行期循环注册的前缀
    for (const mm of src.matchAll(/PublicPrefix:\s*"([^"]+)"/g)) prefixes.push(mm[1])
  }
  void goFiles
  return { routes, prefixes }
}

function segMatch(pattern, path) {
  if (pattern.endsWith('/')) return path.startsWith(pattern)
  const ps = pattern.split('/').filter(Boolean)
  const qs = path.split('/').filter(Boolean)
  if (ps.length !== qs.length) return false
  return ps.every((a, i) => a.startsWith('{') || qs[i].startsWith('${') || a === qs[i])
}

describe('前端端点 × 动词被后端覆盖', () => {
  const { routes, prefixes } = backendRoutes()
  const endpoints = frontendEndpoints()

  it('解析出的规模合理（防解析失效后空集通过）', () => {
    expect(endpoints.length).toBeGreaterThan(300)
    expect(routes.length).toBeGreaterThan(400)
    expect(prefixes.length).toBeGreaterThan(5)
  })

  it('匹配器语义正确（正反例自证）', () => {
    const covered = (verb, path) => {
      if (prefixes.some((x) => path === x || path.startsWith(x + '/'))) return true
      return routes.some((r) => (r.verb === 'ANY' || r.verb === verb) && segMatch(r.path, path))
    }
    expect(covered('GET', '/api/v1/aiops/status')).toBe(true)
    expect(covered('POST', '/api/v1/aiops/analyze')).toBe(true)
    expect(covered('GET', '/api/v1/gpu/models')).toBe(true) // svcproxy 前缀
    expect(covered('GET', '/api/v1/definitely-not-a-route')).toBe(false)
  })

  it('每个前端端点都有匹配的后端注册（动词一致）', () => {
    const covered = (verb, path) => {
      if (prefixes.some((x) => path === x || path.startsWith(x + '/'))) return true
      return routes.some((r) => (r.verb === 'ANY' || r.verb === verb) && segMatch(r.path, path))
    }
    const broken = endpoints
      .filter((e) => !covered(e.verb, e.path))
      .map((e) => `${e.verb} ${e.path}  (${e.file})`)
    expect(
      broken,
      '后端没有匹配的注册 ⇒ 线上 404/405。请改对 helper 动词或路径；若确为动态注册，' +
        '把前缀加进本测试的 DYNAMIC_PREFIXES 并写明机制。'
    ).toEqual([])
  })
})
