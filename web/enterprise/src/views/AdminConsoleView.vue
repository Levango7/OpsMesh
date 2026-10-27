<template>
  <PageShell :title="$t('admin_console.title')" :desc="$t('admin_console.subtitle')" testid="admin-console" :error="loadError">
    <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
    <div v-else>
      <!-- 顶栏：日志级别切换 + 诊断包下载 -->
      <div class="btnbar">
        <label class="field-inline">
          {{ $t('admin_console.log_level') }}
          <select v-model="logLevel" @change="onLogLevelChange">
            <option value="debug">debug</option>
            <option value="info">info</option>
            <option value="warn">warn</option>
            <option value="error">error</option>
          </select>
        </label>
        <Button variant="ghost" size="sm" @click="window.open(getDiagnosticsUrl(), '_blank')" data-testid="admin-diagnostics-link">
          <Icon name="download" :size="14" /> {{ t('admin_console.diagnostics_download') }}
        </Button>
        <Button variant="ghost" size="sm" @click="fetchAll">
          <Icon name="refresh" :size="14" /> {{ t('common.refresh') }}
        </Button>
      </div>

      <!-- 生效配置 -->
      <div class="card">
        <h3 class="card__title">{{ $t('admin_console.config_title') }}</h3>
        <pre class="code-block">{{ configJson }}</pre>
      </div>

      <!-- store 写入失败 -->
      <div class="card">
        <h3 class="card__title">{{ $t('admin_console.store_failures_title') }}</h3>
        <div v-if="storeFailures.total === 0" class="muted">{{ $t('admin_console.no_failures') }}</div>
        <div v-else>
          <p>{{ $t('admin_console.total_failures') }}: {{ storeFailures.total }}</p>
          <DataTable :columns="failureCols" :rows="storeFailures.topOps || []" :empty-text="$t('admin_console.no_failures')" />
          <pre v-if="storeFailures.recentFailures?.length" class="code-block">{{ JSON.stringify(storeFailures.recentFailures, null, 2) }}</pre>
        </div>
      </div>

      <!-- TD-60 服务路由表 -->
      <div class="card">
        <h3 class="card__title">{{ $t('admin_console.service_routing_title') }}</h3>
        <DataTable :columns="routingCols" :rows="serviceRouting.domains || []" :empty-text="$t('admin_console.no_data')" />
      </div>

      <!-- TD-60 服务流量计数器 -->
      <div class="card">
        <h3 class="card__title">{{ $t('admin_console.service_traffic_title') }}</h3>
        <p class="muted">{{ serviceTraffic.window }}</p>
        <DataTable :columns="trafficCols" :rows="serviceTraffic.domains || []" :empty-text="$t('admin_console.no_data')" />
      </div>
    </div>
  </PageShell>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import PageShell from '@/components/PageShell.vue'
import { getAdminConfig, setLogLevel, getDiagnosticsUrl, getStoreFailures, getServiceRouting, getServiceTraffic } from '@/api/admin'
import DataTable from '@/components/DataTable.vue'
import Icon from '@/components/Icon.vue'
import { Button } from '@/components/ui'
import { t } from '@/i18n'

const loading = ref(false)
const loadError = ref('')
const config = ref({})
const storeFailures = ref({ total: 0, topOps: [], recentFailures: [] })
const serviceRouting = ref({ domains: [] })
const serviceTraffic = ref({ domains: [], window: '' })
const logLevel = ref('info')

const configJson = computed(() => JSON.stringify(config.value, null, 2))
const failureCols = [
  { key: 'op', title: 'Op', width: '200px' },
  { key: 'count', title: 'Count', width: '80px' }
]

const routingCols = [
  { key: 'domain', title: 'Domain', width: '120px' },
  { key: 'healthy', title: 'Healthy', width: '80px' },
  { key: 'status', title: 'Status', width: '100px' },
  { key: 'note', title: 'Note', width: '200px' }
]

const trafficCols = [
  { key: 'domain', title: 'Domain', width: '120px' },
  { key: 'requests', title: 'Requests', width: '100px' },
  { key: 'prefixes', title: 'Prefixes', width: '200px' }
]

async function fetchAll() {
  loading.value = true
  loadError.value = ''
  try {
    const [cfg, sf, sr, st] = await Promise.all([
      getAdminConfig().catch(() => ({})),
      getStoreFailures({ top: 20 }).catch(() => ({ total: 0, topOps: [], recentFailures: [] })),
      getServiceRouting().catch(() => ({ domains: [] })),
      getServiceTraffic().catch(() => ({ domains: [], window: '' }))
    ])
    config.value = cfg || {}
    storeFailures.value = sf || { total: 0, topOps: [], recentFailures: [] }
    serviceRouting.value = sr || { domains: [] }
    serviceTraffic.value = st || { domains: [], window: '' }
  } catch (e) {
    loadError.value = e.j?.error || t('admin_console.load_failed')
  } finally {
    loading.value = false
  }
}

async function onLogLevelChange() {
  try {
    await setLogLevel({ level: logLevel.value })
  } catch {
    // ignore
  }
}

onMounted(() => {
  fetchAll()
})
</script>

<style scoped>
.btnbar { display: flex; gap: 10px; align-items: center; margin-bottom: 16px; flex-wrap: wrap; }
.field-inline { display: inline-flex; align-items: center; gap: 6px; font-size: 13px; }
.field-inline select { padding: 4px 8px; border: 1px solid var(--border); border-radius: var(--radius-sm); }
.card { background: var(--surface, var(--surface-l1), var(--color-surface-1, #ffffff)); border: 1px solid var(--border, var(--border-primary), #d3dbe8); border-radius: var(--radius, var(--radius-md), 8px); padding: 16px 18px; margin-bottom: 16px; }
.card h3,
.card__title { margin: 0 0 12px 0; font-size: 13px; font-weight: 600; color: var(--color-ink-muted, var(--text-secondary), #6b7280); }
.code-block { background: var(--surface-3); color: var(--text); padding: 10px; border-radius: var(--radius-sm); overflow: auto; font-size: 12px; line-height: 1.5; white-space: pre-wrap; word-break: break-all; font-family: var(--font-mono); max-height: 400px; }
.muted { color: var(--text-3); font-size: 12.5px; }
.row-actions { display: inline-flex; gap: 6px; }
</style>
