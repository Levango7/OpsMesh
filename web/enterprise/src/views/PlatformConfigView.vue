<template>
  <div>
    <h2 data-testid="platform-title">{{ $t('platform.title') }}</h2>
    <p class="muted">{{ $t('platform.subtitle') }}</p>

    <!-- Tab 切换 -->
    <div class="tabbar">
      <button
        v-for="tab in tabs"
        :key="tab.key"
        :class="['tab', { active: activeTab === tab.key }]"
        :data-testid="'platform-tab-' + tab.key"
        @click="switchTab(tab.key)"
      >{{ $t(tab.label) }}</button>
    </div>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <!-- ========== Tab 1: 平台配置 ========== -->
    <div v-show="activeTab === 'config'">
      <div class="btnbar">
        <button class="outline" @click="store.fetchConfig()"><Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}</button>
        <button class="primary" @click="openEdit" data-testid="platform-config-edit-btn"><Icon name="edit" :size="14" /> {{ $t('platform.edit') }}</button>
      </div>
      <div v-if="store.loading && !store.config" class="muted">{{ $t('common.loading') }}</div>
      <div v-else-if="store.config" class="card">
        <div class="kv-grid">
          <div class="kv"><span class="k">{{ $t('platform.field_version') }}</span><span class="v"><code>{{ store.config.version || '—' }}</code></span></div>
          <div class="kv"><span class="k">{{ $t('platform.field_buildTime') }}</span><span class="v">{{ store.config.buildTime || '—' }}</span></div>
          <div class="kv"><span class="k">{{ $t('platform.field_goVersion') }}</span><span class="v"><code>{{ store.config.goVersion || '—' }}</code></span></div>
          <div class="kv"><span class="k">{{ $t('platform.field_defaultTenant') }}</span><span class="v">{{ store.config.defaultTenant || '—' }}</span></div>
          <div class="kv"><span class="k">{{ $t('platform.field_maxTenants') }}</span><span class="v">{{ store.config.maxTenants ?? '—' }}</span></div>
          <div class="kv"><span class="k">{{ $t('platform.field_enableMarketplace') }}</span><span class="v"><span class="status-pill" :class="store.config.enableMarketplace ? 'ok' : 'off'">{{ store.config.enableMarketplace ? $t('common.yes') : $t('common.no') }}</span></span></div>
          <div class="kv"><span class="k">{{ $t('platform.field_enableBilling') }}</span><span class="v"><span class="status-pill" :class="store.config.enableBilling ? 'ok' : 'off'">{{ store.config.enableBilling ? $t('common.yes') : $t('common.no') }}</span></span></div>
          <div class="kv"><span class="k">{{ $t('platform.field_updatedAt') }}</span><span class="v">{{ store.config.updatedAt || '—' }}</span></div>
        </div>
      </div>
    </div>

    <!-- ========== Tab 2: 健康检查 ========== -->
    <div v-show="activeTab === 'health'">
      <div class="btnbar">
        <button class="outline" @click="store.fetchHealth()"><Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}</button>
      </div>
      <div v-if="store.loading && !store.health" class="muted">{{ $t('common.loading') }}</div>
      <div v-else-if="store.health" class="card">
        <div class="row-2">
          <div class="kv"><span class="k">{{ $t('platform.health_status') }}</span><span class="v"><span class="status-pill" :class="healthClass(store.health.status)">{{ store.health.status || '—' }}</span></span></div>
          <div class="kv"><span class="k">{{ $t('platform.health_timestamp') }}</span><span class="v">{{ store.health.timestamp || '—' }}</span></div>
        </div>
        <h4>{{ $t('platform.health_components') }}</h4>
        <div class="kv-grid">
          <div v-for="(status, name) in (store.health.components || {})" :key="name" class="kv">
            <span class="k">{{ name }}</span>
            <span class="v"><span class="status-pill" :class="healthClass(status)">{{ status }}</span></span>
          </div>
          <div v-if="!Object.keys(store.health.components || {}).length" class="muted">{{ $t('common.no_data') }}</div>
        </div>
      </div>
    </div>

    <!-- ========== Tab 3: 指标汇总 ========== -->
    <div v-show="activeTab === 'metrics'">
      <div class="btnbar">
        <button class="outline" @click="store.fetchMetrics()"><Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}</button>
      </div>
      <div v-if="store.loading && !store.metrics" class="muted">{{ $t('common.loading') }}</div>
      <div v-else-if="store.metrics" class="stats">
        <div class="stat indigo"><div class="stat-v">{{ store.metrics.tenants ?? 0 }}</div><div class="stat-l">{{ $t('platform.metric_tenants') }}</div></div>
        <div class="stat teal"><div class="stat-v">{{ store.metrics.devices ?? 0 }}</div><div class="stat-l">{{ $t('platform.metric_devices') }}</div></div>
        <div class="stat amber"><div class="stat-v">{{ store.metrics.tasks ?? 0 }}</div><div class="stat-l">{{ $t('platform.metric_tasks') }}</div></div>
        <div class="stat rose"><div class="stat-v">{{ store.metrics.alerts ?? 0 }}</div><div class="stat-l">{{ $t('platform.metric_alerts') }}</div></div>
        <div class="stat indigo"><div class="stat-v">{{ store.metrics.apiKeys ?? 0 }}</div><div class="stat-l">{{ $t('platform.metric_apiKeys') }}</div></div>
        <div class="stat teal"><div class="stat-v">{{ store.metrics.plugins ?? 0 }}</div><div class="stat-l">{{ $t('platform.metric_plugins') }}</div></div>
        <div class="stat amber"><div class="stat-v">{{ store.metrics.subscriptions ?? 0 }}</div><div class="stat-l">{{ $t('platform.metric_subscriptions') }}</div></div>
        <div class="stat rose"><div class="stat-v">{{ store.metrics.invoices ?? 0 }}</div><div class="stat-l">{{ $t('platform.metric_invoices') }}</div></div>
      </div>
    </div>

    <!-- 编辑配置抽屉 -->
    <DetailDrawer :open="!!form" :title="$t('platform.edit')" @close="form = null">
      <form v-if="form" class="entity-form" @submit.prevent="onSave">
        <div class="field">
          <label>{{ $t('platform.field_defaultTenant') }}</label>
          <input v-model.trim="form.defaultTenant" type="text" />
        </div>
        <div class="field">
          <label>{{ $t('platform.field_maxTenants') }}</label>
          <input v-model.number="form.maxTenants" type="number" min="0" />
        </div>
        <div class="field">
          <label>
            <input type="checkbox" v-model="form.enableMarketplace" />
            {{ $t('platform.field_enableMarketplace') }}
          </label>
        </div>
        <div class="field">
          <label>
            <input type="checkbox" v-model="form.enableBilling" />
            {{ $t('platform.field_enableBilling') }}
          </label>
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('common.save') }}</button>
          <button type="button" class="outline" @click="form = null">{{ $t('common.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="platform-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// 平台配置页 — 三 Tab：配置 / 健康检查 / 指标汇总
import { ref, reactive, onMounted } from 'vue'
import { usePlatformStore } from '@/stores/platform'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'

const store = usePlatformStore()
const activeTab = ref('config')
const formError = ref('')
const errorConfirm = reactive({ show: false, message: '' })

const tabs = [
  { key: 'config', label: 'platform.tab_config' },
  { key: 'health', label: 'platform.tab_health' },
  { key: 'metrics', label: 'platform.tab_metrics' }
]

function healthClass(status) {
  if (status === 'ok') return 'ok'
  if (status === 'degraded') return 'warn'
  return 'off'
}

function switchTab(key) {
  activeTab.value = key
  if (key === 'config' && !store.config) store.fetchConfig()
  else if (key === 'health' && !store.health) store.fetchHealth()
  else if (key === 'metrics' && !store.metrics) store.fetchMetrics()
}

// ---------- 编辑配置 ----------
const form = ref(null)

function openEdit() {
  formError.value = ''
  const c = store.config || {}
  form.value = {
    defaultTenant: c.defaultTenant || '',
    maxTenants: c.maxTenants ?? 0,
    enableMarketplace: !!c.enableMarketplace,
    enableBilling: !!c.enableBilling
  }
}

async function onSave() {
  formError.value = ''
  try {
    await store.updateConfig({
      defaultTenant: form.value.defaultTenant,
      maxTenants: form.value.maxTenants,
      enableMarketplace: form.value.enableMarketplace,
      enableBilling: form.value.enableBilling
    })
    form.value = null
    toast.success(t('platform.saved'))
  } catch (e) {
    formError.value = e.j?.error || t('platform.save_failed')
  }
}

onMounted(() => {
  store.fetchConfig().catch((e) => {
    errorConfirm.message = e.j?.error || t('error.platformConfigFailed')
    errorConfirm.show = true
  })
})
</script>

<style scoped>
.card {
  background: var(--surface); border: 1px solid var(--border);
  border-radius: var(--radius); padding: 16px; margin-top: 14px;
  box-shadow: var(--shadow);
}
.card h4 { margin: 16px 0 8px; font-size: 13px; }
.kv-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 10px; }
.kv { display: flex; flex-direction: column; gap: 4px; padding: 8px 10px; background: var(--surface-2); border-radius: var(--radius-sm); }
.kv .k { font-size: 11.5px; color: var(--text-3); }
.kv .v { font-size: 13px; color: var(--text); word-break: break-all; }
.row-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; margin-bottom: 8px; }
.btnbar { display: flex; gap: 8px; margin-top: 8px; }
.field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 10px; min-width: 220px; }
.field label { margin: 0; font-size: 12.5px; color: var(--text-2); }

.stats { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; margin-top: 14px; }
.stat {
  background: var(--surface); border: 1px solid var(--border);
  border-radius: var(--radius); padding: 12px 14px;
  border-top: 3px solid var(--accent);
}
.stat.rose { border-top-color: var(--rose); }
.stat.amber { border-top-color: var(--amber); }
.stat.indigo { border-top-color: var(--indigo); }
.stat.teal { border-top-color: var(--teal); }
.stat-v { font-size: 22px; font-weight: 700; color: var(--text); line-height: 1.1; font-variant-numeric: tabular-nums; }
.stat-l { font-size: 11.5px; color: var(--text-3); margin-top: 4px; }

.poll-err {
  padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}
.status-pill.warn { background: var(--warn-soft, #fef3c7); color: var(--warn, #b45309); }

@media (max-width: 768px) {
  .stats { grid-template-columns: repeat(2, 1fr); }
  .row-2 { grid-template-columns: 1fr; }
}
</style>