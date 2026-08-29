// 全局 Toast 工具 — reactive 队列 + 自动消失 + 同文案节流
// 设计约束：
//   - 队列上限 MAX_TOASTS 条，超出时丢弃最旧
//   - 每条默认 AUTO_DISMISS_MS 后自动消失
//   - 同一文案 THROTTLE_MS 内只入队一次（防轮询类重复轰炸）
// 用法：
//   import { toast } from '@/utils/toast'
//   toast.error('操作失败')
//   toast.warn('磁盘告警')
//   toast.success('保存成功')
import { reactive } from 'vue'

export const MAX_TOASTS = 3
export const AUTO_DISMISS_MS = 6000
export const THROTTLE_MS = 10000

// 队列（reactive，供 ToastHost 渲染）
export const toasts = reactive([])

let seq = 0
const lastShownAt = new Map()

/**
 * 入队一条 toast。同文案在 THROTTLE_MS 内重复调用时直接忽略（返回 null）。
 * @param {string} title 文案
 * @param {{ type?: 'error'|'warn'|'success' }} [opts]
 * @returns {number|null} toast id，节流忽略时为 null
 */
export function push(title, opts = {}) {
  const key = String(title || '').trim()
  if (!key) return null
  const now = Date.now()
  const last = lastShownAt.get(key)
  if (last && now - last < THROTTLE_MS) return null
  lastShownAt.set(key, now)

  const item = { id: ++seq, type: opts.type || 'error', title: key }
  if (toasts.length >= MAX_TOASTS) toasts.shift()
  toasts.push(item)
  setTimeout(() => dismiss(item.id), AUTO_DISMISS_MS)
  return item.id
}

/** 手动关闭指定 toast */
export function dismiss(id) {
  const idx = toasts.findIndex((t) => t.id === id)
  if (idx !== -1) toasts.splice(idx, 1)
}

/** 清空全部（如登出时） */
export function clearAll() {
  toasts.splice(0, toasts.length)
}

// 便捷入口
export const toast = {
  error: (title) => push(title, { type: 'error' }),
  warn: (title) => push(title, { type: 'warn' }),
  success: (title) => push(title, { type: 'success' })
}

export default toast
