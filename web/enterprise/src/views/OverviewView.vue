<template>
  <div data-testid="overview-view" class="overview">
    <div class="overview__header">
      <h2 data-testid="overview-title" class="overview__title">{{ $t('overview.title') }}</h2>
      <p class="overview__subtitle">{{ $t('overview.subtitle') }}</p>
    </div>

    <!-- 统计卡片：加载中显示骨架屏 -->
    <div v-if="statsLoading" class="overview__skeleton stats-skeleton" data-testid="overview-stats-skeleton">
      <div class="skeleton-card stats-skeleton-card" v-for="i in 4" :key="i" />
    </div>
    <div v-else class="stats-grid" data-testid="overview-stats">
      <div class="stat-card" data-testid="stat-devices">
        <div class="stat-icon" style="color: var(--color-primary);">
          <Icon name="device" :size="22" />
        </div>
        <div class="stat-body">
          <div class="stat-val" data-testid="stat-devices-value">{{ deviceStore.total }}</div>
          <div class="stat-label">{{ $t('overview.stats_devices') }}</div>
        </div>
      </div>
      <div class="stat-card" data-testid="stat-managed">
        <div class="stat-icon" style="color: color-mix(in srgb, var(--color-primary, #5e6ad2) 60%, var(--color-success, #27a644) 40%);">
          <Icon name="success" :size="22" />
        </div>
        <div class="stat-body">
          <div class="stat-val" data-testid="stat-managed-value">{{ deviceStore.managed }}</div>
          <div class="stat-label">{{ $t('overview.stats_managed') }}</div>
        </div>
      </div>
      <div class="stat-card" data-testid="stat-alerts">
        <div class="stat-icon" style="color: var(--color-error);">
          <Icon name="alerts" :size="22" />
        </div>
        <div class="stat-body">
          <div class="stat-val" data-testid="stat-alerts-value">{{ alertStore.list.length }}</div>
          <div class="stat-label">{{ $t('overview.stats_alerts') }}</div>
        </div>
      </div>
      <div class="stat-card" data-testid="stat-workflows">
        <div class="stat-icon" style="color: var(--color-link, #0070f3);">
          <Icon name="flow" :size="22" />
        </div>
        <div class="stat-body">
          <div class="stat-val" data-testid="stat-workflows-value">{{ workflowCount }}</div>
          <div class="stat-label">{{ $t('overview.stats_workflows') }}</div>
        </div>
      </div>
    </div>

    <!-- 运维能力 6 方向 -->
    <div class="card glass-card">
      <h3 class="card__title">{{ $t('overview.ops_capabilities') }}</h3>
      <p class="card__subtitle">{{ $t('overview.ops_capabilities_sub') }}</p>
      <div class="cap-grid" data-testid="overview-capabilities">
        <router-link
          v-for="cap in capabilities"
          :key="cap.key"
          :to="cap.to"
          class="cap-item"
          :data-testid="'overview-cap-' + cap.key"
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
    <div class="card glass-card">
      <h3 class="card__title">{{ $t('overview.quick_entry') }}</h3>
      <div class="quick-grid" data-testid="overview-quick-entries">
        <router-link v-for="q in quickEntries" :key="q.to" :to="q.to" class="quick-item" :data-testid="'overview-quick-' + q.label.split('.').pop()">
          <span class="quick-icon" :style="{ color: q.color }"><Icon :name="q.icon" :size="20" /></span>
          <span class="quick-label">{{ $t(q.label) }}</span>
        </router-link>
      </div>
    </div>

    <!-- 近期告警 -->
    <div class="card glass-card">
      <h3 class="card__title">{{ $t('overview.recent_alerts') }}</h3>
      <div v-if="!alertStore.list.length" class="muted" data-testid="overview-alerts-empty">{{ $t('common.no_data') }}</div>
      <div v-else class="alert-list" data-testid="overview-alerts-list">
        <div v-for="a in recentAlerts" :key="a.id || a.fingerprint" class="alert-row" :data-testid="'overview-alert-row-' + (a.id || a.fingerprint || 'unknown')">
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
import { fmtTime } from '@/composables/useFormatTime'

const deviceStore = useDeviceStore()
const alertStore = useAlertStore()
const workflowStore = useWorkflowStore()

// 统计加载态：任一数据源在拉取中即显示骨架屏
const statsLoading = computed(() => deviceStore.loading || alertStore.loading)

// 作业编排数量（容错：store 可能未加载）
const workflowCount = computed(() => {
  const list = workflowStore.list
  if (Array.isArray(list)) return list.length
  if (list && typeof list === 'object') return Object.keys(list).length
  return 0
})

// 快速入口
const quickEntries = [
  { to: '/devices', icon: 'device', color: 'var(--color-primary)', label: 'nav.devices' },
  { to: '/tasks', icon: 'task', color: 'var(--color-success)', label: 'nav.tasks' },
  { to: '/alerts', icon: 'alerts', color: 'var(--color-error)', label: 'nav.alerts' },
  { to: '/cmdb', icon: 'cmdb', color: 'var(--color-warning)', label: 'nav.cmdb' },
  { to: '/workflows', icon: 'flow', color: 'var(--color-link)', label: 'nav.workflows' },
  { to: '/deploys', icon: 'deploy', color: 'var(--color-link)', label: 'nav.deploys' },
  { to: '/logs', icon: 'logs', color: 'var(--color-success)', label: 'nav.logs' },
  { to: '/users', icon: 'users', color: 'var(--color-primary)', label: 'nav.users' }
]

// 运维能力 6 方向：设备纳管 / 任务执行 / 配置下发 / 服务管理 / 文件分发 / 状态监控
const capabilities = [
  {
    no: '01', key: 'enroll',
    icon: 'device', color: 'var(--color-primary)', bg: 'color-mix(in srgb, var(--color-primary, #5e6ad2) 15%, transparent)',
    to: '/devices',
    label: 'overview.cap_enroll', desc: 'overview.cap_enroll_desc'
  },
  {
    no: '02', key: 'task',
    icon: 'task', color: 'var(--color-success)', bg: 'color-mix(in srgb, var(--color-success, #27a644) 15%, transparent)',
    to: '/tasks',
    label: 'overview.cap_task', desc: 'overview.cap_task_desc'
  },
  {
    no: '03', key: 'config',
    icon: 'cmdb', color: 'var(--color-warning)', bg: 'color-mix(in srgb, var(--color-warning, #f5a623) 15%, transparent)',
    to: '/cmdb',
    label: 'overview.cap_config', desc: 'overview.cap_config_desc'
  },
  {
    no: '04', key: 'service',
    icon: 'ops', color: 'var(--color-success)', bg: 'color-mix(in srgb, var(--color-success, #27a644) 15%, transparent)',
    to: '/devices',
    label: 'overview.cap_service', desc: 'overview.cap_service_desc'
  },
  {
    no: '05', key: 'file',
    icon: 'deploy', color: 'var(--color-link)', bg: 'color-mix(in srgb, var(--color-link, #0070f3) 15%, transparent)',
    to: '/deploys',
    label: 'overview.cap_file', desc: 'overview.cap_file_desc'
  },
  {
    no: '06', key: 'monitor',
    icon: 'alerts', color: 'var(--color-error)', bg: 'color-mix(in srgb, var(--color-error, #ef4444) 15%, transparent)',
    to: '/alerts',
    label: 'overview.cap_monitor', desc: 'overview.cap_monitor_desc'
  }
]

// 近期告警（最多 6 条）
const recentAlerts = computed(() => (alertStore.list || []).slice(0, 6))


onMounted(() => {
  if (!deviceStore.total) deviceStore.fetchDevices()
  if (!alertStore.list.length) alertStore.fetchAlerts()
})
</script>

<style scoped>
.overview {
  display: block;
}

.overview__header {
  margin-bottom: var(--space-6, 24px);
}

.overview__title {
  margin: 0;
  font-size: var(--font-size-base, 13px);
  font-weight: 600;
  color: var(--color-ink, #f7f8f8);
}

.overview__subtitle {
  margin: 0 0 var(--space-3, 12px) 0;
  font-size: var(--font-size-sm, 12px);
  color: var(--color-ink-muted, #d0d6e0);
}

/* Stats grid */
.stats-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: var(--space-3, 12px);
  margin: var(--space-4, 16px) 0;
}
.stats-skeleton {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: var(--space-3, 12px);
  margin: var(--space-4, 16px) 0;
}
.stats-skeleton-card { height: 76px; }
.stat-card {
  display: flex;
  align-items: center;
  gap: var(--space-3, 12px);
  background: var(--color-surface-1, #0f1011);
  border: 1px solid var(--color-hairline, #23252a);
  border-radius: var(--radius-md, 8px);
  padding: var(--space-3, 12px);
  box-shadow: var(--shadow-sm, 0 1px 2px rgba(0, 0, 0, 0.3));
}
.stat-icon {
  width: 44px;
  height: 44px;
  border-radius: var(--radius-lg, 12px);
  background: var(--color-surface-2, #141516);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.stat-val {
  font-size: 22px;
  font-weight: 700;
  color: var(--color-ink, #f7f8f8);
  line-height: 1.1;
}
.stat-label {
  font-size: var(--font-size-xs, 11px);
  color: var(--color-ink-tertiary, #62666d);
  margin-top: 2px;
}

/* Quick entries */
.quick-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  gap: var(--space-2, 8px);
  margin-top: var(--space-1, 4px);
}
.quick-item {
  display: flex;
  align-items: center;
  gap: var(--space-2, 8px);
  padding: var(--space-2, 8px) var(--space-3, 12px);
  border-radius: var(--radius-sm, 6px);
  background: var(--color-surface-2, #141516);
  border: 1px solid var(--color-hairline, #23252a);
  color: var(--color-ink-muted, #d0d6e0);
  font-size: 13px;
  font-weight: 500;
  transition: var(--transition-fast, 0.15s);
  text-decoration: none;
}
.quick-item:hover {
  background: color-mix(in srgb, var(--color-primary, #5e6ad2) 15%, transparent);
  color: var(--color-primary, #5e6ad2);
  border-color: var(--color-primary, #5e6ad2);
  text-decoration: none;
}
.quick-icon { display: inline-flex; }

/* Alert list */
.alert-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-2, 8px);
  margin-top: var(--space-1, 6px);
}
.alert-row {
  display: flex;
  align-items: center;
  gap: var(--space-2, 10px);
  padding: var(--space-2, 8px) var(--space-3, 10px);
  border-radius: var(--radius-sm, 6px);
  background: var(--color-surface-2, #141516);
}
.alert-name {
  flex: 1;
  font-size: var(--font-size-sm, 13px);
  color: var(--color-ink, #f7f8f8);
}
.alert-time {
  font-size: var(--font-size-xs, 11px);
  color: var(--color-ink-tertiary, #62666d);
}

/* 运维能力 6 方向 */
.card__subtitle {
  margin: var(--space-1, 4px) 0 var(--space-3, 12px) 0;
  font-size: var(--font-size-xs, 11px);
  color: var(--color-ink-muted, #d0d6e0);
}
.cap-sub {
  margin: var(--space-1, 4px) 0 var(--space-3, 12px) 0;
  font-size: var(--font-size-xs, 11px);
}
.cap-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: var(--space-3, 12px);
  margin-top: var(--space-1, 6px);
}
.cap-item {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3, 12px);
  padding: var(--space-3, 12px) var(--space-4, 16px);
  border-radius: var(--radius-md, 8px);
  background: var(--color-surface-2, #141516);
  border: 1px solid var(--color-hairline, #23252a);
  transition: var(--transition-fast, 0.15s);
  text-decoration: none;
}
.cap-item:hover {
  border-color: var(--color-primary, #5e6ad2);
  box-shadow: var(--shadow-sm, 0 1px 2px rgba(0, 0, 0, 0.3));
  transform: translateY(-1px);
  text-decoration: none;
}
.cap-icon {
  width: 44px;
  height: 44px;
  border-radius: var(--radius-lg, 12px);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.cap-body {
  flex: 1;
  min-width: 0;
}
.cap-title {
  font-size: var(--font-size-sm, 13px);
  font-weight: 600;
  color: var(--color-ink, #f7f8f8);
  display: flex;
  align-items: center;
  gap: var(--space-1, 6px);
}
.cap-no {
  font-size: var(--font-size-xs, 11px);
  font-weight: 700;
  color: var(--color-ink-tertiary, #62666d);
  background: var(--color-surface-1, #0f1011);
  border: 1px solid var(--color-hairline, #23252a);
  padding: 1px 6px;
  border-radius: var(--radius-sm, 6px);
  font-variant-numeric: tabular-nums;
}
.cap-desc {
  font-size: var(--font-size-xs, 11px);
  color: var(--color-ink-tertiary, #62666d);
  margin-top: var(--space-1, 4px);
  line-height: 1.5;
}
</style>