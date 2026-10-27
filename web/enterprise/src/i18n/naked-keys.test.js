// 翻译键覆盖守卫（棘轮）——全站字面量 t('key') 必须在 zh 与 en 中都能解析。
//
// 背景（2026-10-27 前端专项）：本类缺陷已连咬四次且都上过屏——
//   ① k8sDeploy/k8sLifecycle 命名空间错位（整两页标签渲染成原始键名）；
//   ② nav.gitops 键错名（存的是无人引用的 nav.gitops-dashboard）；
//   ③ ROUTE_DOMAIN_MAP 漏 9 条（域翻译不按需加载 = 从侧栏进入整页裸键）；
//   ④ knowledge.actions / deviceDiscovery.ip 等 12 键从未存在。
// 前三条有各自的门禁（nav/model.test.js ④⑦），本条兜底第四条：**任何字面量键
// 若在双语里查不到即判红**——无需等用户上报「这页怎么显示英文键名」。
//
// 机制内豁免（非缺陷，见下方 SKIP / 过滤）：
//   - 动态拼接前缀（'k8s.'、'flux.status_' 这类以 . 或 _ 结尾的片段）无法静态解析；
//   - PageShell 组件文档注释里的示例键 'x.title'/'x.subtitle'；
//   - 'a'/'default' 是 createElement('a') / fetchReport('default') 字符串的误匹配。
import { describe, it, expect } from 'vitest'

const sources = import.meta.glob(['../views/**/*.vue', '../components/*.vue', '../App.vue'], {
  eager: true,
  query: '?raw',
  import: 'default'
})
const zh = Object.assign(
  {},
  ...Object.values(import.meta.glob('./locales/zh/*.json', { eager: true })).map((m) => m.default)
)
const en = Object.assign(
  {},
  ...Object.values(import.meta.glob('./locales/en/*.json', { eager: true })).map((m) => m.default)
)

function resolve(dict, key) {
  return key.split('.').reduce((cur, p) => (cur && typeof cur === 'object' ? cur[p] : undefined), dict)
}

const KEY_RE = /(?<![A-Za-z_$])\$?t\(\s*['"]([A-Za-z][\w.]*)['"]/g
const SKIP = new Set(['x.title', 'x.subtitle', 'a', 'default'])

function literalKeys(src) {
  const out = new Set()
  for (const m of src.matchAll(KEY_RE)) {
    const k = m[1]
    if (SKIP.has(k) || k.endsWith('.') || k.endsWith('_')) continue
    out.add(k)
  }
  return out
}

describe('翻译键覆盖（裸键守卫）', () => {
  it('所有字面量 t(\u0027key\u0027) 在 zh 与 en 均可解析', () => {
    const missZh = []
    const missEn = []
    for (const [file, src] of Object.entries(sources)) {
      for (const k of literalKeys(src)) {
        if (typeof resolve(zh, k) !== 'string') missZh.push(`${file}: ${k}`)
        if (typeof resolve(en, k) !== 'string') missEn.push(`${file}: ${k}`)
      }
    }
    expect(missZh, 'zh 缺失的翻译键（会裸键上屏）').toEqual([])
    expect(missEn, 'en 缺失的翻译键（会裸键上屏）').toEqual([])
  })

  it('守卫自身有效：故意用缺失键扫描时必须能被抓到（负向自测）', () => {
    const fake = `<template>{{ $t('nonexistentns.title') }}</template>`
    expect([...literalKeys(fake)]).toEqual(['nonexistentns.title'])
    expect(typeof resolve(zh, 'nonexistentns.title')).not.toBe('string')
  })
})
