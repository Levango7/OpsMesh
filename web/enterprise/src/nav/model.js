// nav/model.js — 侧栏导航模型（单一来源 = 路由表 src/router/index.js 的 meta）。
//
// 统一规则（2026-10-27 布局/分组专项）：
//   ① 侧栏入口 = 路由表中「非 public、非 hideInNav、有已登记 group」的每个入口键
//      （entry key = meta.navKey 或路由 name；同 navKey 的多页合并为一个入口，
//      落到文件中第一条路由的 path，如 k8s 家族四页 → 一个「K8s 管理」）；
//   ② 分组顺序与显示标签由本文件固定（GROUP_ORDER / GROUP_LABELS）；
//   ③ 分组归属、入口标签（meta.title）、图标（meta.icon）、权限门槛（meta.requirePerm）
//      全部取自路由表——新增页面只改路由表即自动进侧栏；
//   ④ 详情页/子 tab 页必须显式 meta.hideInNav（守卫测试见 model.test.js）——
//      hideInNav 是白名单，不许当「藏页面」的后门。
//
// 由 App.vue 消费（buildNavGroups(routes) → 渲染分组侧栏），测试直接测本模块。

export const GROUP_ORDER = [
  '概览',
  '运维管理',
  '资产配置',
  '交付中心',
  '可观测性',
  '自动化',
  'AI 算力',
  '平台',
  '系统管理'
]

export const GROUP_LABELS = {
  '概览': 'nav.overview',
  '运维管理': 'nav.ops',
  '资产配置': 'nav.assets',
  '交付中心': 'nav.delivery',
  '可观测性': 'nav.observability',
  '自动化': 'nav.groupAutomation',
  'AI 算力': 'nav.groupAiCompute',
  '平台': 'nav.groupPlatform',
  '系统管理': 'nav.system'
}

// 合并入口（同 navKey 多页）的标签：默认取首个路由的 meta.title；
// 此处为需要的家族指定更合适的既有键（K8s 家族沿用侧栏既有标签 nav.k8s）。
export const NAVKEY_LABELS = { k8s: 'nav.k8s' }

export function buildNavGroups(routeDefs) {
  const byGroup = new Map()
  const seen = new Set()
  for (const r of routeDefs) {
    const m = r.meta || {}
    if (!r.name || m.public || m.hideInNav) continue
    if (!m.group || !GROUP_LABELS[m.group]) continue
    const key = m.navKey || r.name
    if (seen.has(key)) continue
    seen.add(key)
    if (!byGroup.has(m.group)) byGroup.set(m.group, [])
    byGroup.get(m.group).push({
      name: key,
      path: r.path,
      icon: m.icon || 'task',
      labelKey: m.navKey ? NAVKEY_LABELS[m.navKey] || m.title : m.title,
      required: m.requirePerm || ''
    })
  }
  const out = []
  for (const g of GROUP_ORDER) {
    const items = byGroup.get(g)
    if (items && items.length) out.push({ group: g, labelKey: GROUP_LABELS[g], items })
  }
  return out
}
