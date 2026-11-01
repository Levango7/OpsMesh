// 路由门词汇 vs 后端 RBAC 目录一致性守卫（2026-11-02）
//
// 背景：router meta 的 requirePerm 是**前端发明的门**，其可满足性完全取决于后端
// internal/store/model/perm.go 的 PermSpecs 目录——admin 的权限集由该目录派生。
// 目录里没有某个权限点 ⇒ 该路由对**含 admin 在内的全部角色**都进不去（死路由），
// 且前端毫无报错，只表现为"点了没反应/被静默重定向"。
//
// 真实案例（2026-11-02 修复）：/trace 与 /gitops/dashboard 挂 observability:read，
// 目录无此权限 ⇒ 两页长期死路由；同类历史事故还有 cloud:read（2026-10-22）、
// flux:read（2026-11-02，由后端源码守护扫出）。后端侧已有
// sql_rbac_catalog_test.go 的 handlerRequiredPerms 守护「handler 用到的权限点在目录里」，
// 本测试补上另一半：「前端 requirePerm 在目录里」。两面合起来才闭环。
//
// 棘轮语义：新增路由若挂目录外的权限点即红——要么改挂真实权限点，要么先在后端
// PermSpecs 登记（并考虑 operatorReadOnlyGroups 的层级关系）。
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

// __tests__ → router → src → enterprise → web → 仓库根（5 层）
const REPO_ROOT = join(__dirname, '../../../../..')
const ROUTER = join(__dirname, '../index.js')
const PERM_CATALOG = join(REPO_ROOT, 'internal/store/model/perm.go')

/** 从 router/index.js 抽全部 requirePerm 字面量 */
function routerPerms() {
  const src = readFileSync(ROUTER, 'utf-8')
  return [...new Set([...src.matchAll(/requirePerm:\s*'([^']+)'/g)].map((m) => m[1]))].sort()
}

/** 从后端权限目录抽全部权限点（PermSpec 三元组 {"group", "group:action", "desc"}） */
function catalogPerms() {
  const src = readFileSync(PERM_CATALOG, 'utf-8')
  return new Set([...src.matchAll(/\{"[a-z]+",\s*"([a-z]+:[a-z]+)"/g)].map((m) => m[1]))
}

/** 路由 path → requirePerm，便于失败时报出违规页面而不是只报权限名 */
function routePermPairs() {
  const src = readFileSync(ROUTER, 'utf-8')
  return [...src.matchAll(/path:\s*'([^']+)'[\s\S]{0,400}?requirePerm:\s*'([^']+)'/g)].map((m) => ({
    path: m[1],
    perm: m[2]
  }))
}

describe('路由门词汇与后端 RBAC 目录一致', () => {
  it('权限目录可解析（防解析失效后测试静默通过）', () => {
    const cats = catalogPerms()
    expect(cats.size).toBeGreaterThan(80)
    // 抽查两个断言必须存在，否则说明 perm.go 格式变了
    expect(cats.has('device:read')).toBe(true)
    expect(cats.has('task:write')).toBe(true)
  })

  it('router 至少解析出 30 个 requirePerm（防路由改写导致空集通过）', () => {
    expect(routerPerms().length).toBeGreaterThan(30)
  })

  it('每个 requirePerm 都存在于后端 PermSpecs（否则该路由对含 admin 的全部角色都是死路由）', () => {
    const cats = catalogPerms()
    const missing = routePermPairs().filter((r) => !cats.has(r.perm))
    expect(
      missing.map((r) => `${r.path} -> ${r.perm}`),
      '权限点不在 internal/store/model/perm.go 的 PermSpecs 里：前端挂它 = 死路由。' +
        '请改挂真实权限点，或先在后端登记该权限点（含 operatorReadOnlyGroups 层级评估）。'
    ).toEqual([])
  })
})
