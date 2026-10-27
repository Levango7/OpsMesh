// 侧栏导航守卫（棘轮）——「路由表单一来源」后的不变量。
//
// 背景（2026-10-27 布局/分组专项）：改单一来源前，App.vue 手写 25 项侧栏与
// 路由 meta.group 互相漂移——4 个页面分组矛盾（runbooks/incidents/autoscaler/portal），
// 38 条已实现路由全站零入口（批量执行/Helm/备份/审批/AIOps 等只能手打 URL）。
// 本测试把「不漂移」固化为判据：
//   ① 覆盖铁律——凡声明了分组的页面必须在侧栏出现（要么进、要么显式 hideInNav）；
//   ② hideInNav 是白名单——只有详情页/子 tab 页可隐藏，新增必须在此登记（防后门）；
//   ③ 分组必须都在 GROUP_ORDER/GROUP_LABELS 登记；
//   ④ 分组与入口标签在 zh/en 双语都能解析（防裸键上屏，如 k8sDeploy 旧案）；
//   ⑤ 入口名唯一、path 静态（参数页不许混进侧栏）。
import { describe, it, expect } from 'vitest'
import { routes } from '@/router'
import { ROUTE_DOMAIN_MAP } from '@/i18n'
import { buildNavGroups, GROUP_ORDER, GROUP_LABELS } from './model'

const zh = Object.assign(
  {},
  ...Object.values(import.meta.glob('../i18n/locales/zh/*.json', { eager: true })).map((m) => m.default)
)
const en = Object.assign(
  {},
  ...Object.values(import.meta.glob('../i18n/locales/en/*.json', { eager: true })).map((m) => m.default)
)

function resolve(dict, key) {
  return key.split('.').reduce((cur, p) => (cur && typeof cur === 'object' ? cur[p] : undefined), dict)
}

// hideInNav 白名单：详情页（参数路由）/ 子 tab 页。新增一条 = 明确决定该页不进侧栏。
const HIDDEN_ALLOW = new Set(['device-detail', 'batch-exec', 'batch-detail', 'canary-detail'])

describe('侧栏导航模型（路由表单一来源）', () => {
  const groups = buildNavGroups(routes)

  it('① 覆盖铁律：非 public、非 hideInNav、有分组的每个入口都进侧栏', () => {
    const inNav = new Set(groups.flatMap((g) => g.items.map((i) => i.name)))
    const missing = []
    for (const r of routes) {
      const m = r.meta || {}
      if (!r.name || m.public || m.hideInNav || !m.group) continue
      const key = m.navKey || r.name
      if (!inNav.has(key)) missing.push(key)
    }
    expect(missing, '以下路由有分组却不在侧栏（补 meta 或显式 hideInNav）').toEqual([])
  })

  it('② hideInNav 只能是白名单内的页面（防把页面藏起来当后门）', () => {
    const hidden = routes.filter((r) => r.meta?.hideInNav).map((r) => r.name)
    expect(hidden.sort()).toEqual([...HIDDEN_ALLOW].sort())
  })

  it('③ 分组：顺序与 GROUP_ORDER 一致，且没有未登记的分组值', () => {
    const gs = groups.map((g) => g.group)
    expect(gs).toEqual(GROUP_ORDER.filter((g) => gs.includes(g)))
    const unregistered = routes
      .filter((r) => r.meta?.group && !GROUP_LABELS[r.meta.group] && !r.meta.public)
      .map((r) => `${r.name}:${r.meta.group}`)
    expect(unregistered, '路由声明了未登记分组（请登记进 GROUP_LABELS 或修正 meta）').toEqual([])
  })

  it('④ 分组与入口标签在 zh 与 en 均能解析（防裸键上屏）', () => {
    const keys = new Set(groups.map((g) => g.labelKey))
    for (const g of groups) for (const i of g.items) keys.add(i.labelKey)
    expect([...keys].filter((k) => typeof resolve(zh, k) !== 'string'), 'zh 缺失导航键').toEqual([])
    expect([...keys].filter((k) => typeof resolve(en, k) !== 'string'), 'en 缺失导航键').toEqual([])
  })

  it('⑤ 入口名全局唯一、path 为静态路径', () => {
    const items = groups.flatMap((g) => g.items)
    const names = items.map((i) => i.name)
    expect(new Set(names).size).toBe(names.length)
    for (const i of items) {
      expect(i.path.startsWith('/'), `${i.name} path 必须以 / 开头`).toBe(true)
      expect(i.path.includes(':'), `${i.name} path 含参数段，参数页不应进侧栏`).toBe(false)
      expect(typeof i.icon).toBe('string')
      expect(i.icon.length > 0).toBe(true)
    }
  })

  it('⑥ 同 navKey 家族合并为一项（k8s 四页 → 一个入口，落到首个路由 path）', () => {
    const k8s = groups.flatMap((g) => g.items).filter((i) => i.name === 'k8s')
    expect(k8s.length).toBe(1)
    expect(k8s[0].path).toBe('/k8s/clusters')
  })

  it('⑦ 域翻译映射覆盖全部非公开路由（漏映射 = 进页面整页裸键）', () => {
    const missing = routes
      .filter((r) => r.name && !r.meta?.public && !ROUTE_DOMAIN_MAP[r.name])
      .map((r) => r.name)
    expect(missing, '以下路由未映射功能域（请在 ROUTE_DOMAIN_MAP 登记）').toEqual([])
  })
})
