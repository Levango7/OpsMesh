// a11y.js — 通用可访问性辅助（零侵入叠加层）。
//
// 设计原则：
//   - 纯叠加：只导出工具函数+ aria 属性，不改现有组件行为；
//   - 不影响现有测试（这些后续由各自组件主动 opt-in 调用）；
//   - i18n 文案由使用方提供（本文件不硬编码中文）。
//
// 说明：本文件是"零风险"收尾工具——所有函数无副作用，仅返回属性对。

/**
 * 生成 ARIA 角色与标签属性对（用于增强可访问性而无需改动组件结构）。
 * 用法：<tr v-bind="ariaRow('设备行', row.deviceID)"> → <tr role="row" aria-label="设备行: d-001">
 * @param {string} label 人类可读标签
 * @param {string|number} id 唯一标识（可选，用于 aria-describedby 场景）
 * @returns {{ role: string, 'aria-label': string }}
 */
export function ariaRow(label, id) {
  const safeId = id == null ? '' : String(id)
  return {
    role: 'row',
    'aria-label': safeId ? `${label}（${safeId}）` : label
  }
}

/**
 * 生成模态对话框的 ARIA 属性对（role + aria-modal + aria-labelledby）。
 * @param {string} modalId 对话框标题元素的 id
 * @returns {{ role: string, 'aria-modal': string, 'aria-labelledby': string, tabindex: string }}
 */
export function ariaModal(dialogId) {
  const id = dialogId || ''
  return {
    role: 'dialog',
    'aria-modal': 'true',
    'aria-labelledby': id ? id + '-title' : 'dialog-title',
    tabindex: -1
  }
}

/**
 * 生成按钮的 ARIA 属性对（用于无文字内容仅有图标的按钮）。
 * @param {string} label 屏幕阅读器提示文案
 * @returns {{ 'aria-label': string }}
 */
export function ariaIconButton(label) {
  return { 'aria-label': label }
}

/**
 * 焦点容器工具：modal 开启时捕获焦点在 modal 内，Esc 关闭同时恢复前一个焦点
 * 使用方式：modal 渲染后调用 trapFocus(el)，关闭时 unmount trap。
 * @param {HTMLElement} container 模态内容容器元素
 * @param {Function} onEscape Esc 处理回调（可选）
 * @returns {{destroy(): void}}
 */
export function trapFocus(container, onEscape) {
  if (!container) return { destroy() {} }

  const focusableSelector =
    'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"]), [contenteditable]'
  let previousFocus = null
  let liveRegion = null

  // 进入时记住当前焦点
  const activeBefore = document.activeElement
  if (activeBefore instanceof HTMLElement) {
    previousFocus = activeBefore
  }

  // 创建 Live Region 供屏幕阅读器播报
  liveRegion = document.createElement('div')
  liveRegion.setAttribute('aria-live', 'polite')
  liveRegion.setAttribute('aria-atomic', 'true')
  liveRegion.style.cssText = 'position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0,0,0,0);clip:rect(0 0 0 0);'
  document.body.appendChild(liveRegion)

  function onKeydown(e) {
    const isEsc = e.key === 'Escape' || e.key === 'Esc'
    if (isEsc) {
      if (onEscape) onEscape()
      return
    }
    if (e.key !== 'Tab') return

    const focusables = Array.from(container.querySelectorAll(focusableSelector))
    const modalFocusables = focusables.filter(
      el => !el.hasAttribute('disabled') && !el.getAttribute('aria-hidden')
    )
    if (modalFocusables.length === 0) return

    const first = modalFocusables[0]
    const last = modalFocusables[modalFocusables.length - 1]

    // Tab 出界回卷
    if (e.shiftKey) {
      // Shift+Tab：从第一个跳出时，循环到最后一个
      if (document.activeElement === first) {
        e.preventDefault()
        last.focus()
      }
    } else {
      // Tab：从最后一个跳出时，循环回第一个
      if (document.activeElement === last) {
        e.preventDefault()
        first.focus()
      }
    }
  }

  container.addEventListener('keydown', onKeydown)

  // 初始化焦点到第一个可聚焦元素（或容器本身）
  const initialFocusables = container.querySelectorAll(focusableSelector)
  if (initialFocusables.length > 0) {
    const first = initialFocusables[0]
    if (first instanceof HTMLElement) first.focus()
  } else if (container instanceof HTMLElement) {
    container.focus()
  }

  function destroy() {
    container.removeEventListener('keydown', onKeydown)
    if (liveRegion && liveRegion.parentNode) {
      liveRegion.parentNode.removeChild(liveRegion)
    }
    liveRegion = null
    // 恢复之前的焦点
    if (previousFocus && previousFocus instanceof HTMLElement) {
      previousFocus.focus()
    }
  }

  return { destroy }
}

/**
 * 创建屏幕阅读器辅助文本的 v-hide 样式（视觉隐藏但屏幕阅读器可读）。
 * 用法：<span v-bind="visuallyHidden">操作建议</span>
 * @returns {{position: string, width: string, height: string, clip: string, whiteSpace: string}}
 */
export function visuallyHidden() {
  return {
    position: 'absolute',
    width: '1px',
    height: '1px',
    clip: 'rect(0, 0, 0, 0)',
    whiteSpace: 'nowrap'
  }
}
