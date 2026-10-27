<template>
  <PageShell
    :title="metrics?.deviceID || deviceId || $t('device_detail.title')"
    testid="device-detail"
    :error="store.metricsError"
  >
    <!-- 头部操作区：返回 + 自动刷新指示 + 采集时间 + 刷新（原顶部操作栏整体迁入 #actions） -->
    <template #actions>
      <Button variant="ghost" size="sm" @click="goBack">
        <span style="display:inline-flex;align-items:center;gap:4px;"><Icon name="arrow-left" :size="14" /> {{ $t('common.back') }}</span>
      </Button>
      <span class="refresh-tag" :class="{ active: autoRefresh }">
        <span class="dot" />
        {{ autoRefresh ? $t('device_detail.auto_refresh_on') : $t('device_detail.auto_refresh_off') }}
      </span>
      <span v-if="metrics" class="muted collected">
        {{ $t('device_detail.collected_at') }}: {{ fmtTime(metrics.collectedAt, '—') }}
      </span>
      <Button variant="ghost" size="sm" :disabled="store.metricsLoading" @click="refresh">
        <span style="display:inline-flex;align-items:center;gap:4px;">
          <Icon name="refresh" :size="14" />
          {{ $t('common.refresh') }}
        </span>
      </Button>
    </template>

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
            <!-- Inode -->
            <div v-if="inodeMap[d.mount]" class="disk-foot">
              <span class="muted">{{ $t('device_detail.inode_used') }}: {{ inodeMap[d.mount].used }} / {{ inodeMap[d.mount].total }} ({{ inodeMap[d.mount].usage.toFixed(1) }}%)</span>
            </div>
            <!-- IOPS -->
            <div v-if="iopsMap[d.mount]" class="disk-foot">
              <span class="muted">{{ $t('device_detail.iops_read') }}: {{ iopsMap[d.mount].readIOPS.toFixed(0) }} / {{ $t('device_detail.iops_write') }}: {{ iopsMap[d.mount].writeIOPS.toFixed(0) }}</span>
              <span class="muted">{{ $t('device_detail.iops_read_lat') }}: {{ iopsMap[d.mount].readLatency.toFixed(1) }}ms / {{ $t('device_detail.iops_write_lat') }}: {{ iopsMap[d.mount].writeLatency.toFixed(1) }}ms</span>
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
          :empty-text="$t('device_detail.no_nic')"
        >
          <template #cell-status="{ value }">
            <StatusBadge :status="netStatus(value)" :text="value || '—'" />
          </template>
          <template #cell-rxBytes="{ value }">{{ fmtBytes(value) }}</template>
          <template #cell-txBytes="{ value }">{{ fmtBytes(value) }}</template>
          <template #cell-speed="{ value }">{{ value != null ? value + ' Mbps' : '—' }}</template>
        </DataTable>
      </MetricsCard>

      <!-- GPU 监控 -->
      <MetricsCard v-if="gpus.length" :title="$t('device_detail.gpu')" icon="deploy" accent="--purple">
        <div v-if="gpus.length === 0" class="muted">{{ $t('device_detail.no_gpu') }}</div>
        <div v-else class="gpu-list">
          <div v-for="g in gpus" :key="g.index" class="gpu-row">
            <div class="gpu-head">
              <code class="gpu-name">{{ g.name }}</code>
              <span class="gpu-uuid">{{ g.uuid }}</span>
            </div>
            <div class="gpu-meta-row">
              <span class="gpu-meta">{{ $t('device_detail.gpu_usage') }}: {{ g.usage.toFixed(1) }}%</span>
              <span class="gpu-meta">{{ $t('device_detail.gpu_mem') }}: {{ fmtBytes(g.memUsed * 1024 * 1024) }} / {{ fmtBytes(g.memTotal * 1024 * 1024) }} ({{ g.memUsage.toFixed(1) }}%)</span>
              <span class="gpu-meta">{{ $t('device_detail.gpu_temp') }}: {{ g.temp }}°C</span>
              <span class="gpu-meta">{{ $t('device_detail.gpu_power') }}: {{ g.powerDraw.toFixed(1) }}W / {{ g.powerLimit.toFixed(1) }}W</span>
              <span class="gpu-meta">{{ $t('device_detail.gpu_driver') }}: {{ g.driver }}</span>
            </div>
            <div class="gpu-bar-track">
              <div class="gpu-bar-fill" :style="{ width: g.usage + '%', background: barColor(g.usage) }" />
            </div>
            <div class="gpu-bar-track">
              <div class="gpu-bar-fill" :style="{ width: g.memUsage + '%', background: barColor(g.memUsage) }" />
            </div>
          </div>
        </div>
      </MetricsCard>

      <!-- 环境依赖 -->
      <MetricsCard v-if="envs.length" :title="$t('device_detail.env_deps')" icon="logs" accent="--indigo">
        <div v-if="envs.length === 0" class="muted">{{ $t('device_detail.no_env_deps') }}</div>
        <div v-else class="env-list">
          <div v-for="e in envs" :key="e.name" class="env-row">
            <span class="env-name">{{ e.name }}</span>
            <span class="env-version">{{ e.version }}</span>
          </div>
        </div>
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
        <div class="col col-full">
          <MetricsCard :title="$t('device_detail.process_top')" icon="task" accent="--purple">
            <div v-if="!processes.length" class="muted">{{ $t('common.no_data') }}</div>
            <div v-else class="proc-top-list">
              <div v-for="p in processes" :key="p.pid" class="proc-row">
                <span class="proc-pid">{{ p.pid }}</span>
                <span class="proc-name">{{ p.name || p.cmd }}</span>
                <span class="proc-cpu">{{ p.cpu.toFixed(1) }}%</span>
                <span class="proc-mem">{{ p.mem.toFixed(1) }}%</span>
                <span class="proc-user">{{ p.user || '—' }}</span>
              </div>
            </div>
          </MetricsCard>
        </div>
      </div>
    </template>
  </PageShell>
</template>

<script setup>
import PageShell from '@/components/PageShell.vue'

// 设备详情页 — 监控指标仪表盘
// 路由：/devices/:id
// 自动每 30 秒刷新一次指标
import { computed, onMounted, onUnmounted, defineAsyncComponent } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useDeviceStore } from '@/stores/device'
import { t } from '@/i18n'
import MetricsCard from '@/components/MetricsCard.vue'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import Icon from '@/components/Icon.vue'
import { Button } from '@/components/ui'
import { fmtTime } from '@/composables/useFormatTime'
// ProgressRing 仅设备详情页使用，改为异步加载减小首屏 components chunk
const ProgressRing = defineAsyncComponent(() => import('@/components/ProgressRing.vue'))

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
// GPU 列表
const gpus = computed(() => metrics.value?.gpus || [])
// 环境依赖列表
const envs = computed(() => metrics.value?.envs || [])

// Inode 映射（按 mount 索引）
const inodeMap = computed(() => {
  const map = {}
  const inodes = metrics.value?.inodes || []
  inodes.forEach(d => { map[d.mount] = d })
  return map
})

// IOPS 映射（按 mount 索引）
const iopsMap = computed(() => {
  const map = {}
  const iops = metrics.value?.iops || []
  iops.forEach(d => { map[d.mount] = d })
  return map
})

// 进程 Top 列表
const processes = computed(() => metrics.value?.processes || [])

// 网络表格列定义
const netCols = [
  { key: 'name', title: t('device_detail.col_nic') },
  { key: 'ip', title: 'IP' },
  { key: 'mac', title: 'MAC' },
  { key: 'status', title: t('device_detail.col_status'), slot: 'cell-status' },
  { key: 'speed', title: t('device_detail.col_speed'), slot: 'cell-speed' },
  { key: 'rxBytes', title: t('device_detail.col_rx'), slot: 'cell-rxBytes' },
  { key: 'txBytes', title: t('device_detail.col_tx'), slot: 'cell-txBytes' }
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
  if (days > 0) parts.push(days + t('device_detail.uptime_day'))
  if (hours > 0 || days > 0) parts.push(hours + t('device_detail.uptime_hour'))
  parts.push(mins + t('device_detail.uptime_minute'))
  return parts.join('')
}

// 进度条颜色：按阈值返回 CSS 变量
function barColor(pct) {
  if (pct >= 85) return 'var(--color-error)'
  if (pct >= 60) return 'var(--color-warning)'
  return 'var(--color-success)'
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
  if (v === 'warning' || v === 'warn') return 'warn'
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
.refresh-tag {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1, 4px);
  font-size: var(--font-size-xs, 11px);
  color: var(--color-ink-tertiary, #62666d);
  background: var(--color-surface-2, #141516);
  border: 1px solid var(--color-hairline, #23252a);
  padding: var(--space-1, 4px) var(--space-2, 8px);
  border-radius: var(--radius-full, 9999px);
}
.refresh-tag .dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--color-ink-tertiary, #62666d);
}
.refresh-tag.active {
  color: var(--color-success, #27a644);
}
.refresh-tag.active .dot {
  background: var(--color-success, #27a644);
  animation: pulse 1.6s infinite;
}
@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: .35; }
}
.collected {
  font-size: var(--font-size-xs, 11px);
}

.loading-block {
  padding: var(--space-6, 24px) 0;
  text-align: center;
}

/* 两栏布局 */
.row {
  display: flex;
  gap: var(--space-3, 12px);
  flex-wrap: wrap;
  align-items: stretch;
}
.col-half {
  flex: 1;
  min-width: 320px;
}

/* 基本信息网格 */
.info-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: var(--space-2, 8px) var(--space-3, 12px);
}
.info-item {
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.info-label {
  font-size: var(--font-size-xs, 11px);
  color: var(--color-ink-tertiary, #62666d);
}
.info-value {
  font-size: var(--font-size-sm, 13px);
  color: var(--color-ink, #f7f8f8);
  font-weight: 500;
}

/* CPU 卡片 */
.cpu-wrap {
  display: flex;
  align-items: center;
  gap: var(--space-3, 12px);
  flex-wrap: wrap;
}
.cpu-meta {
  flex: 1;
  min-width: 180px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.meta-row {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  font-size: 13px;
}
.meta-label {
  color: var(--color-ink-tertiary, #62666d);
}
.meta-value {
  color: var(--color-ink, #f7f8f8);
  font-weight: 500;
  text-align: right;
}

/* 内存卡片 */
.mem-wrap {
  display: flex;
  flex-direction: column;
  gap: var(--space-2, 8px);
}
.mem-bar-row {
  display: flex;
  align-items: center;
  gap: var(--space-2, 8px);
}
.mem-bar-track {
  flex: 1;
  height: 10px;
  background: var(--color-surface-2, #141516);
  border-radius: var(--radius-full, 9999px);
  overflow: hidden;
  border: 1px solid var(--color-hairline, #23252a);
}
.mem-bar-fill {
  height: 100%;
  border-radius: 999px;
  transition: width 0.35s ease, background 0.2s ease;
}
.mem-bar-pct {
  font-size: 13px;
  font-weight: 600;
  color: var(--color-ink, #f7f8f8);
  min-width: 48px;
  text-align: right;
}
.mem-stats {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 10px;
}
.mem-stat {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.mem-stat-label {
  font-size: var(--font-size-xs, 11px);
  color: var(--color-ink-tertiary, #62666d);
}
.mem-stat-val {
  font-size: 14px;
  color: var(--color-ink, #f7f8f8);
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

/* 磁盘列表 */
.disk-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-2, 8px);
}
.disk-row {
  background: var(--color-surface-2, #141516);
  border: 1px solid var(--color-hairline, #23252a);
  border-radius: var(--radius-sm, 6px);
  padding: var(--space-2, 8px) var(--space-3, 12px);
}
.disk-head {
  display: flex;
  align-items: center;
  gap: var(--space-2, 8px);
  margin-bottom: var(--space-1, 6px);
}
.disk-mount {
  flex: 1;
}
.disk-type {
  font-size: var(--font-size-xs, 11px);
  color: var(--color-ink-tertiary, #62666d);
}
.disk-usage {
  font-size: 13px;
  font-weight: 600;
  color: var(--color-ink, #f7f8f8);
  font-variant-numeric: tabular-nums;
}
.disk-bar-track {
  height: 8px;
  background: var(--color-surface-1, #0f1011);
  border-radius: 999px;
  overflow: hidden;
  border: 1px solid var(--color-hairline, #23252a);
}
.disk-bar-fill {
  height: 100%;
  border-radius: 999px;
  transition: width 0.35s ease, background 0.2s ease;
}
.disk-foot {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  font-size: 12px;
  color: var(--color-ink-muted, #d0d6e0);
  margin-top: 6px;
}

/* 服务列表 */
.svc-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.svc-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 7px 10px;
  border-radius: var(--radius-sm, 6px);
  background: var(--color-surface-2, #141516);
  border: 1px solid var(--color-hairline, #23252a);
}
.svc-name {
  flex: 1;
  font-size: 13px;
  color: var(--color-ink, #f7f8f8);
  font-weight: 500;
}
.svc-enabled {
  font-size: 11.5px;
}
.svc-enabled.on {
  color: var(--color-success, #27a644);
}
.svc-enabled.off {
  color: var(--color-ink-tertiary, #62666d);
}

/* 进程信息 */
.proc-wrap {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  padding: var(--space-2, 8px) 0;
}
.proc-big {
  font-size: 36px;
  font-weight: 700;
  color: var(--color-ink, #f7f8f8);
  line-height: 1;
  font-variant-numeric: tabular-nums;
}
.proc-label {
  font-size: var(--font-size-sm, 12px);
  color: var(--color-ink-tertiary, #62666d);
}

/* 进程 Top 列表 */
.proc-top-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.proc-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 10px;
  border-radius: var(--radius-sm, 6px);
  background: var(--color-surface-2, #141516);
  border: 1px solid var(--color-hairline, #23252a);
  font-size: 12px;
}
.proc-pid {
  width: 50px;
  color: var(--color-ink-tertiary, #62666d);
  font-family: var(--font-mono, monospace);
}
.proc-name {
  flex: 1;
  color: var(--color-ink, #f7f8f8);
  font-weight: 500;
}
.proc-cpu {
  width: 70px;
  color: var(--color-ink-tertiary, #62666d);
  font-family: var(--font-mono, monospace);
}
.proc-mem {
  width: 70px;
  color: var(--color-ink-tertiary, #62666d);
  font-family: var(--font-mono, monospace);
}
.proc-user {
  width: 100px;
  color: var(--color-ink-tertiary, #62666d);
}

/* GPU 列表 */
.gpu-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-2, 8px);
}
.gpu-row {
  background: var(--color-surface-2, #141516);
  border: 1px solid var(--color-hairline, #23252a);
  border-radius: var(--radius-sm, 6px);
  padding: var(--space-2, 8px) var(--space-3, 12px);
}
.gpu-head {
  display: flex;
  justify-content: space-between;
  margin-bottom: var(--space-2, 8px);
}
.gpu-name {
  font-size: 13px;
  font-weight: 600;
  color: var(--color-ink, #f7f8f8);
}
.gpu-uuid {
  font-size: 11px;
  color: var(--color-ink-tertiary, #62666d);
  font-family: var(--font-mono, monospace);
}
.gpu-meta-row {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2, 8px);
  margin-bottom: var(--space-2, 8px);
}
.gpu-meta {
  font-size: 12px;
  color: var(--color-ink-tertiary, #62666d);
}
.gpu-bar-track {
  height: 8px;
  background: var(--color-surface-1, #0f1011);
  border-radius: 999px;
  overflow: hidden;
  border: 1px solid var(--color-hairline, #23252a);
  margin-bottom: var(--space-2, 8px);
}
.gpu-bar-fill {
  height: 100%;
  border-radius: 999px;
  transition: width 0.35s ease, background 0.2s ease;
}

/* 环境依赖列表 */
.env-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.env-row {
  background: var(--color-surface-2, #141516);
  border: 1px solid var(--color-hairline, #23252a);
  border-radius: var(--radius-sm, 6px);
  padding: 8px 12px;
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.env-name {
  font-size: 13px;
  color: var(--color-ink, #f7f8f8);
  font-weight: 500;
}
.env-version {
  font-size: 13px;
  color: var(--color-ink-tertiary, #62666d);
  font-family: var(--font-mono, monospace);
}

/* 响应式 */
@media (max-width: 768px) {
  .col-half {
    min-width: 100%;
  }
  .mem-stats {
    grid-template-columns: 1fr;
  }
}
</style>