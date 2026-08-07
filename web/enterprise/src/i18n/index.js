// 简易 i18n 模块 — 不依赖 vue-i18n，自己实现响应式语言切换
// 用法：
//   import { t, currentLang, setLang } from '@/i18n'
//   t('nav.devices')          // 翻译
//   currentLang.value         // 当前语言 'zh' | 'en'
//   setLang('en')             // 切换语言
// 回退机制：当前语言缺少某键时，回退到 fallbackLang（默认 'zh'）查找，
// 仍找不到则返回 key 本身，便于排查缺失翻译。
import { ref } from 'vue'
import zh from './zh.json'
import en from './en.json'

const messages = { zh, en }
const STORAGE_KEY = 'opsmesh-lang'
const VALID = ['zh', 'en']
// 回退语言：当 currentLang 缺少某键时从此查找。中文作为基准语言覆盖最全。
const FALLBACK_LANG = 'zh'

// 当前语言响应式 ref
export const currentLang = ref(localStorage.getItem(STORAGE_KEY) || 'zh')
if (!VALID.includes(currentLang.value)) currentLang.value = 'zh'

// 按点分路径取嵌套值：get(obj, 'a.b.c') → obj.a.b.c
function get(obj, path) {
  const keys = path.split('.')
  let cur = obj
  for (const k of keys) {
    if (cur == null || typeof cur !== 'object') return undefined
    cur = cur[k]
  }
  return cur
}

// 翻译函数：t(key, params?) — 支持插值 {name}
// 查找顺序：currentLang → fallbackLang → 返回 key 本身
export function t(key, params) {
  let msg = get(messages[currentLang.value], key)
  // 当前语言缺失时回退到 fallbackLang（避免 UI 出现裸键或 undefined）
  if (msg == null && currentLang.value !== FALLBACK_LANG) {
    msg = get(messages[FALLBACK_LANG], key)
  }
  if (msg == null) return key // 仍找不到返回 key 本身，便于排查
  if (typeof msg !== 'string' || !params) return msg
  // 简单插值：{name} → params.name
  return msg.replace(/\{(\w+)\}/g, (_, k) => (params[k] != null ? params[k] : `{${k}}`))
}

// 切换语言并持久化
export function setLang(lang) {
  if (!VALID.includes(lang)) return
  currentLang.value = lang
  localStorage.setItem(STORAGE_KEY, lang)
  document.documentElement.setAttribute('data-lang', lang)
}

// 初始化：同步 DOM 属性
export function initLang() {
  document.documentElement.setAttribute('data-lang', currentLang.value)
}

export default { t, currentLang, setLang, initLang }