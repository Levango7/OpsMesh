<template>
  <PageShell :title="$t('gitops.title')" :desc="$t('gitops.subtitle')" testid="gitops" :error="loadErr">
    <!-- 统计卡片 -->
    <div v-if="loading" class="route-skeleton stats-skeleton" data-testid="gitops-stats-skeleton">
      <div class="skeleton-card stats-skeleton-card" v-for="i in 5" :key="i" />
    </div>
    <div v-else class="stats-grid" data-testid="gitops-stats">
      <div class="stat-card" data-testid="stat-total-apps">
        <div class="stat-icon" style="color: var(--indigo);"><Icon name="deploy" :size="22" /></div>
        <div class="stat-body">
          <div class="stat-val">{{ totalApps }}</div>
          <div class="stat-label">{{ $t('gitops.stats_total_apps') }}</div>
        </div>
      </div>
      <div class="stat-card" data-testid="stat-healthy">
        <div class="stat-icon" style="color: var(--teal);"><Icon name="success" :size="22" /></div>
        <div class="stat-body">
          <div class="stat-val">{{ healthyCount }}</div>
          <div class="stat-label">{{ $t('gitops.stats_healthy') }}</div>
        </div>
      </div>
      <div class="stat-card" data-testid="stat-degraded">
        <div class="stat-icon" style="color: var(--rose);"><Icon name="alerts" :size="22" /></div>
        <div class="stat-body">
          <div class="stat-val">{{ degradedCount }}</div>
          <div class="stat-label">{{ $t('gitops.stats_degraded') }}</div>
        </div>
      </div>
      <div class="stat-card" data-testid="stat-out-of-sync">
        <div class="stat-icon" style="color: var(--amber);"><Icon name="flow" :size="22" /></div>
        <div class="stat-body">
          <div class="stat-val">{{ outOfSyncCount }}</div>
          <div class="stat-label">{{ $t('gitops.stats_out_of_sync') }}</div>
        </div>
      </div>
      <div class="stat-card" data-testid="stat-clusters">
        <div class="stat-icon" style="color: var(--sky);"><Icon name="device" :size="22" /></div>
        <div class="stat-body">
          <div class="stat-val">{{ clusterCount }}</div>
          <div class="stat-label">{{ $t('gitops.stats_clusters') }}</div>
        </div>
      </div>
    </div>

    <!-- 应用状态分布 -->
    <div class="card">
      <h3>{{ $t('gitops.app_status_distribution') }}</h3>
      <div class="chart-placeholder" v-if="!loading">
        <div class="dist-row">
          <span class="dist-label">{{ $t('gitops.status_synced') }}</span>
          <div class="dist-bar" :style="syncedBarStyle"></div>
          <span class="dist-val">{{ syncedCount }}</span>
        </div>
        <div class="dist-row">
          <span class="dist-label">{{ $t('gitops.status_outofsync') }}</span>
          <div class="dist-bar" :style="outOfSyncBarStyle"></div>
          <span class="dist-val">{{ outOfSyncCount }}</span>
        </div>
        <div class="dist-row">
          <span class="dist-label">{{ $t('gitops.status_unknown') }}</span>
          <div class="dist-bar" :style="unknownBarStyle"></div>
          <span class="dist-val">{{ unknownCount }}</span>
        </div>
      </div>
    </div>

    <!-- 健康状态分布 -->
    <div class="card">
      <h3>{{ $t('gitops.health_distribution') }}</h3>
      <div class="chart-placeholder" v-if="!loading">
        <div class="dist-row">
          <span class="dist-label">{{ $t('flux.health_healthy') }}</span>
          <div class="dist-bar" :style="healthyBarStyle"></div>
          <span class="dist-val">{{ healthyCount }}</span>
        </div>
        <div class="dist-row">
          <span class="dist-label">{{ $t('flux.health_degraded') }}</span>
          <div class="dist-bar" :style="degradedBarStyle"></div>
          <span class="dist-val">{{ degradedCount }}</span>
        </div>
        <div class="dist-row">
          <span class="dist-label">{{ $t('flux.health_missing') }}</span>
          <div class="dist-bar" :style="missingBarStyle"></div>
          <span class="dist-val">{{ missingCount }}</span>
        </div>
      </div>
    </div>

    <!-- 最近同步 -->
    <div class="card">
      <h3>{{ $t('gitops.recent_syncs') }}</h3>
      <div v-if="recentSyncs.length === 0" class="muted">{{ $t('gitops.no_syncs') }}</div>
      <table class="data-table" v-else>
        <thead>
          <tr>
            <th>{{ $t('gitops.col_app') }}</th>
            <th>{{ $t('gitops.col_type') }}</th>
            <th>{{ $t('gitops.col_status') }}</th>
            <th>{{ $t('gitops.col_time') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(s, i) in recentSyncs" :key="i">
            <td>{{ s.name }}</td>
            <td>{{ $t('flux.type_' + s.type) }}</td>
            <td><span class="tag" :class="'st-' + s.status">{{ $t('flux.status_' + s.status) }}</span></td>
            <td>{{ fmtTime(s.updatedAt) }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 快捷入口 -->
    <div class="card quick-links">
      <h3>{{ $t('gitops.quick_links') }}</h3>
      <div class="link-grid">
        <a class="link-item" :to="{ name: 'argocd' }">
          <Icon name="deploy" :size="20" />
          <span>{{ $t('nav.argocd') }}</span>
        </a>
        <a class="link-item" :to="{ name: 'flux' }">
          <Icon name="flow" :size="20" />
          <span>{{ $t('nav.flux') }}</span>
        </a>
        <a class="link-item" :to="{ name: 'k8s-clusters' }">
          <Icon name="device" :size="20" />
          <span>{{ $t('nav.k8s_clusters') }}</span>
        </a>
      </div>
    </div>
  </PageShell>
</template>

<script setup>
import { reactive, ref, computed, onMounted } from 'vue'
import PageShell from '@/components/PageShell.vue'
import { listApps } from '@/api/argocd'
import { listFluxApps } from '@/api/flux'
import { t } from '@/i18n'
import Icon from '@/components/Icon.vue'

const loading = ref(false)
const loadErr = ref('')
const apps = reactive([])
const clusterCount = ref(0)

const syncedCount = ref(0)
const outOfSyncCount = ref(0)
const unknownCount = ref(0)
const healthyCount = ref(0)
const degradedCount = ref(0)
const missingCount = ref(0)
const recentSyncs = reactive([])

const totalApps = ref(0)

const syncedPct = ref(0)
const outOfSyncPct = ref(0)
const unknownPct = ref(0)
const healthyPct = ref(0)
const degradedPct = ref(0)
const missingPct = ref(0)

const syncedBarStyle = computed(() => ({ width: syncedPct.value + '%' }))
const outOfSyncBarStyle = computed(() => ({ width: outOfSyncPct.value + '%' }))
const unknownBarStyle = computed(() => ({ width: unknownPct.value + '%' }))
const healthyBarStyle = computed(() => ({ width: healthyPct.value + '%' }))
const degradedBarStyle = computed(() => ({ width: degradedPct.value + '%' }))
const missingBarStyle = computed(() => ({ width: missingPct.value + '%' }))

function fmtTime(ts) {
  if (!ts) return '-'
  return new Date(ts).toLocaleString()
}

async function load() {
  loading.value = true
  loadErr.value = ''
  try {
    const [argoResp, fluxResp] = await Promise.all([listApps(), listFluxApps()])
    const argoApps = argoResp.apps || []
    const fluxApps = fluxResp.apps || []
    apps.splice(0, apps.length, ...argoApps, ...fluxApps)

    totalApps.value = apps.length

    syncedCount.value = 0
    outOfSyncCount.value = 0
    unknownCount.value = 0
    healthyCount.value = 0
    degradedCount.value = 0
    missingCount.value = 0

    recentSyncs.splice(0, recentSyncs.length)
    apps.forEach((a) => {
      if (a.status === 'synced') syncedCount.value++
      else if (a.status === 'outofsync') outOfSyncCount.value++
      else unknownCount.value++

      if (a.healthStatus === 'healthy') healthyCount.value++
      else if (a.healthStatus === 'degraded') degradedCount.value++
      else if (a.healthStatus === 'missing') missingCount.value++

      recentSyncs.push({
        name: a.name,
        type: a.type || 'kustomization',
        status: a.status,
        updatedAt: a.updatedAt,
      })
    })

    // 最近同步按时间降序，取前 10
    recentSyncs.sort((a, b) => new Date(b.updatedAt || 0) - new Date(a.updatedAt || 0))
    recentSyncs.splice(10)

    // 百分比
    const total = totalApps.value || 1
    syncedPct.value = Math.round((syncedCount.value / total) * 100)
    outOfSyncPct.value = Math.round((outOfSyncCount.value / total) * 100)
    unknownPct.value = Math.round((unknownCount.value / total) * 100)
    healthyPct.value = Math.round((healthyCount.value / total) * 100)
    degradedPct.value = Math.round((degradedCount.value / total) * 100)
    missingPct.value = Math.round((missingCount.value / total) * 100)
  } catch (e) {
    // 此前静默吞错（错误仅由全局 request.js toast 提示）——统一骨架批次补上页面内可见错误
    loadErr.value = e.j?.error || t('common.load_failed')
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  load()
})
</script>

<style scoped>
.stats-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 14px;
  margin-bottom: 18px;
}
.stat-card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 16px;
  display: flex;
  align-items: center;
  gap: 12px;
}
.stat-icon {
  font-size: 22px;
  width: 40px;
  height: 40px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--radius-sm);
  background: var(--muted);
}
.stat-body {
  flex: 1;
}
.stat-val {
  font-size: 20px;
  font-weight: 600;
  color: var(--text);
}
.stat-label {
  font-size: 12px;
  color: var(--text-2);
}
.card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 16px;
  margin-bottom: 14px;
}
.chart-placeholder {
  margin-top: 10px;
}
.dist-row {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 6px 0;
}
.dist-label {
  width: 110px;
  font-size: 12px;
  color: var(--text-2);
}
.dist-bar {
  flex: 1;
  height: 8px;
  background: var(--muted);
  border-radius: 4px;
  overflow: hidden;
}
.dist-bar > div {
  height: 100%;
  background: var(--accent);
}
.dist-val {
  width: 40px;
  font-size: 12px;
  color: var(--text-2);
  font-family: monospace;
  text-align: right;
}
.data-table {
  width: 100%;
  border-collapse: collapse;
  margin-top: 10px;
  font-size: 13px;
}
.data-table th,
.data-table td {
  border: 1px solid var(--border);
  padding: 6px 8px;
  text-align: left;
}
.data-table th {
  background: var(--muted);
  color: var(--text-2);
  font-size: 12px;
}
.data-table tr:hover {
  background: var(--muted-soft);
}
.quick-links {
  padding: 12px;
}
.quick-links h3 {
  margin: 0 0 10px 0;
  font-size: 14px;
  color: var(--text-2);
}
.link-grid {
  display: flex;
  gap: 12px;
}
.link-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 16px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  color: var(--text);
  text-decoration: none;
  font-size: 13px;
}
.link-item:hover {
  background: var(--muted-soft);
  border-color: var(--accent);
}
.link-item Icon {
  color: var(--accent);
}
</style>