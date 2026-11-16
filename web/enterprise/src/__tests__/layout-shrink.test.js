// 布局收缩门禁：内容区必须可收缩，否则窄视口整页横向滚动。
//
// 缺陷原型（2026-11-16 实测）：`.layout` 是行 flex，`.content` 作为 flex 项沿用默认
// `min-width: auto` —— 该值取**自身 min-content 宽**，而宽表格（.data-table）与长串
// 把它顶到 800+px。于是一旦视口变窄（≤1024），整页出现横向滚动：
//   /devices  1024×768 横向滚动 34px；768×1024 横向滚动 64px
//   /secrets  1024×768 横向滚动 6px
// 而 1440×900 下同样两页一切正常 —— 这正是「只在窄屏翻车」的典型形态。
//
// 修法：`.content` 显式 `min-width: 0`，让内容区随视口收缩；溢出改由
// DataTable 的 `.dt-wrap { overflow-x: auto }` **就地**承接（实测 1024 下
// wrap clientW 770 / scrollW 804，页面 scrollWidth 仍为 1024 = 不再整页滚动）。
//
// 为什么用静态判据：jsdom 不做真实盒模型布局，量不到 flex 的 min-content；
// 而这条规则一旦被后人删掉，症状要等到有人用窄屏打开页面才会被发现。
// 故在此钉住「`.content` 规则块里必须出现 min-width: 0」。
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const APP = resolve(__dirname, '../App.vue')

// 括号配对取 `.content { ... }` 的 rule 体（不按 ';' 切分：那会漏掉嵌套与多行声明）。
function ruleBody(src, selector) {
  const i = src.indexOf(selector + ' {')
  if (i < 0) return null
  let depth = 0
  for (let j = i + selector.length; j < src.length; j++) {
    if (src[j] === '{') depth++
    else if (src[j] === '}') {
      depth--
      if (depth === 0) return src.slice(i, j + 1)
    }
  }
  return null
}

describe('布局收缩', () => {
  it('.content 必须带 min-width: 0（否则窄视口整页横向滚动）', () => {
    const src = readFileSync(APP, 'utf-8')
    const body = ruleBody(src, '.content')
    expect(body, 'App.vue 里找不到 .content 规则——选择器改名了？请同步本门禁').toBeTruthy()
    expect(body).toMatch(/min-width:\s*0/)
  })

  it('.dt-wrap 必须保留 overflow-x: auto（表格就地滚动，而不是撑破页面）', () => {
    const src = readFileSync(resolve(__dirname, '../components/DataTable.vue'), 'utf-8')
    expect(src).toMatch(/\.dt-wrap\s*\{[^}]*overflow-x:\s*auto/)
  })
})
