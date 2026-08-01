<template>
  <div class="app">
    <!-- 顶栏 -->
    <header class="appbar">
      <div class="brand">
        <div class="logo">OM</div>
        <div>
          <h1>OpsMesh 企业版</h1>
          <div class="sub">多租户运维编排控制台 · Vue 3</div>
        </div>
      </div>
      <div class="appbar-meta">
        <span class="chip">设备 <b>{{ deviceStore.total }}</b></span>
        <span class="chip">纳管 <b>{{ deviceStore.managed }}</b></span>
        <span class="chip">告警 <b>{{ alertStore.list.length }}</b></span>
      </div>
    </header>

    <div class="layout">
      <!-- 侧栏导航 -->
      <aside class="sidebar">
        <div
          v-for="item in navItems"
          :key="item.name"
          class="tab"
          :class="{ active: $route.name === item.name }"
          @click="$router.push('/' + item.name)"
        >
          <span class="tdot" :style="{ background: item.color }"></span>
          {{ item.title }}
        </div>
        <div class="apilist">
          <div>API: <code>/api/v1</code></div>
          <div>Base: <code>/enterprise/</code></div>
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
      <span>OpsMesh Enterprise · Vue 3 + Vite + Pinia</span>
      <span class="muted">© 2026 OpsMesh</span>
    </footer>
  </div>
</template>

<script setup>
import { onMounted, onUnmounted } from 'vue'
import { useDeviceStore } from '@/stores/device'
import { useAlertStore } from '@/stores/alert'

const deviceStore = useDeviceStore()
const alertStore = useAlertStore()

const navItems = [
  { name: 'devices', title: '设备纳管', color: 'var(--indigo)' },
  { name: 'tasks', title: '任务下发', color: 'var(--teal)' },
  { name: 'alerts', title: '监控告警', color: 'var(--rose)' },
  { name: 'cmdb', title: '配置项 CMDB', color: 'var(--amber)' },
  { name: 'workflows', title: '作业编排', color: 'var(--violet)' },
  { name: 'deploys', title: '部署中心', color: 'var(--sky)' },
  { name: 'logs', title: '日志检索', color: 'var(--green)' }
]

// 主轮询：受页面可见性控制
let pollTimers = []
function startPolls() {
  if (pollTimers.length) return
  pollTimers = [
    setInterval(() => deviceStore.fetchDevices(), 5000),
    setInterval(() => alertStore.fetchAlerts(), 10000)
  ]
}
function stopPolls() { pollTimers.forEach(clearInterval); pollTimers = [] }

function onVisibility() {
  if (document.hidden) stopPolls()
  else { startPolls(); deviceStore.fetchDevices(); alertStore.fetchAlerts() }
}

onMounted(() => {
  deviceStore.fetchDevices()
  alertStore.fetchAlerts()
  document.addEventListener('visibilitychange', onVisibility)
  startPolls()
})
onUnmounted(() => {
  stopPolls()
  document.removeEventListener('visibilitychange', onVisibility)
})
</script>

<style scoped>
.app { display: flex; flex-direction: column; min-height: 100vh; }
.appbar {
  position: sticky; top: 0; z-index: 30;
  display: flex; align-items: center; justify-content: space-between;
  padding: 12px 24px;
  background: rgba(247,248,253,.86);
  backdrop-filter: blur(10px);
  border-bottom: 1px solid var(--border);
}
.brand { display: flex; align-items: center; gap: 12px; }
.brand .logo {
  width: 38px; height: 38px; border-radius: 10px;
  background: linear-gradient(135deg, var(--indigo), #8b5cf6);
  display: flex; align-items: center; justify-content: center;
  color: #fff; font-weight: 700; font-size: 14px;
  box-shadow: 0 4px 14px rgba(99,102,241,.4);
}
.brand h1 { line-height: 1.1; font-size: 16px; }
.brand .sub { font-size: 12px; color: var(--text-3); }
.appbar-meta { display: flex; gap: 8px; flex-wrap: wrap; }
.chip {
  font-size: 12px; color: var(--text-2);
  background: var(--surface-3); border: 1px solid var(--border);
  padding: 3px 10px; border-radius: 999px; font-variant-numeric: tabular-nums;
}
.chip b { color: var(--text); font-weight: 600; }

.layout { display: flex; flex: 1; min-height: 0; }
.sidebar {
  flex: 0 0 210px; width: 210px;
  background: var(--surface); border-right: 1px solid var(--border);
  padding: 16px 12px; overflow: auto;
}
.sidebar .tab {
  display: flex; width: 100%; text-align: left;
  padding: 9px 12px; border-radius: 9px;
  color: var(--text-2); border: none; background: none;
  font-weight: 500; gap: 9px; margin: 2px 0; font-size: 13.5px;
  cursor: pointer; align-items: center;
}
.sidebar .tab:hover { background: var(--bg-soft); color: var(--text); }
.sidebar .tab.active { background: var(--accent-soft); color: var(--accent); font-weight: 600; }
.sidebar .tab .tdot { width: 7px; height: 7px; border-radius: 50%; flex: 0 0 auto; }
.apilist {
  margin-top: 22px; padding-top: 16px;
  border-top: 1px dashed var(--border);
  font-size: 12.5px; color: var(--text-3); line-height: 1.8;
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
  .appbar { padding: 10px 14px; }
}
</style>