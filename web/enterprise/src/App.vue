<template>
  <!-- 未登录：只渲染路由（登录/注册页自己负责全屏布局） -->
  <router-view v-if="!authStore.isLoggedIn" />

  <!-- 已登录：主布局（顶栏 + 侧栏 + 内容 + 底栏） -->
  <div v-else class="app">
    <!-- 顶栏 -->
    <header class="appbar">
      <div class="brand">
        <div class="logo"><Icon name="brand" :size="20" /></div>
        <div>
          <h1>{{ $t('app.title') }}</h1>
          <div class="sub">{{ $t('app.subtitle') }}</div>
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
        >
          <Icon :name="themeStore.isDark ? 'theme-light' : 'theme-dark'" :size="18" />
        </button>

        <!-- 语言切换 -->
        <button class="icon-btn" @click="toggleLang" :title="$t('topbar.lang')">
          <Icon name="lang" :size="18" />
          <span class="lang-label">{{ currentLang === 'zh' ? '中' : 'EN' }}</span>
        </button>

        <!-- 用户信息 -->
        <div class="user-info">
          <span class="user-avatar"><Icon name="users" :size="16" /></span>
          <span class="user-name" data-testid="topbar-username">{{ authStore.user?.username || '—' }}</span>
        </div>

        <!-- 退出 -->
        <button class="icon-btn danger" @click="onLogout" :title="$t('topbar.logout')" data-testid="topbar-logout">
          <Icon name="logout" :size="18" />
        </button>
      </div>
    </header>

    <div class="layout">
      <!-- 侧栏导航：分组 + 编号功能入口 -->
      <aside class="sidebar">
        <div v-for="grp in navView" :key="grp.group" class="nav-group">
          <div class="nav-group-title">{{ $t(grp.labelKey) }}</div>
          <div
            v-for="item in grp.items"
            :key="item.name"
            class="tab"
            :class="{ active: $route.name === item.name }"
            @click="$router.push('/' + item.name)"
          >
            <span class="tab-index">{{ item.index }}</span>
            <span class="tab-icon"><Icon :name="item.icon" :size="16" /></span>
            <span class="tab-label">{{ $t(item.labelKey) }}</span>
          </div>
        </div>

        <div class="apilist">
          <div>{{ $t('common.api_base') }}</div>
          <div>{{ $t('common.web_base') }}</div>
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
      <span>{{ $t('app.footer') }}</span>
      <span class="muted">{{ $t('app.copyright') }}</span>
    </footer>
  </div>
</template>

<script setup>
// 应用根组件 — 顶栏 + 分组侧栏 + 内容 + 底栏
// 已登录显示主布局；未登录仅渲染路由（登录/注册页全屏）
import { onMounted, onUnmounted, computed, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useDeviceStore } from '@/stores/device'
import { useAlertStore } from '@/stores/alert'
import { useAuthStore } from '@/stores/auth'
import { useThemeStore } from '@/stores/theme'
import { currentLang, setLang } from '@/i18n'
import { createSSEClient } from '@/api/sse'
import Icon from '@/components/Icon.vue'

const router = useRouter()
const deviceStore = useDeviceStore()
const alertStore = useAlertStore()
const authStore = useAuthStore()
const themeStore = useThemeStore()

// 分组导航：每组 { group, labelKey, items[{ name, icon, labelKey, required }] }
// required 为该功能入口对应的后端权限（与 requireProd 闸同源）；
// 为空表示无需权限门槛（如概览页，登录即可见）。侧栏将按当前用户权限过滤。
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
      { name: 'alerts', icon: 'alerts', labelKey: 'nav.alerts', required: 'alert:read' },
      { name: 'os-optimize', icon: 'task', labelKey: 'nav.osopt', required: 'task:read' },
      { name: 'middleware', icon: 'deploy', labelKey: 'nav.mwdep', required: 'deploy:read' },
      { name: 'k8s', icon: 'device', labelKey: 'nav.k8s', required: 'device:read' }
    ]
  },
  {
    group: 'assets', labelKey: 'nav.assets',
    items: [{ name: 'cmdb', icon: 'cmdb', labelKey: 'nav.cmdb', required: 'cmdb:read' }]
  },
  {
    group: 'delivery', labelKey: 'nav.delivery',
    items: [
      { name: 'workflows', icon: 'flow', labelKey: 'nav.workflows', required: 'workflow:read' },
      { name: 'deploys', icon: 'deploy', labelKey: 'nav.deploys', required: 'deploy:read' }
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
      { name: 'secrets', icon: 'task', labelKey: 'nav.secrets', required: 'role:read' }
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
.app { display: flex; flex-direction: column; min-height: 100vh; }
.appbar {
  position: sticky; top: 0; z-index: 30;
  display: flex; align-items: center; justify-content: space-between;
  padding: 10px 20px;
  background: var(--appbar-bg);
  backdrop-filter: blur(10px);
  border-bottom: 1px solid var(--border);
}
.brand { display: flex; align-items: center; gap: 12px; }
.brand .logo {
  width: 38px; height: 38px; border-radius: 10px;
  background: linear-gradient(135deg, var(--indigo), var(--sky));
  display: flex; align-items: center; justify-content: center;
  color: #fff;
  box-shadow: 0 4px 14px rgba(99,102,241,.4);
}
.brand h1 { line-height: 1.1; font-size: 16px; }
.brand .sub { font-size: 12px; color: var(--text-3); }

.appbar-right { display: flex; align-items: center; gap: 8px; }
.appbar-meta { display: flex; gap: 6px; flex-wrap: wrap; margin-right: 4px; }
.chip {
  font-size: 12px; color: var(--text-2);
  background: var(--surface-3); border: 1px solid var(--border);
  padding: 3px 10px; border-radius: 999px; font-variant-numeric: tabular-nums;
}
.chip b { color: var(--text); font-weight: 600; }

/* 顶栏图标按钮 */
.icon-btn {
  display: inline-flex; align-items: center; gap: 4px;
  height: 34px; padding: 0 10px;
  background: var(--surface-3); border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  color: var(--text-2); cursor: pointer; transition: .15s;
}
.icon-btn:hover { background: var(--bg-soft); color: var(--text); }
.icon-btn.danger:hover { color: var(--fail); border-color: var(--fail); }
.lang-label { font-size: 12px; font-weight: 600; }

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
  flex: 0 0 210px; width: 210px;
  background: var(--surface); border-right: 1px solid var(--border);
  padding: 14px 10px; overflow: auto;
}

/* 分组导航 */
.nav-group { margin-bottom: 14px; }
.nav-group-title {
  font-size: 11px; font-weight: 600; color: var(--text-3);
  text-transform: uppercase; letter-spacing: .06em;
  padding: 6px 12px 4px;
}
.sidebar .tab {
  display: flex; width: 100%; text-align: left;
  padding: 8px 12px; border-radius: var(--radius-sm);
  color: var(--text-2); border: none; background: none;
  font-weight: 500; gap: 9px; margin: 2px 0; font-size: 13.5px;
  cursor: pointer; align-items: center;
}
.sidebar .tab:hover { background: var(--bg-soft); color: var(--text); }
.sidebar .tab.active { background: var(--accent-soft); color: var(--accent); font-weight: 600; }
.tab-index {
  flex: 0 0 20px; height: 20px; border-radius: 6px;
  display: inline-flex; align-items: center; justify-content: center;
  font-size: 11px; font-weight: 700; font-variant-numeric: tabular-nums;
  background: var(--surface-3); color: var(--text-3);
  border: 1px solid var(--border);
}
.sidebar .tab.active .tab-index { background: var(--accent); color: #fff; border-color: var(--accent); }
.tab-icon { display: inline-flex; flex-shrink: 0; }
.tab-label { flex: 1; min-width: 0; }

.apilist {
  margin-top: 18px; padding-top: 14px;
  border-top: 1px dashed var(--border);
  font-size: 12px; color: var(--text-3); line-height: 1.8;
  padding-left: 12px;
}

.content { flex: 1; max-width: 1200px; margin: 0 auto; padding: 22px; }

.footbar {
  display: flex; align-items: center; justify-content: space-between;
  gap: 12px; flex-wrap: wrap;
  padding: 10px 24px; background: var(--surface);
  border-top: 1px solid var(--border); font-size: 12px; color: var(--text-3);
}

@media (max-width: 768px) {
  .sidebar { flex: 0 0 100%; position: fixed; transform: translateX(-100%); z-index: 100; }
  .content { padding: 14px; }
  .appbar { padding: 8px 12px; }
  .appbar-meta { display: none; }
}
</style>
