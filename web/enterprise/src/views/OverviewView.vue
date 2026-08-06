<template>
  <div>
    <h2>{{ $t('overview.title') }}</h2>
    <p class="muted">{{ $t('overview.subtitle') }}</p>

    <!-- 统计卡片 -->
    <div class="stats-grid">
      <div class="stat-card">
        <div class="stat-icon" style="color: var(--indigo);"><Icon name="device" :size="22" /></div>
        <div class="stat-body">
          <div class="stat-val">{{ deviceStore.total }}</div>
          <div class="stat-label">{{ $t('overview.stats_devices') }}</div>
        </div>
      </div>
      <div class="stat-card">
        <div class="stat-icon" style="color: var(--teal);"><Icon name="success" :size="22" /></div>
        <div class="stat-body">
          <div class="stat-val">{{ deviceStore.managed }}</div>
          <div class="stat-label">{{ $t('overview.stats_managed') }}</div>
        </div>
      </div>
      <div class="stat-card">
        <div class="stat-icon" style="color: var(--rose);"><Icon name="alerts" :size="22" /></div>
        <div class="stat-body">
          <div class="stat-val">{{ alertStore.list.length }}</div>
          <div class="stat-label">{{ $t('overview.stats_alerts') }}</div>
        </div>
      </div>
      <div class="stat-card">
        <div class="stat-icon" style="color: var(--sky);"><Icon name="flow" :size="22" /></div>
        <div class="stat-body">
          <div class="stat-val">{{ workflowCount }}</div>
          <div class="stat-label">{{ $t('overview.stats_workflows') }}</div>
        </div>
      </div>
    </div>

    <!-- 运维能力 6 方向 -->
    <div class="card">
      <h3>{{ $t('overview.ops_capabilities') }}</h3>
      <p class="muted cap-sub">{{ $t('overview.ops_capabilities_sub') }}</p>
      <div class="cap-grid">
        <router-link
          v-for="cap in capabilities"
          :key="cap.key"
          :to="cap.to"
          class="cap-item"
        >
          <span class="cap-icon" :style="{ color: cap.color, background: cap.bg }">
            <Icon :name="cap.icon" :size="22" />
          </span>
          <div class="cap-body">
            <div class="cap-title">
              <span class="cap-no">{{ cap.no }}</span>
              {{ $t(cap.label) }}
            </div>
            <div class="cap-desc">{{ $t(cap.desc) }}</div>
          </div>
        </router-link>
      </div>
    </div>

    <!-- 快速入口 -->
    <div class="card">
      <h3>{{ $t('overview.quick_entry') }}</h3>
      <div class="quick-grid">
        <router-link v-for="q in quickEntries" :key="q.to" :to="q.to" class="quick-item">
          <span class="quick-icon" :style="{ color: q.color }"><Icon :name="q.icon" :size="20" /></span>
          <span class="quick-label">{{ $t(q.label) }}</span>
        </router-link>
      </div>
    </div>

    <!-- 近期告警 -->
    <div class="card">
      <h3>{{ $t('overview.recent_alerts') }}</h3>
      <div v-if="!alertStore.list.length" class="muted">{{ $t('common.no_data') }}</div>
      <div v-else class="alert-list">
        <div v-for="a in recentAlerts" :key="a.id || a.fingerprint" class="alert-row">
          <StatusBadge :status="a.severity || a.status" :text="a.severity || a.status" />
          <span class="alert-name">{{ a.name || a.alertname || a.message || '—' }}</span>
          <span class="muted alert-time">{{ fmtTime(a.startsAt || a.createdAt || a.created_at) }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
// 总览页 — 聚合各 store 概况，提供快速入口与近期告警
import { computed, onMounted } from 'vue'
import { useDeviceStore } from '@/stores/device'
import { useAlertStore } from '@/stores/alert'
import { useWorkflowStore } from '@/stores/workflow'
import Icon from '@/components/Icon.vue'
import StatusBadge from '@/components/StatusBadge.vue'

const deviceStore = useDeviceStore()
const alertStore = useAlertStore()
const workflowStore = useWorkflowStore()

// 作业编排数量（容错：store 可能未加载）
const workflowCount = computed(() => {
  const list = workflowStore.list
  if (Array.isArray(list)) return list.length
  if (list && typeof list === 'object') return Object.keys(list).length
  return 0
})

// 快速入口
const quickEntries = [
  { to: '/devices', icon: 'device', color: 'var(--indigo)', label: 'nav.devices' },
  { to: '/tasks', icon: 'task', color: 'var(--teal)', label: 'nav.tasks' },
  { to: '/alerts', icon: 'alerts', color: 'var(--rose)', label: 'nav.alerts' },
  { to: '/cmdb', icon: 'cmdb', color: 'var(--amber)', label: 'nav.cmdb' },
  { to: '/workflows', icon: 'flow', color: 'var(--sky)', label: 'nav.workflows' },
  { to: '/deploys', icon: 'deploy', color: 'var(--sky)', label: 'nav.deploys' },
  { to: '/logs', icon: 'logs', color: 'var(--green)', label: 'nav.logs' },
  { to: '/users', icon: 'users', color: 'var(--indigo)', label: 'nav.users' }
]

// 运维能力 6 方向：设备纳管 / 任务执行 / 配置下发 / 服务管理 / 文件分发 / 状态监控
const capabilities = [
  {
    no: '01', key: 'enroll',
    icon: 'device', color: 'var(--indigo)', bg: 'var(--indigo-soft)',
    to: '/devices',
    label: 'overview.cap_enroll', desc: 'overview.cap_enroll_desc'
  },
  {
    no: '02', key: 'task',
    icon: 'task', color: 'var(--teal)', bg: 'var(--teal-soft)',
    to: '/tasks',
    label: 'overview.cap_task', desc: 'overview.cap_task_desc'
  },
  {
    no: '03', key: 'config',
    icon: 'cmdb', color: 'var(--amber)', bg: 'var(--amber-soft)',
    to: '/cmdb',
    label: 'overview.cap_config', desc: 'overview.cap_config_desc'
  },
  {
    no: '04', key: 'service',
    icon: 'ops', color: 'var(--green)', bg: 'var(--green-soft)',
    to: '/devices',
    label: 'overview.cap_service', desc: 'overview.cap_service_desc'
  },
  {
    no: '05', key: 'file',
    icon: 'deploy', color: 'var(--sky)', bg: 'var(--sky-soft)',
    to: '/deploys',
    label: 'overview.cap_file', desc: 'overview.cap_file_desc'
  },
  {
    no: '06', key: 'monitor',
    icon: 'alerts', color: 'var(--rose)', bg: 'var(--rose-soft)',
    to: '/alerts',
    label: 'overview.cap_monitor', desc: 'overview.cap_monitor_desc'
  }
]

// 近期告警（最多 6 条）
const recentAlerts = computed(() => (alertStore.list || []).slice(0, 6))

function fmtTime(s) {
  if (!s) return ''
  const d = new Date(s)
  return isNaN(d.getTime()) ? s : d.toLocaleString('zh-CN', { hour12: false })
}

onMounted(() => {
  if (!deviceStore.total) deviceStore.fetchDevices()
  if (!alertStore.list.length) alertStore.fetchAlerts()
})
</script>

<style scoped>
.stats-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 14px;
  margin: 16px 0;
}
.stat-card {
  display: flex; align-items: center; gap: 14px;
  background: var(--surface); border: 1px solid var(--border);
  border-radius: var(--radius); padding: 16px 18px;
  box-shadow: var(--shadow);
}
.stat-icon {
  width: 44px; height: 44px; border-radius: 12px;
  background: var(--surface-3);
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
.stat-val { font-size: 22px; font-weight: 700; color: var(--text); line-height: 1.1; }
.stat-label { font-size: 12.5px; color: var(--text-3); margin-top: 2px; }

.quick-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  gap: 10px;
  margin-top: 8px;
}
.quick-item {
  display: flex; align-items: center; gap: 10px;
  padding: 10px 12px; border-radius: var(--radius-sm);
  background: var(--surface-3); border: 1px solid var(--border);
  color: var(--text-2); font-size: 13px; font-weight: 500;
  transition: .15s;
}
.quick-item:hover {
  background: var(--accent-soft); color: var(--accent);
  border-color: var(--accent); text-decoration: none;
}
.quick-icon { display: inline-flex; }

.alert-list { display: flex; flex-direction: column; gap: 8px; margin-top: 6px; }
.alert-row {
  display: flex; align-items: center; gap: 10px;
  padding: 8px 10px; border-radius: var(--radius-sm);
  background: var(--surface-3);
}
.alert-name { flex: 1; font-size: 13px; color: var(--text); }
.alert-time { font-size: 12px; }

/* 运维能力 6 方向 */
.cap-sub { margin: 4px 0 10px; font-size: 12.5px; }
.cap-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 12px;
  margin-top: 6px;
}
.cap-item {
  display: flex; align-items: flex-start; gap: 12px;
  padding: 14px 16px; border-radius: var(--radius);
  background: var(--surface-3); border: 1px solid var(--border);
  transition: .15s; text-decoration: none;
}
.cap-item:hover {
  border-color: var(--accent);
  box-shadow: 0 4px 14px rgba(91,94,240,.12);
  transform: translateY(-1px);
  text-decoration: none;
}
.cap-icon {
  width: 44px; height: 44px; border-radius: 12px;
  display: inline-flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
.cap-body { flex: 1; min-width: 0; }
.cap-title {
  font-size: 14px; font-weight: 600; color: var(--text);
  display: flex; align-items: center; gap: 6px;
}
.cap-no {
  font-size: 11px; font-weight: 700; color: var(--text-3);
  background: var(--surface); border: 1px solid var(--border);
  padding: 1px 6px; border-radius: 5px;
  font-variant-numeric: tabular-nums;
}
.cap-desc { font-size: 12.5px; color: var(--text-3); margin-top: 4px; line-height: 1.5; }
</style>