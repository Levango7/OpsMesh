<template>
  <div>
    <h2>{{ $t('gateway_routes.title') }}</h2>
    <p class="muted">{{ $t('gateway_routes.subtitle') }}</p>

    <!-- 网关统计卡片（只读） -->
    <div class="stats-grid">
      <div class="stat-card">
        <div class="stat-num">{{ stats.totalRequests ?? '—' }}</div>
        <div class="stat-label">{{ $t('gateway_routes.stats_total_requests') }}</div>
      </div>
      <div class="stat-card">
        <div class="stat-num">{{ stats.totalErrors ?? '—' }}</div>
        <div class="stat-label">{{ $t('gateway_routes.stats_total_errors') }}</div>
      </div>
      <div class="stat-card">
        <div class="stat-num">{{ stats.avgLatencyMs != null ? stats.avgLatencyMs.toFixed(1) + ' ms' : '—' }}</div>
        <div class="stat-label">{{ $t('gateway_routes.stats_avg_latency') }}</div>
      </div>
      <div class="stat-card">
        <div class="stat-num">{{ stats.activeRoutes ?? '—' }}</div>
        <div class="stat-label">{{ $t('gateway_routes.stats_active_routes') }}</div>
      </div>
    </div>

    <div class="btnbar">
      <button class="primary" data-testid="gateway-add-route-btn" @click="openCreate">
        <Icon name="add" :size="14" />
        {{ $t('gateway_routes.add') }}
      </button>
      <button class="outline" @click="fetchAll">
        <Icon name="refresh" :size="14" />
        {{ $t('common.refresh') }}
      </button>
    </div>

    <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
    <DataTable v-else :columns="columns" :rows="routes" row-key="id" :empty-text="$t('gateway_routes.empty')">
      <template #cell-name="{ value }"><code>{{ value }}</code></template>
      <template #cell-pathPrefix="{ value }"><code>{{ value }}</code></template>
      <template #cell-targetBackend="{ value }"><code>{{ value }}</code></template>
      <template #cell-methods="{ value }">
        <div class="scope-tags">
          <span v-for="m in value || []" :key="m" class="method-tag">{{ m }}</span>
          <span v-if="!(value || []).length" class="muted">{{ $t('gateway_routes.methods_all') }}</span>
        </div>
      </template>
      <template #cell-rateLimitPerSec="{ value }">{{ value > 0 ? value : $t('gateway_routes.unlimited') }}</template>
      <template #cell-enabled="{ value }">
        <StatusBadge :status="value ? 'ok' : 'fail'" :text="value ? $t('gateway_routes.enabled') : $t('gateway_routes.disabled')" />
      </template>
      <template #cell-updatedAt="{ value }">{{ fmtTime(value) }}</template>
      <template #cell-actions="{ row }">
        <div class="row-actions">
          <button class="xs outline" @click="openEdit(row)"><Icon name="edit" :size="13" /></button>
          <!-- 启停切换 -->
          <button v-if="!row.enabled" class="xs outline" @click="onToggle(row, true)">{{ $t('gateway_routes.enable') }}</button>
          <button v-else class="xs outline" @click="onToggle(row, false)">{{ $t('gateway_routes.disable') }}</button>
          <button class="xs danger" @click="onDelete(row)"><Icon name="delete" :size="13" /></button>
        </div>
      </template>
    </DataTable>

    <!-- 新增/编辑抽屉 -->
    <DetailDrawer :open="!!form" :title="form && form.id ? $t('gateway_routes.edit') : $t('gateway_routes.add')" @close="form = null">
      <form v-if="form" class="route-form" @submit.prevent="onSave">
        <div class="field">
          <label>{{ $t('gateway_routes.new_name') }}</label>
          <input v-model.trim="form.name" type="text" required />
        </div>
        <div class="field">
          <label>{{ $t('gateway_routes.new_path_prefix') }}</label>
          <input v-model.trim="form.pathPrefix" type="text" required placeholder="/api/v1/xxx" />
        </div>
        <div class="field">
          <label>{{ $t('gateway_routes.new_target_backend') }}</label>
          <input v-model.trim="form.targetBackend" type="text" required placeholder="http://host:port" />
          <span class="hint">{{ $t('gateway_routes.target_backend_hint') }}</span>
        </div>
        <div class="field">
          <label>{{ $t('gateway_routes.new_methods') }}</label>
          <div class="checkbox-group">
            <label v-for="m in methodOptions" :key="m" class="checkbox-item">
              <input type="checkbox" :value="m" v-model="form.methods" />
              <span>{{ m }}</span>
            </label>
          </div>
          <span class="hint">{{ $t('gateway_routes.methods_hint') }}</span>
        </div>
        <div class="field">
          <label>{{ $t('gateway_routes.new_rate_limit') }}</label>
          <input v-model.number="form.rateLimitPerSec" type="number" min="0" step="1" />
          <span class="hint">{{ $t('gateway_routes.rate_limit_hint') }}</span>
        </div>
        <div class="field">
          <label class="checkbox-item">
            <input type="checkbox" v-model="form.enabled" />
            <span>{{ $t('gateway_routes.new_enabled') }}</span>
          </label>
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('gateway_routes.save') }}</button>
          <button type="button" class="outline" @click="form = null">{{ $t('gateway_routes.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 删除确认（替代 confirm） -->
    <ConfirmModal
      v-model="deleteConfirm.show"
      data-testid="gateway-routes-delete-confirm-modal"
      :title="$t('gateway_routes.delete')"
      :message="$t('gateway_routes.confirm_delete')"
      @confirm="onDeleteConfirm"
    />
    <!-- 错误提示（替代 alert） -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="gateway-routes-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// API 网关路由管理页 — 路由规则列表 + 新增/编辑/删除 + 启停 + 网关统计
import { ref, reactive, onMounted } from 'vue'
import { listRoutes, createRoute, updateRoute, deleteRoute, enableRoute, disableRoute, getStats } from '@/api/gateway'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { fmtTime } from '@/composables/useFormatTime'

const routes = ref([])
const stats = ref({})
const loading = ref(false)
const form = ref(null)
const formError = ref('')

// 删除确认弹窗（替代 confirm）
const deleteConfirm = reactive({ show: false, row: null })
// 错误提示弹窗（替代 alert）
const errorConfirm = reactive({ show: false, message: '' })

const methodOptions = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS']

const columns = [
  { key: 'name', title: t('gateway_routes.name'), slot: 'cell-name' },
  { key: 'pathPrefix', title: t('gateway_routes.path_prefix'), slot: 'cell-pathPrefix' },
  { key: 'targetBackend', title: t('gateway_routes.target_backend'), slot: 'cell-targetBackend' },
  { key: 'methods', title: t('gateway_routes.methods'), slot: 'cell-methods' },
  { key: 'rateLimitPerSec', title: t('gateway_routes.rate_limit'), slot: 'cell-rateLimitPerSec', width: '100px' },
  { key: 'enabled', title: t('gateway_routes.status'), slot: 'cell-enabled', width: '80px' },
  { key: 'updatedAt', title: t('gateway_routes.updated_at'), slot: 'cell-updatedAt', width: '150px' },
  { key: 'actions', title: t('gateway_routes.actions'), slot: 'cell-actions', width: '140px' }
]

async function fetchRoutes() {
  loading.value = true
  try {
    const r = await listRoutes()
    routes.value = (r && r.routes) || []
  } catch {
    routes.value = []
  } finally {
    loading.value = false
  }
}

async function fetchStats() {
  try {
    stats.value = (await getStats()) || {}
  } catch {
    stats.value = {}
  }
}

function fetchAll() {
  fetchRoutes()
  fetchStats()
}

function openCreate() {
  formError.value = ''
  form.value = { id: null, name: '', pathPrefix: '', targetBackend: '', methods: [], rateLimitPerSec: 0, enabled: true }
}

function openEdit(row) {
  formError.value = ''
  form.value = {
    id: row.id,
    name: row.name || '',
    pathPrefix: row.pathPrefix || '',
    targetBackend: row.targetBackend || '',
    methods: [...(row.methods || [])],
    rateLimitPerSec: row.rateLimitPerSec || 0,
    enabled: !!row.enabled
  }
}

async function onSave() {
  formError.value = ''
  const body = {
    name: form.value.name,
    pathPrefix: form.value.pathPrefix,
    targetBackend: form.value.targetBackend,
    methods: form.value.methods,
    rateLimitPerSec: Number(form.value.rateLimitPerSec) || 0,
    enabled: form.value.enabled
  }
  try {
    if (form.value.id) {
      await updateRoute(form.value.id, body)
      toast.success(t('gateway_routes.update_ok'))
    } else {
      await createRoute(body)
      toast.success(t('gateway_routes.create_ok'))
    }
    form.value = null
    fetchAll()
  } catch (e) {
    formError.value = e.j?.error || t('gateway_routes.save_failed')
  }
}

// 启停切换
async function onToggle(row, enable) {
  try {
    if (enable) await enableRoute(row.id)
    else await disableRoute(row.id)
    toast.success(enable ? t('gateway_routes.enable_ok') : t('gateway_routes.disable_ok'))
    fetchAll()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('gateway_routes.toggle_failed')
    errorConfirm.show = true
  }
}

function onDelete(row) {
  deleteConfirm.row = row
  deleteConfirm.show = true
}

async function onDeleteConfirm() {
  const row = deleteConfirm.row
  if (!row) return
  try {
    await deleteRoute(row.id)
    toast.success(t('gateway_routes.delete_ok'))
    fetchAll()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('gateway_routes.delete_failed')
    errorConfirm.show = true
  }
}

onMounted(() => {
  fetchAll()
})
</script>

<style scoped>
.stats-grid { display: flex; flex-wrap: wrap; gap: 12px; margin: 14px 0 4px; }
.stat-card {
  min-width: 150px; padding: 14px 20px;
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  text-align: center;
}
.stat-num { font-size: 22px; font-weight: 700; color: var(--accent); }
.stat-label { margin-top: 5px; font-size: 12px; color: var(--text-3); }
.row-actions { display: flex; gap: 4px; }
.scope-tags { display: flex; flex-wrap: wrap; gap: 4px; }
.method-tag {
  display: inline-flex; align-items: center; height: 19px;
  padding: 0 8px; border-radius: 999px;
  background: var(--accent-soft); color: var(--accent);
  font-size: 11px; font-weight: 700; font-family: var(--mono, monospace);
}
.route-form { display: flex; flex-direction: column; gap: 12px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field > label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.checkbox-group { display: flex; flex-wrap: wrap; gap: 8px; }
.checkbox-item {
  display: inline-flex; align-items: center; gap: 5px;
  font-size: 13px; color: var(--text); margin: 0;
  padding: 4px 10px; border: 1px solid var(--border); border-radius: var(--radius-sm);
  background: var(--surface-2); cursor: pointer;
}
.hint { font-size: 12px; color: var(--text-3); }
.btnbar { margin-top: 8px; }
</style>
