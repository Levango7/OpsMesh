// 共享原语统一门禁（2026-10-28 CSS 收编专项）
//
// 规则：全站共享原语只允许定义在 src/styles/pages.css。视图/组件的 <style scoped>
// 一律不得再定义它们——2026-10-28 实测 178 处 scoped 声明、78 个文件在遮蔽全局
// 同名规则，同名类最多有 8 种 .card / 16 种 .btnbar / 10 种 .field 写法，
// 这就是「东一榔头西一棒子」的技术根源。
//
// 判定口径：
//   - 用括号配对解析（不是按 ';' 切分——那样会把上条规则的 '}' 带进下一条选择器，
//     导致每个 style 块只能抓到第一条规则，是个真实的漏报陷阱）。
//   - 按归一化后的选择器串精确匹配，不按类名前缀猜，所以 .card h4 / .field-hint /
//     .card-grid 这类页面专属派生类不会被误伤。
//   - 嵌套在 @media/@supports/@keyframes 内的规则同样检查（那也在遮蔽全局）。
//
// 棘轮语义（与 page-conventions.spec.js 一致）：
//   ① 违规必须全在豁免清单里；
//   ② 清单条目只减不增，发现新违规即红；
//   ③ 清单条目必须写「为什么还留着」。
import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative, sep } from 'node:path'

const SRC_DIR = join(__dirname, '../..')

/** pages.css 实际owns 的选择器（归一化后）。新增原语必须同步这里。 */
const OWNED = new Set([
  // 卡片
  '.card',
  '.card__title',
  '.card h3',
  '.card > h3:first-child',
  '.card + .card',
  // 字段
  '.field',
  '.field label',
  '.field > label',
  // 按钮栏 / 表单栏
  '.btnbar',
  '.flowbar',
  '.flowbar .field',
  '.flowbar .field label',
  '.flowbar .field > label',
  '.flowbar .field select',
  '.flowbar .field input',
  '.flowbar .field select:focus',
  '.flowbar .field input:focus',
  // 行布局
  '.row',
  '.row-actions',
  // 文本辅助
  '.muted',
  '.msg',
  '.msg.ok',
  '.msg.err',
  '.hint',
  // 错误横幅
  '.poll-err',
  // 状态 / 节点行
  '.status-row',
  '.status-label',
  '.status-value',
  '.node-row',
  // 对话框家族 A（遮罩 + 面板）
  '.modal-mask',
  '.modal',
  '.modal-sm',
  '.modal-lg',
  '.modal-head',
  '.modal-head h3',
  '.modal-body',
  '.modal-body .field',
  '.modal-body .field label',
  '.modal-body .field input',
  '.modal-body .field textarea',
  '.modal-body .field input:focus',
  '.modal-body .field textarea:focus',
  '.modal-body .req',
  // 对话框家族 B（浮层 + 盒子）。BEM 的 .modal__* / .modal--* 是 ui/Modal.vue 组件
  // 的另一套命名，不遮蔽本家族，故不列入。
  '.modal-overlay',
  '.modal-box',
  '.modal-title',
  '.modal-message',
  '.modal-actions',
  '.modal-enter-active',
  '.modal-leave-active',
  '.modal-enter-from',
  '.modal-leave-to',
  '.modal-enter-active .modal-box',
  '.modal-leave-active .modal-box',
  '.modal-enter-from .modal-box',
  '.modal-leave-to .modal-box',
  // 第 7 批收编的跨文件重复派生类（判据见 pages.css 同名节：正文逐字节同文 +
  // 零泄漏；有分歧或会波及未覆盖用法的刻意不收，故这里只列这 18 个）
  '.auth-page',
  '.auth-form',
  '.auth-tools',
  '.submit-btn .spin',
  '.stat.indigo',
  '.stat.teal',
  '.stat.amber',
  '.stat.rose',
  '.status-pill.ok',
  '.status-pill.off',
  '.status-pill.bad',
  '.tag.st-failed',
  '.badge.info',
  '.tool-btn',
  '.tool-btn:hover',
  '.tab:hover',
  '.chart-bar:hover',
  '.bg-blob'
])

/** 已知例外：file → reasons。只减不增，空表即全清。 */
const EXEMPT = new Map()

function walk(dir) {
  const out = []
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) out.push(...walk(p))
    else if (name.endsWith('.vue')) out.push(p)
  }
  return out
}

/**
 * 括号配对解析出 <style scoped> 内**所有**规则的选择器（含 @media 等 at-rule 内的）。
 * 返回归一化后的单个选择器串列表（多选择器规则按逗号拆开）。
 */
function scopedSelectors(source) {
  const block = source.match(/<style[^>]*scoped[^>]*>([\s\S]*?)<\/style>/)?.[1]
  if (!block) return []

  const selectors = []
  let buf = ''
  let depth = 0
  let inAtRule = 0 // >0 表示当前处于 at-rule 块内部

  for (let i = 0; i < block.length; i++) {
    const c = block[i]
    if (c === '{') {
      if (depth === 0) {
        const prelude = buf.trim()
        buf = ''
        const at = prelude.match(/^@([a-z-]+)/)
        if (at) {
          // @media/@supports/@keyframes/@layer 内部规则也要查，所以继续深入但不记录 prelude
          inAtRule++
          if (at[1] === 'keyframes' || at[1] === 'layer') inAtRule += 100 // keyframes 内的 from/to 不是选择器
        } else if (inAtRule === 0) {
          selectors.push(prelude)
        } else {
          selectors.push(prelude)
        }
      } else {
        buf += c
      }
      depth++
      continue
    }
    if (c === '}') {
      depth--
      if (depth === 0) {
        buf = ''
        if (inAtRule > 0) inAtRule--
        if (inAtRule >= 100) inAtRule -= 100
      } else {
        buf += c
      }
      continue
    }
    if (c === ';' && depth === 0) {
      buf = ''
      continue
    }
    if (depth === 0) buf += c
  }

  const out = []
  for (const prelude of selectors) {
    for (const sel of prelude.split(',')) {
      const norm = sel.replace(/\s+/g, ' ').trim()
      if (norm && !norm.startsWith('@')) out.push(norm)
    }
  }
  return out
}

describe('共享原语单一来源（pages.css）', () => {
  it('扫描到足够的 .vue 文件（防目录变动后测试悄悄失效）', () => {
    expect(walk(SRC_DIR).length).toBeGreaterThan(90)
  })

  it('守卫自身解析正常（防解析 bug 导致漏报）', () => {
    // 同一段 CSS 里连续多条规则都必须被解析出来，而不是只抓到第一条
    const sels = scopedSelectors(
      '<style scoped>.card { padding: 1px; }\n.card__title { margin: 0; }\n.row { gap: 1px; }</style>'
    )
    expect(sels).toEqual(['.card', '.card__title', '.row'])
  })

  it.each(
    walk(SRC_DIR).map((f) => [relative(SRC_DIR, f).split(sep).join('/'), f])
  )('%s 未在 scoped 里重定义共享原语', (_rel, abs) => {
    const exempt = EXEMPT.get(_rel)
    const found = exempt
      ? []
      : scopedSelectors(readFileSync(abs, 'utf-8')).filter((s) => OWNED.has(s))
    expect(
      found,
      '请删除此处 scoped 副本（含 .field label 等后代形式与 @media 内的），' +
        '改用 styles/pages.css 的全局原语；确需变体请用已定义的 ' +
        '--raised/--bottom/--end/--grid-row/--fixed/--divided 修饰类'
    ).toEqual([])
  })
})
