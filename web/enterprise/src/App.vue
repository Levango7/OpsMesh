<template>
  <!-- 未登录：只渲染路由（登录/注册页自己负责全屏布局） -->
  <router-view v-if="!authStore.isLoggedIn" />

  <!-- 已登录：主布局（顶栏 + 侧栏 + 内容 + 底栏） -->
  <div v-else class="app" :class="{ 'app--sidebar-collapsed': sidebarCollapsed }">
    <!-- 顶栏 -->
    <header class="appbar">
      <div class="appbar-left">
        <!-- 移动端侧栏切换按钮（仅 ≤768px 可见） -->
        <button
          class="icon-btn menu-toggle"
          :aria-label="$t('common.menu')"
          data-testid="topbar-menu-toggle"
          @click="sidebarOpen = !sidebarOpen"
        >
          <Icon name="task" :size="18" />
        </button>
        <!-- 桌面端侧栏折叠/展开按钮 -->
        <button
          class="icon-btn sidebar-collapse-btn"
          :class="{ 'is-collapsed': sidebarCollapsed }"
          :aria-label="sidebarCollapsed ? $t('sidebar.expand', 'Expand sidebar') : $t('sidebar.collapse', 'Collapse sidebar')"
          :aria-pressed="sidebarCollapsed"
          data-testid="sidebar-collapse-toggle"
          @click="toggleSidebarCollapse"
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <rect x="3" y="3" width="18" height="18" rx="2" />
            <line x1="9" y1="3" x2="9" y2="21" />
            <polyline :points="sidebarCollapsed ? '15 9 18 12 15 15' : '13 9 10 12 13 15'" />
          </svg>
        </button>
        <div class="brand">
          <div class="logo">
            <!-- OpsMesh 品牌标识：六边形运维网格（与 favicon.svg 完全一致） -->
            <svg class="brand-logo" viewBox="0 0 24 24" fill="none" aria-hidden="true">
              <path
                d="M12 2.6L20.1 6.9v10.2L12 21.4L3.9 17.1V6.9L12 2.6z"
                stroke="currentColor" stroke-width="1.1" stroke-linejoin="round" opacity="0.85"
              />
              <g stroke="currentColor" stroke-width="0.9" stroke-linecap="round" opacity="0.55">
                <path d="M12 12V2.6M12 12l8.1-5.1M12 12l8.1 5.1M12 12v9.4M12 12l-8.1 5.1M12 12L3.9 6.9" />
              </g>
              <circle cx="12" cy="12" r="2.1" fill="currentColor" />
              <circle cx="12" cy="2.6" r="1.2" fill="currentColor" opacity="0.95" />
              <circle cx="20.1" cy="6.9" r="1.2" fill="currentColor" opacity="0.95" />
              <circle cx="20.1" cy="17.1" r="1.2" fill="currentColor" opacity="0.95" />
              <circle cx="12" cy="21.4" r="1.2" fill="currentColor" opacity="0.95" />
              <circle cx="3.9" cy="17.1" r="1.2" fill="currentColor" opacity="0.95" />
              <circle cx="3.9" cy="6.9" r="1.2" fill="currentColor" opacity="0.95" />
            </svg>
          </div>
          <div class="brand-text">
            <h1>{{ $t('app.title') }}</h1>
            <div class="sub">{{ $t('app.subtitle') }}</div>
          </div>
        </div>
      </div>

      <div class="appbar-right">
        <!-- 资源统计 chip -->
        <div class="appbar-meta">
          <span class="chip">{{ $t('topbar.devices') }} <b>{{ deviceStore.total }}</b></span>
          <span class="chip">{{ $t('topbar.managed') }} <b>{{ deviceStore.managed }}</b></span>
          <span class="chip">{{ $t('topbar.alerts') }} <b>{{ alertStore.list.length }}</b></span>
        </div>

        <!-- 主题切换 -->
        <button
          class="icon-btn"
          @click="themeStore.toggle()"
          :title="themeStore.isDark ? $t('topbar.theme_light') : $t('topbar.theme_dark')"
          :aria-label="themeStore.isDark ? $t('topbar.theme_light') : $t('topbar.theme_dark')"
          :aria-pressed="themeStore.isDark"
        >
          <Icon :name="themeStore.isDark ? 'theme-light' : 'theme-dark'" :size="18" />
        </button>

        <!-- 语言切换 -->
        <button class="icon-btn" @click="toggleLang" :title="$t('topbar.lang')" :aria-label="$t('topbar.lang')" aria-live="polite">
          <Icon name="lang" :size="18" />
          <span class="lang-label">{{ currentLang === 'zh' ? '中' : 'EN' }}</span>
        </button>

        <!-- 用户信息 -->
        <div class="user-info">
          <span class="user-avatar"><Icon name="users" :size="16" /></span>
          <span class="user-name" data-testid="topbar-username">{{ authStore.user?.username || '—' }}</span>
        </div>

        <!-- 退出 -->
        <button
          class="icon-btn danger"
          @click="onLogout"
          :title="$t('topbar.logout')"
          :aria-label="$t('topbar.logout')"
          data-testid="topbar-logout"
        >
          <Icon name="logout" :size="18" />
        </button>
      </div>
    </header>

    <div class="layout">
      <!-- 移动端侧栏遮罩（点击关闭抽屉） -->
      <div
        v-if="sidebarOpen"
        class="sidebar-mask"
        data-testid="sidebar-mask"
        @click="sidebarOpen = false"
      />
      <!-- 侧栏导航：分组 + 编号功能入口 -->
      <aside class="sidebar" :class="{ open: sidebarOpen, collapsed: sidebarCollapsed }">
        <div v-for="grp in navView" :key="grp.group" class="nav-group">
          <div class="nav-group-title">{{ $t(grp.labelKey) }}</div>
          <div
            v-for="item in grp.items"
            :key="item.name"
            class="tab"
            :class="{ active: isNavActive(item.name) }"
            :data-label="$t(item.labelKey)"
            :title="sidebarCollapsed ? $t(item.labelKey) : ''"
            @click="goNav(item.name)"
          >
            <span class="tab-index">{{ item.index }}</span>
            <span class="tab-icon"><Icon :name="item.icon" :size="16" /></span>
            <span class="tab-label">{{ $t(item.labelKey) }}</span>
          </div>
        </div>

      </aside>

      <!-- 主内容区 -->
      <main class="content">
        <router-view v-slot="{ Component }">
          <transition name="fade" mode="out-in">
            <component :is="Component" />
          </transition>
        </router-view>
      </main>
    </div>

    <footer class="footbar">
      <span class="footbar-brand">{{ $t('app.footer') }}</span>
      <span class="muted">{{ $t('app.copyright') }}</span>
    </footer>
  </div>

  <!-- 全局错误 Toast（独立根节点，始终挂载；仅登录后的非 401 API 错误会触发） -->
  <ToastHost />
</template>

<script setup>
// 应用根组件 — 顶栏 + 分组侧栏 + 内容 + 底栏
// 已登录显示主布局；未登录仅渲染路由（登录/注册页全屏）
import { onMounted, onUnmounted, computed, watch, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useDeviceStore } from '@/stores/device'
import { useAlertStore } from '@/stores/alert'
import { useAuthStore } from '@/stores/auth'
import { useThemeStore } from '@/stores/theme'
import { currentLang, setLang } from '@/i18n'
import { createSSEClient } from '@/api/sse'
import Icon from '@/components/Icon.vue'
import ToastHost from '@/components/ToastHost.vue'

const router = useRouter()
const deviceStore = useDeviceStore()
const alertStore = useAlertStore()
const authStore = useAuthStore()
const themeStore = useThemeStore()

// 分组导航：每组 { group, labelKey, items[{ name, icon, labelKey, required }] }
// required 为该功能入口对应的后端权限（与 requireProd 闸同源）；
// 为空表示无需权限门槛（如概览页，登录即可见）。侧栏将按当前用户权限过滤。
//
// 图标唯一性：每个入口使用语义化且互不重复的图标（无重复），
// 避免用户在长列表中靠位置记忆定位功能。
// 全部为「实心填充」风格，视觉重量一致（不使用线框图标）。
const navGroups = [
  {
    group: 'overview', labelKey: 'nav.overview',
    items: [{ name: 'overview', icon: 'home', labelKey: 'nav.home', required: '' }]
  },
  {
    group: 'ops', labelKey: 'nav.ops',
    items: [
      { name: 'devices', icon: 'device', labelKey: 'nav.devices', required: 'device:read' },
      { name: 'tasks', icon: 'task', labelKey: 'nav.tasks', required: 'task:read' },
      { name: 'dead-letter', icon: 'fail', labelKey: 'nav.dead_letter', required: 'task:read' },
      { name: 'alerts', icon: 'alerts', labelKey: 'nav.alerts', required: 'alert:read' },
      { name: 'os-optimize', icon: 'tune', labelKey: 'nav.osopt', required: 'task:read' },
      { name: 'system-ops', icon: 'settings', labelKey: 'nav.system_ops', required: 'system:write' },
      { name: 'middleware', icon: 'plugin', labelKey: 'nav.mwdep', required: 'deploy:read' },
      { name: 'infra-console', icon: 'datacenter', labelKey: 'nav.infra_console', required: 'device:read' },
      { name: 'k8s', icon: 'k8s', labelKey: 'nav.k8s', required: 'device:read' }
    ]
  },
  {
    group: 'assets', labelKey: 'nav.assets',
    items: [{ name: 'cmdb', icon: 'cmdb', labelKey: 'nav.cmdb', required: 'cmdb:read' }]
  },
  {
    group: 'delivery', labelKey: 'nav.delivery',
    items: [
      { name: 'workflows', icon: 'branch', labelKey: 'nav.workflows', required: 'workflow:read' },
      { name: 'deploys', icon: 'deploy', labelKey: 'nav.deploys', required: 'deploy:read' }
    ]
  },
  {
    group: 'aiops', labelKey: 'nav.aiops',
    items: [
      { name: 'gpu', icon: 'gpu', labelKey: 'nav.gpu', required: 'gpu:read' },
      { name: 'portal', icon: 'portal', labelKey: 'nav.portal', required: 'portal:read' },
      { name: 'runbooks', icon: 'runbook', labelKey: 'nav.runbooks', required: 'runbook:read' },
      { name: 'incidents', icon: 'incident', labelKey: 'nav.incidents', required: 'incident:read' },
      { name: 'autoscaler', icon: 'autoscaler', labelKey: 'nav.autoscaler', required: 'autoscaler:read' },
      { name: 'bot', icon: 'bot', labelKey: 'nav.bot', required: 'bot:read' }
    ]
  },
  {
    group: 'observability', labelKey: 'nav.observability',
    items: [{ name: 'logs', icon: 'logs', labelKey: 'nav.logs', required: 'log:read' }]
  },
  {
    group: 'system', labelKey: 'nav.system',
    items: [
      { name: 'users', icon: 'users', labelKey: 'nav.users', required: 'user:read' },
      { name: 'roles', icon: 'roles', labelKey: 'nav.roles', required: 'role:read' },
      { name: 'permissions', icon: 'permissions', labelKey: 'nav.permissions', required: 'role:read' },
      { name: 'secrets', icon: 'key', labelKey: 'nav.secrets', required: 'role:read' },
      { name: 'admin-console', icon: 'settings', labelKey: 'nav.admin_console', required: 'diagnostics:dump' }
    ]
  }
]

// 权限过滤后的侧栏视图：仅保留当前用户有权限的入口，并赋予全局连续编号（1、2、3…）。
// 编号即"功能入口序号"，用户点击即可执行对应操作；空权限集合时（demo 等）放宽展示全部。
const navView = computed(() => {
  const out = []
  let idx = 0
  for (const grp of navGroups) {
    const items = grp.items
      .filter((it) => authStore.hasPerm(it.required))
      .map((it) => ({ ...it, index: ++idx }))
    if (items.length) out.push({ ...grp, items })
  }
  return out
})

// 切换语言
function toggleLang() {
  setLang(currentLang.value === 'zh' ? 'en' : 'zh')
}

// 移动端侧栏抽屉开关（仅 ≤768px 生效，桌面端侧栏始终可见）
const sidebarOpen = ref(false)

// 桌面端侧栏折叠状态（210px ↔ 64px 仅图标）。持久化到 localStorage，
// 保证刷新/重新登录后保持用户偏好。
const SIDEBAR_COLLAPSE_KEY = 'opsmesh-sidebar-collapsed'
const sidebarCollapsed = ref(false)
try {
  sidebarCollapsed.value = localStorage.getItem(SIDEBAR_COLLAPSE_KEY) === '1'
} catch {
  // localStorage 不可用（隐私模式/SSR）时降级为不折叠
  sidebarCollapsed.value = false
}
function toggleSidebarCollapse() {
  sidebarCollapsed.value = !sidebarCollapsed.value
  try {
    localStorage.setItem(SIDEBAR_COLLAPSE_KEY, sidebarCollapsed.value ? '1' : '0')
  } catch {
    // 忽略持久化失败，不影响本次会话内的折叠行为
  }
}
// 点击导航项：跳转并关闭移动端抽屉
function goNav(name) {
  router.push('/' + name)
  sidebarOpen.value = false
}
// 侧栏高亮判定：子页面通过 meta.navKey 归并回父级条目
// （如 K8s 拆分为 /k8s/clusters 与 /k8s/resources，两页都高亮「K8s 管理」）
function isNavActive(name) {
  const r = router.currentRoute.value
  return r.name === name || r.meta?.navKey === name
}
// 路由切换时自动关闭移动端抽屉（避免点击后退/前进时抽屉残留）
watch(() => router.currentRoute.value.path, () => { sidebarOpen.value = false })

// 退出登录
function onLogout() {
  stopPolls()
  stopSSE()
  authStore.logout()
  router.push({ name: 'login' })
}

// 主轮询：受页面可见性控制（仅已登录时启动）
// SSE 接入后轮询降级为兜底（SSE 断线时恢复），正常时由事件驱动刷新。
let pollTimers = []
let sseClient = null
function startPolls() {
  if (pollTimers.length) return
  pollTimers = [
    setInterval(() => deviceStore.fetchDevices(), 5000),
    setInterval(() => alertStore.fetchAlerts(), 10000)
  ]
}
function stopPolls() { pollTimers.forEach(clearInterval); pollTimers = [] }

// SSE 事件 → store 刷新映射（事件到达即拉最新，替代轮询的主动拉取）。
// 仅刷新"列表/计数"级数据；详情页自身仍有局部轮询（DeviceDetail 等）不受影响。
function handleSSEEvent(ev) {
  switch (ev.type) {
    case 'device_online':
    case 'device_offline':
      deviceStore.fetchDevices()
      break
    case 'alert_new':
      alertStore.fetchAlerts()
      // 将 AIOps 根因分析结果缓存到 store，供告警视图展示
      try {
        const rc = ev.data.rootCause
        if (rc) {
          let parsed = rc
          if (typeof rc === 'string') {
            parsed = JSON.parse(rc)
          }
          alertStore.setRootCause(ev.data.alertID, {
            category: parsed.category,
            component: parsed.component,
            description: parsed.description,
            confidence: parsed.confidence,
            evidence: parsed.evidence || []
          })
        }
        // 缓存 traceID，供告警详情页跳转到链路追踪
        if (ev.data.traceID) {
          alertStore.setTraceID(ev.data.alertID, ev.data.traceID)
        }
      } catch {
        // 根因解析失败不阻断告警刷新
      }
      break
    case 'task_status':
      // 任务状态变更：task store 在路由懒加载中，此处经事件总线通知各视图自行刷新。
      window.dispatchEvent(new CustomEvent('opsmesh:task-status', { detail: ev.data }))
      break
    default:
      // hello / approval_status / schedule_status / *_template_changed / agent_logs
      // 目前无全局列表依赖，事件仅消费（含契约校验），无需主动拉取。
      break
  }
}

function startSSE() {
  if (sseClient) return
  sseClient = createSSEClient({
    url: '/api/v1/events/stream',
    onEvent: handleSSEEvent,
    // 断线（非鉴权失败）时恢复轮询兜底；重连成功由 onConnect 暂停轮询。
    onDisconnect: () => { startPolls() },
    onConnect: () => { stopPolls() }
  })
  sseClient.start()
}
function stopSSE() {
  if (sseClient) { sseClient.stop(); sseClient = null }
}

function onVisibility() {
  if (document.hidden) { stopPolls(); stopSSE() }
  else { startPolls(); startSSE(); deviceStore.fetchDevices(); alertStore.fetchAlerts() }
}

onMounted(() => {
  // 首屏挂载时：若已登录（极少见，仅非首次加载时可能），直接启动实时层
  if (authStore.isLoggedIn) {
    deviceStore.fetchDevices()
    alertStore.fetchAlerts()
    document.addEventListener('visibilitychange', onVisibility)
    startPolls()
    startSSE()
  }
})

// 响应登录态变化：无论是 cold start 后的 fetchMe 完成，还是登录页 SPA 跳转，
// isLoggedIn 变为 true 时都要启动 SSE/轮询/visibility 监听（幂等，已启动则忽略）。
watch(
  () => authStore.isLoggedIn,
  (loggedIn) => {
    if (loggedIn) {
      deviceStore.fetchDevices()
      alertStore.fetchAlerts()
      document.addEventListener('visibilitychange', onVisibility)
      startPolls()
      startSSE()
    }
  }
)

onUnmounted(() => {
  stopPolls()
  stopSSE()
  document.removeEventListener('visibilitychange', onVisibility)
})
</script>

<style scoped>
.app {
  display: flex; flex-direction: column; min-height: 100vh;
  /* 底布由 body 的网格渐变承担，这里保持透明让光晕透上来 */
  background: transparent;
  /* 侧栏宽度单一事实源：侧栏本体与页脚品牌文字共用，折叠时同步收窄 */
  --sidebar-w: 210px;
}
.app--sidebar-collapsed { --sidebar-w: 64px; }
.appbar {
  position: sticky; top: 0; z-index: 30;
  display: flex; align-items: center; justify-content: space-between;
  /* 左内边距 14px：折叠按钮（36px 宽）的中心落在 x=32，
     与折叠态侧栏（64px 宽、图标居中）的图标中心 x=32 精确对齐 */
  padding: 10px 20px 10px 14px;
  /* 顶栏：更通透的玻璃，滚动时透出底布网格 */
  background: var(--glass-topbar-bg, rgba(255, 255, 255, 0.58));
  backdrop-filter: blur(var(--glass-blur, 20px)) saturate(160%);
  -webkit-backdrop-filter: blur(var(--glass-blur, 20px)) saturate(160%);
  border-bottom: 1px solid var(--glass-topbar-border, rgba(255, 255, 255, 0.42));
  box-shadow: var(--glass-topbar-shadow, 0 6px 24px rgba(31, 38, 135, 0.08));
}
.brand { display: flex; align-items: center; gap: 12px; }
.brand .logo {
  width: 38px; height: 38px; border-radius: 10px;
  background: linear-gradient(135deg, var(--indigo), var(--sky));
  display: flex; align-items: center; justify-content: center;
  color: #fff;
  box-shadow: 0 4px 14px rgba(99,102,241,.4);
  flex-shrink: 0;
}
.brand-logo { width: 24px; height: 24px; color: #fff; }
.brand h1 { line-height: 1.1; font-size: 16px; }
.brand .sub { font-size: 12px; color: var(--text-3); }

.appbar-right { display: flex; align-items: center; gap: 8px; }
.appbar-left { display: flex; align-items: center; gap: 10px; }
/* 移动端菜单切换按钮：桌面端隐藏，≤768px 显示。
   选择器需比 .icon-btn 更具体 —— 否则同优先级的 .icon-btn{display:inline-flex}
   会因书写顺序靠后而胜出，导致汉堡按钮在桌面端误显示。 */
.icon-btn.menu-toggle { display: none; }
.appbar-meta { display: flex; gap: 6px; flex-wrap: wrap; margin-right: 4px; }
.chip {
  font-size: 12px; color: var(--text-2);
  background: var(--surface-3); border: 1px solid var(--border);
  padding: 3px 10px; border-radius: 999px; font-variant-numeric: tabular-nums;
}
.chip b { color: var(--text); font-weight: 600; }

/* 顶栏图标按钮 */
.icon-btn {
  display: inline-flex; align-items: center; justify-content: center; gap: 4px;
  height: 34px; padding: 0 10px;
  background: var(--surface-3); border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  color: var(--text-2); cursor: pointer; transition: .15s;
  /* line-height:0 消除 inline 元素基线留白，使图标在按钮内严格垂直居中 */
  line-height: 0;
}
.icon-btn:hover { background: var(--bg-soft); color: var(--text); }
.icon-btn.danger:hover { color: var(--fail); border-color: var(--fail); }
.lang-label { font-size: 12px; font-weight: 600; line-height: 1; }
/* SVG 默认 display:inline 会带基线间隙，改为 block 后由 flex 居中 */
.icon-btn svg { display: block; }

/* 用户信息 */
.user-info {
  display: inline-flex; align-items: center; gap: 6px;
  height: 34px; padding: 0 10px;
  background: var(--accent-soft); border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  color: var(--text);
}
.user-avatar {
  width: 22px; height: 22px; border-radius: 50%;
  background: var(--accent); color: #fff;
  display: inline-flex; align-items: center; justify-content: center;
}
.user-name { font-size: 13px; font-weight: 500; }

/* 布局 */
.layout { display: flex; flex: 1; min-height: 0; }
.sidebar {
  flex: 0 0 var(--sidebar-w, 210px); width: var(--sidebar-w, 210px);
  background: var(--color-surface-1, #ffffff);
  border-right: 1px solid var(--color-hairline, #d3dbe8);
  padding: 16px 12px;
  box-shadow: var(--shadow-md, 0 4px 6px rgba(0, 0, 0, 0.08));
}

/* 侧栏折叠态：仅保留图标。
   固定 64px 宽 + 12px 内边距 → 内容区 40px，16px 图标居中后左右各留 12px；
   图标中心恒为 x=32，与顶栏折叠按钮中心严格对齐。 */
.sidebar.collapsed { flex: 0 0 64px; width: 64px; padding: 14px 12px; }
.sidebar.collapsed .nav-group-title,
.sidebar.collapsed .tab-label,
.sidebar.collapsed .tab-index { display: none; }
/* 折叠态：去掉缩进引导线（无空间且失去意义） */
.sidebar.collapsed .tab::before { display: none; }
.sidebar.collapsed .sidebar .tab,
.sidebar.collapsed .tab {
  justify-content: center;
  padding: 9px 0;
  gap: 0;
}
.sidebar.collapsed .tab-icon { width: 100%; justify-content: center; }
/* 折叠态悬停浮出标签 */
.sidebar.collapsed .tab:hover::after {
  content: attr(data-label);
  position: absolute;
  left: calc(100% + 10px);
  top: 50%;
  transform: translateY(-50%);
  padding: 5px 10px;
  border-radius: var(--radius-sm, 6px);
  background: var(--surface-1, #1a2138);
  color: var(--text, #f7f8f8);
  font-size: 12px;
  white-space: nowrap;
  pointer-events: none;
  box-shadow: var(--shadow, 0 4px 14px rgba(0,0,0,.2));
  z-index: 200;
}
.sidebar.collapsed .tab { position: relative; }

/* 折叠切换按钮 */
.sidebar-collapse-btn { flex-shrink: 0; }
.sidebar-collapse-btn svg { width: 18px; height: 18px; }
.sidebar-collapse-btn.is-collapsed { color: var(--accent); }

/* 品牌区：折叠时隐藏副标题 */
.sidebar.collapsed ~ .content .brand-text { display: none; }

/* 分组导航 */
.nav-group { margin-bottom: 16px; }
/* 分组标题：加大加粗，与组内条目形成清晰的层级差 */
.nav-group-title {
  font-size: 12.5px; font-weight: 700; color: var(--text-2);
  text-transform: uppercase; letter-spacing: .09em;
  padding: 8px 12px 7px;
  display: flex; align-items: center; gap: 8px;
}
/* 组标题下的分隔线，强化分组边界 */
.nav-group-title::after {
  content: '';
  flex: 1;
  height: 1px;
  background: var(--color-hairline, #d3dbe8);
  opacity: .7;
}
.sidebar .tab {
  display: flex; width: 100%; text-align: left;
  /* 左内边距 18px：相对分组标题（12px）缩进 6px，层级关系一目了然 */
  padding: 8px 12px 8px 18px; border-radius: var(--radius-sm);
  color: var(--text-2); border: none; background: none;
  font-weight: 500; gap: 9px; margin: 2px 0; font-size: 13.5px;
  cursor: pointer; align-items: center;
  position: relative;
}
/* 缩进引导线：把条目与所属分组在视觉上串联起来 */
.sidebar .tab::before {
  content: '';
  position: absolute;
  left: 8px;
  top: 50%;
  width: 4px;
  height: 1px;
  background: var(--color-hairline, #d3dbe8);
  transition: background-color .15s;
}
.sidebar .tab:hover::before { background: var(--color-hairline-strong, #bcc7da); }
.sidebar .tab.active::before { background: var(--accent); width: 6px; }
.sidebar .tab:hover { background: var(--bg-soft); color: var(--text); }
.sidebar .tab.active { background: var(--accent-soft); color: var(--accent); font-weight: 600; }
.tab-index {
  flex: 0 0 20px; height: 20px; border-radius: 6px;
  display: inline-flex; align-items: center; justify-content: center;
  font-size: 11px; font-weight: 700; font-variant-numeric: tabular-nums;
  background: var(--color-surface-3, #eef1f7); color: var(--text-3);
  border: 1px solid var(--color-hairline, #d3dbe8);
}
.sidebar .tab.active .tab-index { background: var(--accent); color: #fff; border-color: var(--accent); }
.tab-icon { display: inline-flex; flex-shrink: 0; }
.tab-label { flex: 1; min-width: 0; }

.content { flex: 1; max-width: 1200px; margin: 0 auto; padding: 22px; }

.footbar {
  display: flex; align-items: center; justify-content: space-between;
  gap: 12px; flex-wrap: wrap;
  /* 左内边距对齐侧栏内容线（10px），整体比原来左移 */
  padding: 10px 24px 10px 10px;
  background: var(--surface);
  border-top: 1px solid var(--border); font-size: 12px; color: var(--text-3);
}
/* 页脚品牌文字约束在侧栏宽度内：最右字符不越过侧栏右边界，超出则省略号 */
.footbar-brand {
  flex: 0 0 var(--sidebar-w, 210px);
  max-width: var(--sidebar-w, 210px);
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  padding-left: 2px;
}

@media (max-width: 768px) {
  .icon-btn.menu-toggle { display: inline-flex; }
  /* 移动端用抽屉，侧栏折叠按钮无意义 */
  .sidebar-collapse-btn { display: none; }
  .sidebar {
    flex: 0 0 260px; width: 260px;
    position: fixed; top: 0; bottom: 0; left: 0;
    transform: translateX(-100%); z-index: 100;
    transition: transform .22s ease;
    box-shadow: var(--drawer-shadow);
  }
  /* 移动端抽屉始终展开显示文字（覆盖折叠态的隐藏规则） */
  .sidebar.collapsed { flex: 0 0 260px; width: 260px; padding: 14px 10px; }
  .sidebar.collapsed .nav-group-title,
  .sidebar.collapsed .tab-label,
  .sidebar.collapsed .tab-index { display: revert; }
  .sidebar.collapsed .sidebar .tab,
  .sidebar.collapsed .tab { justify-content: flex-start; padding: 8px 12px; gap: 9px; }
  .sidebar.collapsed .tab:hover::after { content: none; }
  .sidebar.open { transform: translateX(0); }
  .sidebar-mask {
    position: fixed; inset: 0; z-index: 99;
    background: rgba(0,0,0,.42);
  }
  .content { padding: 14px; }
  .appbar { padding: 8px 12px; }
  .appbar-meta { display: none; }
  /* 移动端顶栏用户名隐藏，仅保留头像，节省横向空间 */
  .user-name { display: none; }
  /* 移动端副标题隐藏，节省顶部空间 */
  .brand .sub { display: none; }
}

/* ===========================================================================
 * 液态玻璃 (Liquid Glass) — 暗色模式覆盖
 * =========================================================================== */
[data-theme="dark"] .appbar {
  background: var(--glass-topbar-bg, rgba(16, 18, 28, 0.62));
  border-bottom-color: var(--glass-topbar-border, rgba(255, 255, 255, 0.07));
  box-shadow: var(--glass-topbar-shadow, 0 6px 28px rgba(0, 0, 0, 0.5));
}

[data-theme="dark"] .sidebar {
  background:
    linear-gradient(180deg, var(--glass-sidebar-tint, rgba(94,106,210,0.07)) 0%, transparent 220px),
    var(--glass-sidebar-bg, rgba(12, 14, 22, 0.86));
  border-right-color: var(--glass-sidebar-border, rgba(255, 255, 255, 0.06));
  box-shadow: var(--glass-sidebar-shadow, 10px 0 32px rgba(0, 0, 0, 0.45));
}

/* ===========================================================================
 * 液态玻璃 — 卡片增强
 * =========================================================================== */
.glass-card {
  /* 以统一层级的一层 L1 为基准色，保留少量透明度维持玻璃质感 */
  background: var(--glass-card-bg, rgba(255, 255, 255, 0.92));
  backdrop-filter: blur(var(--glass-blur, 20px)) saturate(150%);
  -webkit-backdrop-filter: blur(var(--glass-blur, 20px)) saturate(150%);
  border: 1px solid var(--glass-card-border, var(--border-primary, #d3dbe8));
  box-shadow: var(--glass-shadow-light, 0 8px 32px rgba(31, 38, 135, 0.12));
  position: relative;
  overflow: hidden;
}

.glass-card::before {
  content: '';
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 1px;
  background: var(--glass-highlight-light, linear-gradient(135deg, rgba(255, 255, 255, 0.4) 0%, rgba(255, 255, 255, 0.05) 100%));
  pointer-events: none;
}

[data-theme="dark"] .glass-card {
  background: var(--glass-card-bg, rgba(19, 22, 32, 0.92));
  border-color: var(--glass-card-border, var(--border-primary, #2b3243));
  box-shadow: var(--glass-shadow-dark, 0 8px 32px rgba(0, 0, 0, 0.4));
}

[data-theme="dark"] .glass-card::before {
  background: var(--glass-highlight-dark, linear-gradient(135deg, rgba(255, 255, 255, 0.12) 0%, rgba(255, 255, 255, 0.04) 100%));
}

/* Reduced motion */
@media (prefers-reduced-motion: reduce) {
  .appbar,
  .sidebar,
  .glass-card {
    backdrop-filter: none !important;
    -webkit-backdrop-filter: none !important;
  }
}


</style>
