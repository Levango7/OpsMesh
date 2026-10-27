// 页面骨架统一门禁（2026-10-27 布局专项）
//
// 规则：src/views 下每个视图必须用 PageShell 组件包裹（引用 '@/components/PageShell' 即视为合规）。
// 迁移终态（2026-10-27）：四波共 78 页迁移完毕（Wave1 24 / Wave2 26 / Wave3 13 / Wave4 15），
// 存量豁免账 82 → 3——余下 3 条为**永久豁免**（见下），不再是待办。
// 棘轮语义（保留，防回潮）：
//   ① 非合规文件必须全在豁免清单里（新增页面不得绕过，新页面请直接用 PageShell）；
//   ② 清单里任何文件一旦合规就必须删条目，否则判红（防豁免变永久）；
//   ③ 条目只减不增。
import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative, sep } from 'node:path'

const VIEWS_DIR = join(__dirname, '..')

// 永久豁免（唯一允许的类别）：全屏认证页——登录/注册/首登改密位于 app 壳之外，
// 为全屏独立布局，不适用 PageShell。除此之外**不得新增豁免**。
const EXEMPT = new Set([
  'LoginView.vue',
  'RegisterView.vue',
  'ChangePasswordView.vue'
])

function walk(dir) {
  const out = []
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) out.push(...walk(p))
    else if (name.endsWith('.vue')) out.push(p)
  }
  return out
}

describe('页面骨架统一（PageShell）', () => {
  it('非合规视图 ⊆ 豁免清单，且豁免清单无过期条目（棘轮）', () => {
    const files = walk(VIEWS_DIR)
    const nonConforming = []
    for (const f of files) {
      const rel = relative(VIEWS_DIR, f).split(sep).join('/')
      const src = readFileSync(f, 'utf-8')
      if (!/components\/PageShell/.test(src)) nonConforming.push(rel)
    }
    // ① 新页面/未豁免页面必须合规
    const unlisted = nonConforming.filter((v) => !EXEMPT.has(v))
    expect(unlisted, '未用 PageShell 且不在豁免清单（新增页面请用 PageShell 包裹）').toEqual([])
    // ② 已合规的豁免条目必须删除（防豁免变永久）
    const stale = [...EXEMPT].filter((v) => !nonConforming.includes(v))
    expect(stale, '这些页面已合规，请从 EXEMPT 删除对应条目').toEqual([])
  })
})
