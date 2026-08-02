// 简易 i18n 模块 — 不依赖 vue-i18n，自己实现响应式语言切换
// 用法：
//   import { t, currentLang, setLang } from '@/i18n'
//   t('nav.devices')          // 翻译
//   currentLang.value         // 当前语言 'zh' | 'en'
//   setLang('en')             // 切换语言
import { ref } from 'vue'
import zh from './zh.json'
import en from './en.json'

const messages = { zh, en }
const STORAGE_KEY = 'opsmesh-lang'
const VALID = ['zh', 'en']

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
export function t(key, params) {
  const msg = get(messages[currentLang.value], key)
  if (msg == null) return key // 找不到返回 key 本身，便于排查
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