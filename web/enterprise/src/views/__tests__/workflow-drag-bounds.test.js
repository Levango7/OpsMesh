// 画布拖拽边界门禁（2026-11-17）
//
// 缺陷原型：WorkflowsView 的画布节点拖拽只钳**左/上**（`Math.max(0, …)`），没有右/下边界
// ⇒ 节点可以被拖出画布右/下之外。画布本身 `overflow` 不可滚动、也无缩放，
// 出界的节点**捞不回来**（用户视角=节点凭空消失）。
//
// 为什么用静态判据：jsdom 不做真实盒模型布局，`svg.clientWidth` 恒 0、
// `getBoundingClientRect` 全零，量不到「拖出可视区」这个行为；
// 而这条钳制一旦被删，症状要等有人在真浏览器里拖一下才会发现。
// （与 `src/__tests__/layout-shrink.test.js` 同一取舍。）
//
// 判据钉**绑定形态**（不是泛泛的「有 Math.min」）：
//   - `onDragMove` 必须同时出现 `Math.max(0, …)`（左/上）与 `Math.min(…, clientWidth/Height)`
//     的钳制对；
//   - 钳制必须以 `canvasRef` 的可视尺寸为上界（不是写死的常量——固定 560px 会随
//     「视口感知高度」的 CSS 一起失效）。
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const src = readFileSync(resolve(__dirname, '../WorkflowsView.vue'), 'utf-8')

function fnBody(name) {
  const i = src.indexOf(`function ${name}(`)
  if (i < 0) return ''
  // 从函数起点到下一个 `function ` / 文件尾（浅切分足够：该函数体内无嵌套 function 声明）
  const next = src.indexOf('\nfunction ', i + 1)
  return src.slice(i, next < 0 ? src.length : next)
}

describe('WorkflowsView 画布拖拽边界', () => {
  const body = fnBody('onDragMove')

  it('onDragMove 存在且同时钳左/上与右/下', () => {
    expect(body, '找不到 onDragMove —— 拖拽实现被重构了？请同步本门禁').toBeTruthy()
    expect(body).toMatch(/Math\.max\(0,/)
    expect(body).toMatch(/Math\.min\(/)
  })

  it('右/下边界必须来自画布可视尺寸（canvasRef 的 clientWidth/Height）', () => {
    expect(body).toMatch(/clientWidth/)
    expect(body).toMatch(/clientHeight/)
    expect(body).toMatch(/canvasRef/)
  })
})
