// useFormatTime composable — 根据当前 i18n 语言格式化时间
// 用法：
//   import { fmtTime } from '@/composables/useFormatTime'
//   fmtTime('2026-01-01T00:00:00Z')        // 按当前语言格式化
//   fmtTime(s, '—')                         // 自定义空值占位
//   import { useFormatTime } from '@/composables/useFormatTime'
//   const { fmtTime } = useFormatTime()
import { currentLang } from '@/i18n'

// 根据当前语言选择 locale：中文 → zh-CN，英文 → en-US
function locale() {
  return currentLang.value === 'en' ? 'en-US' : 'zh-CN'
}

// 时间格式化：接收时间字符串或 Date 对象，按当前语言 locale 格式化
// 输出格式包含 year/month/day/hour/minute，24 小时制
// - 空值返回 defaultValue（默认 ''）
// - 无法解析的字符串原样返回，便于排查
export function fmtTime(s, defaultValue = '') {
  if (!s) return defaultValue
  const d = s instanceof Date ? s : new Date(s)
  if (isNaN(d.getTime())) return s
  return new Intl.DateTimeFormat(locale(), {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false
  }).format(d)
}

// composable 形式：在组件中 const { fmtTime } = useFormatTime()
export function useFormatTime() {
  return { fmtTime }
}

export default useFormatTime