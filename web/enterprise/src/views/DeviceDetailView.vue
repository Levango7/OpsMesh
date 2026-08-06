<template>
  <div class="device-detail">
    <!-- 顶部操作栏 -->
    <div class="detail-topbar">
      <div class="topbar-left">
        <button class="xs outline" @click="goBack">
          <span style="display:inline-flex;align-items:center;gap:4px;"><Icon name="arrow-left" :size="14" /> {{ $t('common.back') || '返回' }}</span>
        </button>
        <h2>{{ $t('device_detail.title') }}</h2>
        <code v-if="metrics">{{ metrics.deviceID }}</code>
      </div>
      <div class="topbar-right">
        <span class="refresh-tag" :class="{ active: autoRefresh }">
          <span class="dot" />
          {{ autoRefresh ? $t('device_detail.auto_refresh_on') : $t('device_detail.auto_refresh_off') }}
        </span>
        <span v-if="metrics" class="muted collected">
          {{ $t('device_detail.collected_at') }}: {{ fmtTime(metrics.collectedAt) }}
        </span>
        <button class="xs outline" :disabled="store.metricsLoading" @click="refresh">
          <span style="display:inline-flex;align-items:center;gap:4px;">
            <Icon name="refresh" :size="14" />
            {{ $t('common.refresh') }}
          </span>
        </button>
      </div>
    </div>

    <!-- 错误提示 -->
    <div v-if="store.metricsError" class="poll-err"><Icon name="warning" :size="14" /> {{ store.metricsError }}</div>

    <!-- 加载中 / 空态 -->
    <div v-if="store.metricsLoading && !metrics" class="muted loading-block">
      {{ $t('common.loading') }}
    </div>
    <div v-else-if="!metrics" class="muted loading-block">
      {{ $t('common.no_data') }}
    </div>

    <!-- 指标内容 -->
    <template v-if="metrics">
      <!-- 基本信息 -->
      <MetricsCard :title="$t('device_detail.basic_info')" icon="device" accent="--indigo">
        <div class="info-grid">
          <div class="info-item">
            <span class="info-label">{{ $t('device_detail.hostname') }}</span>
            <span class="info-value">{{ metrics.hostname || '—' }}</span>
          </div>
          <div class="info-item">
            <span class="info-label">{{ $t('device_detail.device_id') }}</span>
            <span class="info-value"><code>{{ metrics.deviceID }}</code></span>
          </div>
          <div class="info-item">
            <span class="info-label">{{ $t('device_detail.segment') }}</span>
            <span class="info-value">{{ segment || '—' }}</span>
          </div>
          <div class="info-item">
            <span class="info-label">{{ $t('device_detail.ip') }}</span>
            <span class="info-value">{{ primaryIP || '—' }}</span>
          </div>
          <div class="info-item">
            <span class="info-label">{{ $t('device_detail.os') }}</span>
            <span class="info-value">{{ osText }}</span>
          </div>
          <div class="info-item">
            <span class="info-label">{{ $t('device_detail.kernel') }}</span>
            <span class="info-value">{{ metrics.kernel || '—' }}</span>
          </div>
          <div class="info-item">
            <span class="info-label">{{ $t('device_detail.arch') }}</span>
            <span class="info-value">{{ metrics.arch || '—' }}</span>
          </div>
          <div class="info-item">
            <span class="info-label">{{ $t('device_detail.uptime') }}</span>
            <span class="info-value">{{ fmtUptime(metrics.uptime) }}</span>
          </div>
        </div>
      </MetricsCard>

      <!-- CPU + 内存（左右两栏） -->
      <div class="row">
        <div class="col col-half">
          <MetricsCard :title="$t('device_detail.cpu')" icon="ops" accent="--sky">
            <div class="cpu-wrap">
              <ProgressRing
                :value="cpuUsage"
                :size="130"
                :stroke-width="11"
                :label="$t('device_detail.usage')"
              />
              <div class="cpu-meta">
                <div class="meta-row">
                  <span class="meta-label">{{ $t('device_detail.cpu_model') }}</span>
                  <span class="meta-value">{{ metrics.cpu?.model || '—' }}</span>
                </div>
                <div class="meta-row">
                  <span class="meta-label">{{ $t('device_detail.cpu_cores') }}</span>
                  <span class="meta-value">{{ metrics.cpu?.cores ?? '—' }}</span>
                </div>
                <div class="meta-row">
                  <span class="meta-label">{{ $t('device_detail.usage') }}</span>
                  <span class="meta-value">{{ cpuUsage.toFixed(1) }}%</span>
                </div>
              </div>
            </div>
          </MetricsCard>
        </div>
        <div class="col col-half">
          <MetricsCard :title="$t('device_detail.memory')" icon="cmdb" accent="--teal">
            <div class="mem-wrap">
              <div class="mem-bar-row">
                <div class="mem-bar-track">
                  <div class="mem-bar-fill" :style="{ width: memUsage + '%', background: memBarColor }" />
                </div>
                <span class="mem-bar-pct">{{ memUsage.toFixed(1) }}%</span>
              </div>
              <div class="mem-stats">
                <div class="mem-stat">
                  <span class="mem-stat-label">{{ $t('device_detail.mem_total') }}</span>
                  <span class="mem-stat-val">{{ fmtBytes(metrics.memory?.total) }}</span>
                </div>
                <div class="mem-stat">
                  <span class="mem-stat-label">{{ $t('device_detail.mem_used') }}</span>
                  <span class="mem-stat-val">{{ fmtBytes(metrics.memory?.used) }}</span>
                </div>
                <div class="mem-stat">
                  <span class="mem-stat-label">{{ $t('device_detail.mem_available') }}</span>
                  <span class="mem-stat-val">{{ fmtBytes(metrics.memory?.available) }}</span>
                </div>
              </div>
            </div>
          </MetricsCard>
        </div>
      </div>

      <!-- 磁盘监控 -->
      <MetricsCard :title="$t('device_detail.disks')" icon="deploy" accent="--amber">
        <div v-if="!disks.length" class="muted">{{ $t('common.no_data') }}</div>
        <div v-else class="disk-list">
          <div v-for="d in disks" :key="d.mount" class="disk-row">
            <div class="disk-head">
              <code class="disk-mount">{{ d.mount }}</code>
              <span class="disk-type">{{ d.type || '—' }}</span>
              <span class="disk-usage">{{ (d.usage || 0).toFixed(1) }}%</span>
            </div>
            <div class="disk-bar-track">
              <div class="disk-bar-fill" :style="{ width: (d.usage || 0) + '%', background: barColor(d.usage) }" />
            </div>
            <div class="disk-foot">
              <span>{{ $t('device_detail.disk_used') }}: {{ fmtBytes(d.used) }} / {{ fmtBytes(d.total) }}</span>
              <span class="muted">{{ $t('device_detail.disk_free') }}: {{ fmtBytes(d.free) }}</span>
            </div>
          </div>
        </div>
      </MetricsCard>

      <!-- 网络监控 -->
      <MetricsCard :title="$t('device_detail.network')" icon="flow" accent="--sky">
        <DataTable
          :columns="netCols"
          :rows="network"
          row-key="name"
          empty-text="无网卡"
        >
          <template #cell-status="{ value }">
            <StatusBadge :status="netStatus(value)" :text="value || '—'" />
          </template>
          <template #cell-rxBytes="{ value }">{{ fmtBytes(value) }}</template>
          <template #cell-txBytes="{ value }">{{ fmtBytes(value) }}</template>
          <template #cell-speed="{ value }">{{ value != null ? value + ' Mbps' : '—' }}</template>
        </DataTable>
      </MetricsCard>

      <!-- 服务状态 + 进程信息（左右两栏） -->
      <div class="row">
        <div class="col col-half">
          <MetricsCard :title="$t('device_detail.services')" icon="task" accent="--indigo">
            <div v-if="!services.length" class="muted">{{ $t('common.no_data') }}</div>
            <div v-else class="svc-list">
              <div v-for="s in services" :key="s.name" class="svc-row">
                <span class="svc-name">{{ s.name }}</span>
                <StatusBadge :status="svcStatus(s)" :text="s.status || '—'" />
                <span class="svc-enabled" :class="{ on: s.enabled, off: !s.enabled }">
                  {{ s.enabled ? $t('device_detail.enabled') : $t('device_detail.disabled') }}
                </span>
              </div>
            </div>
          </MetricsCard>
        </div>
        <div class="col col-half">
          <MetricsCard :title="$t('device_detail.process_info')" icon="logs" accent="--green">
            <div class="proc-wrap">
              <div class="proc-big">{{ metrics.processCount ?? '—' }}</div>
              <div class="proc-label">{{ $t('device_detail.process_count') }}</div>
            </div>
          </MetricsCard>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup>
// 设备详情页 — 监控指标仪表盘
// 路由：/devices/:id
// 自动每 30 秒刷新一次指标
import { computed, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useDeviceStore } from '@/stores/device'
import MetricsCard from '@/components/MetricsCard.vue'
import ProgressRing from '@/components/ProgressRing.vue'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import Icon from '@/components/Icon.vue'

const route = useRoute()
const router = useRouter()
const store = useDeviceStore()

// 自动刷新间隔（毫秒）
const REFRESH_INTERVAL = 30000
const autoRefresh = true
let timer = null

// 当前设备 ID（路由参数）
const deviceId = computed(() => route.params.id)

// 监控指标数据
const metrics = computed(() => store.metrics)

// 网段：从设备列表中查找匹配 deviceID 的设备所在网段
const segment = computed(() => {
  const dev = store.flat.find((d) => d.deviceID === deviceId.value)
  return dev?.segment || ''
})

// 主 IP：取第一个网卡的 IP
const primaryIP = computed(() => {
  const nets = metrics.value?.network || []
  return nets[0]?.ip || ''
})

// 操作系统文本：os + osVersion
const osText = computed(() => {
  const m = metrics.value
  if (!m) return '—'
  return [m.os, m.osVersion].filter(Boolean).join(' ') || '—'
})

// CPU 使用率（容错）
const cpuUsage = computed(() => metrics.value?.cpu?.usage || 0)

// 内存使用率（容错）
const memUsage = computed(() => metrics.value?.memory?.usage || 0)

// 内存进度条颜色（按阈值）
const memBarColor = computed(() => barColor(memUsage.value))

// 磁盘列表
const disks = computed(() => metrics.value?.disks || [])
// 网卡列表
const network = computed(() => metrics.value?.network || [])
// 服务列表
const services = computed(() => metrics.value?.services || [])

// 网络表格列定义
const netCols = [
  { key: 'name', title: '网卡' },
  { key: 'ip', title: 'IP' },
  { key: 'mac', title: 'MAC' },
  { key: 'status', title: '状态', slot: 'cell-status' },
  { key: 'speed', title: '速率', slot: 'cell-speed' },
  { key: 'rxBytes', title: '接收', slot: 'cell-rxBytes' },
  { key: 'txBytes', title: '发送', slot: 'cell-txBytes' }
]

// === 工具函数 ===

// 字节数格式化：B/KB/MB/GB/TB
function fmtBytes(n) {
  if (n == null || isNaN(n)) return '—'
  if (n === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  const i = Math.floor(Math.log(Math.abs(n)) / Math.log(1024))
  const idx = Math.min(i, units.length - 1)
  const v = n / Math.pow(1024, idx)
  return v.toFixed(idx === 0 ? 0 : 1) + ' ' + units[idx]
}

// 运行时长格式化：X天X小时X分钟（输入秒）
function fmtUptime(sec) {
  if (sec == null || isNaN(sec)) return '—'
  const s = Math.max(0, Math.floor(sec))
  const days = Math.floor(s / 86400)
  const hours = Math.floor((s % 86400) / 3600)
  const mins = Math.floor((s % 3600) / 60)
  const parts = []
  if (days > 0) parts.push(days + '天')
  if (hours > 0 || days > 0) parts.push(hours + '小时')
  parts.push(mins + '分钟')
  return parts.join('')
}

// 时间格式化
function fmtTime(s) {
  if (!s) return '—'
  const d = new Date(s)
  return isNaN(d.getTime()) ? s : d.toLocaleString('zh-CN', { hour12: false })
}

// 进度条颜色：按阈值返回 CSS 变量
function barColor(pct) {
  if (pct >= 85) return 'var(--fail)'
  if (pct >= 60) return 'var(--warn)'
  return 'var(--ok)'
}

// 网卡状态映射到 StatusBadge 状态
function netStatus(s) {
  if (!s) return 'info'
  const v = String(s).toLowerCase()
  if (v === 'up' || v === 'active' || v === 'ok' || v === 'success') return 'ok'
  if (v === 'down' || v === 'failed' || v === 'error') return 'fail'
  if (v === 'warning' || v === 'warn') return 'warn'
  return 'info'
}

// 服务状态映射到 StatusBadge 状态
function svcStatus(s) {
  if (!s) return 'info'
  const v = String(s.status).toLowerCase()
  if (v === 'running' || v === 'active' || v === 'ok' || v === 'success') return 'ok'
  if (v === 'failed' || v === 'error' || v === 'stopped') return 'fail'
  if (v === 'warning' || v === 'warn' || v === 'pending') return 'warn'
  return 'info'
}

// 返回上一页
function goBack() {
  router.push({ name: 'devices' })
}

// 立即刷新
async function refresh() {
  if (deviceId.value) await store.fetchMetrics(deviceId.value)
}

onMounted(async () => {
  // 确保设备列表已加载（用于查找网段）
  if (!store.total) await store.fetchDevices()
  if (deviceId.value) await store.fetchMetrics(deviceId.value)
  // 启动自动刷新
  timer = setInterval(() => {
    if (deviceId.value && !document.hidden) store.fetchMetrics(deviceId.value)
  }, REFRESH_INTERVAL)
})

onUnmounted(() => {
  if (timer) clearInterval(timer)
  store.clearMetrics()
})
</script>

<style scoped>
.device-detail { display: block; }

/* 顶部操作栏 */
.detail-topbar {
  display: flex; align-items: center; justify-content: space-between;
  gap: 12px; flex-wrap: wrap; margin-bottom: 14px;
}
.topbar-left { display: flex; align-items: center; gap: 10px; }
.topbar-left h2 { margin: 0; }
.topbar-right { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.refresh-tag {
  display: inline-flex; align-items: center; gap: 5px;
  font-size: 12px; color: var(--text-3);
  background: var(--surface-3); border: 1px solid var(--border);
  padding: 3px 10px; border-radius: 999px;
}
.refresh-tag .dot {
  width: 7px; height: 7px; border-radius: 50%;
  background: var(--text-3);
}
.refresh-tag.active { color: var(--ok); }
.refresh-tag.active .dot {
  background: var(--ok);
  animation: pulse 1.6s infinite;
}
@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: .35; }
}
.collected { font-size: 12px; }

.poll-err {
  padding: 8px 12px; margin: 6px 0 14px; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}
.loading-block { padding: 40px 0; text-align: center; }

/* 两栏布局 */
.row { display: flex; gap: 16px; flex-wrap: wrap; align-items: stretch; }
.col-half { flex: 1; min-width: 320px; }

/* 基本信息网格 */
.info-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 12px 18px;
}
.info-item { display: flex; flex-direction: column; gap: 3px; }
.info-label { font-size: 11.5px; color: var(--text-3); }
.info-value { font-size: 13.5px; color: var(--text); font-weight: 500; }

/* CPU 卡片 */
.cpu-wrap { display: flex; align-items: center; gap: 22px; flex-wrap: wrap; }
.cpu-meta { flex: 1; min-width: 180px; display: flex; flex-direction: column; gap: 8px; }
.meta-row { display: flex; justify-content: space-between; gap: 10px; font-size: 13px; }
.meta-label { color: var(--text-3); }
.meta-value { color: var(--text); font-weight: 500; text-align: right; }

/* 内存卡片 */
.mem-wrap { display: flex; flex-direction: column; gap: 14px; }
.mem-bar-row { display: flex; align-items: center; gap: 10px; }
.mem-bar-track {
  flex: 1; height: 10px; background: var(--surface-3);
  border-radius: 999px; overflow: hidden;
}
.mem-bar-fill {
  height: 100%; border-radius: 999px;
  transition: width .35s ease, background .2s ease;
}
.mem-bar-pct { font-size: 13px; font-weight: 600; color: var(--text); min-width: 48px; text-align: right; }
.mem-stats { display: grid; grid-template-columns: repeat(3, 1fr); gap: 10px; }
.mem-stat { display: flex; flex-direction: column; gap: 2px; }
.mem-stat-label { font-size: 11.5px; color: var(--text-3); }
.mem-stat-val { font-size: 14px; color: var(--text); font-weight: 600; font-variant-numeric: tabular-nums; }

/* 磁盘列表 */
.disk-list { display: flex; flex-direction: column; gap: 14px; }
.disk-row {
  background: var(--surface-3); border: 1px solid var(--border);
  border-radius: var(--radius-sm); padding: 10px 12px;
}
.disk-head { display: flex; align-items: center; gap: 10px; margin-bottom: 6px; }
.disk-mount { flex: 1; }
.disk-type { font-size: 11.5px; color: var(--text-3); }
.disk-usage { font-size: 13px; font-weight: 600; color: var(--text); font-variant-numeric: tabular-nums; }
.disk-bar-track {
  height: 8px; background: var(--surface); border-radius: 999px; overflow: hidden;
  border: 1px solid var(--border);
}
.disk-bar-fill {
  height: 100%; border-radius: 999px;
  transition: width .35s ease, background .2s ease;
}
.disk-foot {
  display: flex; justify-content: space-between; gap: 10px;
  font-size: 12px; color: var(--text-2); margin-top: 6px;
}

/* 服务列表 */
.svc-list { display: flex; flex-direction: column; gap: 6px; }
.svc-row {
  display: flex; align-items: center; gap: 10px;
  padding: 7px 10px; border-radius: var(--radius-sm);
  background: var(--surface-3); border: 1px solid var(--border);
}
.svc-name { flex: 1; font-size: 13px; color: var(--text); font-weight: 500; }
.svc-enabled { font-size: 11.5px; }
.svc-enabled.on { color: var(--ok); }
.svc-enabled.off { color: var(--text-3); }

/* 进程信息 */
.proc-wrap { display: flex; flex-direction: column; align-items: center; gap: 6px; padding: 10px 0; }
.proc-big { font-size: 36px; font-weight: 700; color: var(--text); line-height: 1; font-variant-numeric: tabular-nums; }
.proc-label { font-size: 12.5px; color: var(--text-3); }

/* 响应式 */
@media (max-width: 768px) {
  .col-half { min-width: 100%; }
  .mem-stats { grid-template-columns: 1fr; }
}
</style>