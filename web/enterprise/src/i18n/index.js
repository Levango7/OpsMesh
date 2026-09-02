// 简易 i18n 模块 — 不依赖 vue-i18n，自己实现响应式语言切换
// 用法：
//   import { t, currentLang, setLang, loadDomain, loadRouteDomain } from '@/i18n'
//   t('nav.devices')          // 翻译
//   currentLang.value         // 当前语言 'zh' | 'en'
//   setLang('en')             // 切换语言
//   await loadRouteDomain('devices')  // 路由切换时按需加载对应域翻译
//
// 按需加载策略：
//   - 初始同步加载 common 域（首屏 + 公共翻译：导航/按钮/错误提示/登录/概览等）
//   - 其他功能域（ops/assets/delivery/observability/system/automation/ai/platform）
//     通过 import.meta.glob 收集，路由切换时按需异步加载
//   - t() API 不变，向后兼容；未加载的键回退到 fallbackLang 或返回 key 本身
//
// 回退机制：当前语言缺少某键时，回退到 fallbackLang（默认 'zh'）查找，
// 仍找不到则返回 key 本身，便于排查缺失翻译。
import { ref } from 'vue'
import zhCommon from './locales/zh/common.json'
import enCommon from './locales/en/common.json'

const STORAGE_KEY = 'opsmesh-lang'
const VALID = ['zh', 'en']
// 回退语言：当 currentLang 缺少某键时从此查找。中文作为基准语言覆盖最全。
const FALLBACK_LANG = 'zh'

// 功能域列表（除 common 外，按需加载）
export const DOMAINS = [
  'ops',
  'assets',
  'delivery',
  'observability',
  'system',
  'automation',
  'ai',
  'platform'
]

// 路由名 → 功能域映射（供 router afterEach 钩子按需加载对应域翻译）
export const ROUTE_DOMAIN_MAP = {
  // 公共/概览：common 已初始加载，无需按需
  login: 'common',
  register: 'common',
  'change-password': 'common',
  overview: 'common',
  // 运维管理 → ops
  devices: 'ops',
  'device-detail': 'ops',
  tasks: 'ops',
  alerts: 'ops',
  'os-optimize': 'ops',
  middleware: 'ops',
  k8s: 'ops',
  batch: 'ops',
  // 资产配置 → assets
  cmdb: 'assets',
  // 交付中心 → delivery
  workflows: 'delivery',
  deploys: 'delivery',
  traffic: 'delivery',
  pipeline: 'delivery',
  argocd: 'delivery',
  canary: 'delivery',
  // 可观测性 → observability
  logs: 'observability',
  slos: 'observability',
  'alert-rules': 'observability',
  // 系统管理 → system
  users: 'system',
  roles: 'system',
  permissions: 'system',
  secrets: 'system',
  tickets: 'system',
  compliance: 'system',
  ha: 'system',
  backups: 'system',
  quotas: 'system',
  tenants: 'system',
  'audit-events': 'system',
  // 自动化 → automation
  schedules: 'automation',
  automation: 'automation',
  webhooks: 'automation',
  scripts: 'automation',
  runbooks: 'automation',
  incidents: 'automation',
  autoscaler: 'automation',
  // AI 算力 → ai
  gpu: 'ai',
  bot: 'ai',
  // 平台 → platform
  plugins: 'platform',
  portal: 'platform',
  billing: 'platform',
  apikeys: 'platform',
  'gateway-routes': 'platform',
  'notify-channels': 'platform',

  // ====================================================================
  // P1 五子域路由 → 功能域映射
  //   platform/federation → system
  //   deploys-federation   → delivery
  //   config/cmdb-*        → ops
  // ====================================================================
  platform: 'system',
  federation: 'system',
  'deploys-federation': 'delivery',
  config: 'ops',
  'cmdb-changes': 'ops',
  'cmdb-attr-templates': 'ops',
  'cmdb-collect': 'ops',

  // ====================================================================
  // P2 四子域路由 → 功能域映射
  //   approval-flows/approval-requests/audits → system
  //   network-topology/network-diagnose/network-devices/auto-provision → ops
  // ====================================================================
  'approval-flows': 'system',
  'approval-requests': 'system',
  'network-topology': 'ops',
  'network-diagnose': 'ops',
  'network-devices': 'ops',
  audits: 'system',
  'auto-provision': 'ops'
}

// 预先收集所有功能域 json 模块（Vite 会为每个 json 生成单独 chunk，按需加载）
const domainModules = import.meta.glob('./locales/*/*.json')

// messages：初始只含 common 域翻译（首屏立即可用）
const messages = {
  zh: { ...zhCommon },
  en: { ...enCommon }
}

// 已加载的域追踪：key = "lang:domain"，避免重复加载
const loadedDomains = new Set(['zh:common', 'en:common'])


// 当前语言响应式 ref
export const currentLang = ref(localStorage.getItem(STORAGE_KEY) || 'zh')
if (!VALID.includes(currentLang.value)) currentLang.value = 'zh'

// 响应式版本号：异步加载域翻译后递增，触发依赖 t() 的组件重新渲染
const i18nVersion = ref(0)

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
  // 依赖 i18nVersion，确保异步加载域翻译后 t() 重新计算
  i18nVersion.value
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

// 异步加载某语言的功能域翻译并合并到 messages[lang]
// 幂等：重复调用同一 (lang, domain) 只加载一次
export async function loadDomain(lang, domain) {
  if (!VALID.includes(lang) || !DOMAINS.includes(domain)) return
  const cacheKey = `${lang}:${domain}`
  if (loadedDomains.has(cacheKey)) return
  loadedDomains.add(cacheKey)
  try {
    const key = `./locales/${lang}/${domain}.json`
    const loader = domainModules[key]
    if (!loader) {
      loadedDomains.delete(cacheKey)
      return
    }
    const mod = await loader()
    Object.assign(messages[lang], mod.default || mod)
    // 加载完成后递增版本号，触发依赖 t() 的组件重新渲染
    i18nVersion.value++
  } catch {
    // 加载失败：移除标记，允许后续重试；t() 回退到 fallbackLang 或返回 key
    loadedDomains.delete(cacheKey)
  }
}

// 根据路由名加载对应域翻译（供 router afterEach 钩子调用）
// common 域已初始加载，直接跳过
export async function loadRouteDomain(routeName) {
  const domain = ROUTE_DOMAIN_MAP[routeName]
  if (!domain || domain === 'common') return
  await loadDomain(currentLang.value, domain)
}

// 切换语言并持久化
// 切换后异步加载新语言下所有已加载的域，确保已展示的翻译不丢失
export function setLang(lang) {
  if (!VALID.includes(lang)) return
  const oldLang = currentLang.value
  currentLang.value = lang
  localStorage.setItem(STORAGE_KEY, lang)
  document.documentElement.setAttribute('data-lang', lang)
  // 切换语言后，加载新语言下所有之前已加载的域（common 已初始加载，跳过）
  if (oldLang !== lang) {
    for (const key of loadedDomains) {
      if (key.startsWith(oldLang + ':')) {
        const domain = key.slice(oldLang.length + 1)
        if (domain !== 'common') loadDomain(lang, domain)
      }
    }
  }
}

// 初始化：同步 DOM 属性
export function initLang() {
  document.documentElement.setAttribute('data-lang', currentLang.value)
}

// 测试专用：同步注入域翻译到 messages，避免 eager glob 污染生产构建
// 仅在 vitest.setup.js 中调用，生产环境不会使用
export function __injectDomainMessages(lang, domain, msgs) {
  if (!VALID.includes(lang) || domain === 'common') return
  Object.assign(messages[lang], msgs)
  loadedDomains.add(`${lang}:${domain}`)
  i18nVersion.value++
}

export default {
  t,
  currentLang,
  setLang,
  initLang,
  loadDomain,
  loadRouteDomain,
  __injectDomainMessages,
  DOMAINS,
  ROUTE_DOMAIN_MAP
}
