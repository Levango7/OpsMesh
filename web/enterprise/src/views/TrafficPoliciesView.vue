<template>
  <div>
    <h2>{{ $t('traffic.title') }}</h2>
    <p class="muted">{{ $t('traffic.subtitle') }}</p>

    <div class="btnbar">
      <button class="primary" @click="openCreate">
        <Icon name="add" :size="14" />
        {{ $t('traffic.add') }}
      </button>
      <button class="outline" @click="fetchPolicies">
        <Icon name="refresh" :size="14" />
        {{ $t('common.refresh') }}
      </button>
    </div>

    <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
    <div v-else>
      <DataTable :columns="columns" :rows="policies" row-key="id" :empty-text="$t('traffic.empty')">
        <template #cell-type="{ value }"><code>{{ typeText(value) }}</code></template>
        <template #cell-status="{ value }">
          <span class="status-pill" :class="value === 'active' ? 'ok' : 'off'">{{ statusText(value) }}</span>
        </template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <button
              class="xs outline"
              :title="row.status === 'active' ? $t('traffic.disable') : $t('traffic.enable')"
              @click="onToggle(row)"
            ><Icon :name="row.status === 'active' ? 'mute' : 'success'" :size="13" /></button>
            <button class="xs outline" @click="openEdit(row)"><Icon name="edit" :size="13" /></button>
            <button class="xs danger" @click="onDelete(row)"><Icon name="delete" :size="13" /></button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- 新增/编辑抽屉 -->
    <DetailDrawer :open="!!form" :title="form && form.id ? $t('traffic.edit') : $t('traffic.add')" @close="form = null">
      <form v-if="form" class="policy-form" @submit.prevent="onSave">
        <div class="field">
          <label>{{ $t('traffic.new_name') }}</label>
          <input v-model.trim="form.name" type="text" required />
        </div>
        <div class="field">
          <label>{{ $t('traffic.new_service') }}</label>
          <input v-model.trim="form.serviceName" type="text" required />
        </div>
        <div class="field">
          <label>{{ $t('traffic.new_type') }}</label>
          <select v-model="form.type">
            <option value="canary">{{ $t('traffic.t_canary') }}</option>
            <option value="timeout">{{ $t('traffic.t_timeout') }}</option>
            <option value="retry">{{ $t('traffic.t_retry') }}</option>
            <option value="circuit_breaker">{{ $t('traffic.t_circuit_breaker') }}</option>
            <option value="mirror">{{ $t('traffic.t_mirror') }}</option>
          </select>
        </div>
        <div v-if="form.type === 'timeout' || form.type === 'retry'" class="field">
          <label>{{ $t('traffic.new_timeout') }}</label>
          <input v-model.trim="form.timeout" type="text" placeholder="5s" />
        </div>
        <div v-if="form.type === 'retry'" class="field">
          <label>{{ $t('traffic.new_retries') }}</label>
          <input v-model.number="form.retries" type="number" min="0" max="10" />
        </div>
        <div v-if="form.type === 'circuit_breaker'" class="field">
          <label>{{ $t('traffic.new_max_conns') }}</label>
          <input v-model.number="form.maxConns" type="number" min="0" />
        </div>
        <div v-if="form.type === 'mirror'" class="field">
          <label>{{ $t('traffic.new_mirror_percent') }}</label>
          <input v-model.number="form.mirrorPercent" type="number" min="0" max="100" />
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('traffic.save') }}</button>
          <button type="button" class="outline" @click="form = null">{{ $t('traffic.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 删除确认（替代 confirm） -->
    <ConfirmModal
      v-model="deleteConfirm.show"
      data-testid="traffic-delete-confirm-modal"
      :title="$t('traffic.delete')"
      :message="$t('traffic.confirm_delete')"
      @confirm="onDeleteConfirm"
    />
    <!-- 错误提示（替代 alert） -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="traffic-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// 流量策略管理页 — CRUD + 启用/禁用（灰度/超时/重试/熔断/镜像）
import { ref, computed, reactive, onMounted } from 'vue'
import {
  getTrafficPolicies, createTrafficPolicy, updateTrafficPolicy, deleteTrafficPolicy,
  enableTrafficPolicy, disableTrafficPolicy
} from '@/api/traffic'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'

const policies = ref([])
const loading = ref(false)
const form = ref(null)
const formError = ref('')

// 删除确认弹窗（替代 confirm）
const deleteConfirm = reactive({ show: false, row: null })
// 错误提示弹窗（替代 alert）
const errorConfirm = reactive({ show: false, message: '' })

const columns = computed(() => [
  { key: 'name', title: t('traffic.name') },
  { key: 'serviceName', title: t('traffic.service') },
  { key: 'type', title: t('traffic.type'), slot: 'cell-type' },
  { key: 'status', title: t('traffic.status'), slot: 'cell-status' },
  { key: 'actions', title: t('traffic.actions'), slot: 'cell-actions', width: '90px' }
])

function typeText(v) {
  const m = {
    canary: 't_canary', timeout: 't_timeout', retry: 't_retry',
    circuit_breaker: 't_circuit_breaker', mirror: 't_mirror'
  }
  return m[v] ? t(`traffic.${m[v]}`) : (v || '—')
}

function statusText(v) {
  if (v === 'active') return t('traffic.active')
  if (v === 'inactive') return t('traffic.inactive')
  return v || '—'
}

async function fetchPolicies() {
  loading.value = true
  try {
    const r = await getTrafficPolicies()
    policies.value = (r && r.policies) || []
  } catch (e) {
    policies.value = []
  } finally {
    loading.value = false
  }
}

function openCreate() {
  formError.value = ''
  form.value = {
    id: null, name: '', serviceName: '', type: 'canary',
    timeout: '5s', retries: 2, maxConns: 100, mirrorPercent: 10
  }
}

function openEdit(row) {
  formError.value = ''
  form.value = {
    id: row.id,
    name: row.name || '',
    serviceName: row.serviceName || '',
    type: row.type || 'canary',
    timeout: row.timeout || '5s',
    retries: row.retries || 0,
    maxConns: row.maxConns || 0,
    mirrorPercent: row.mirrorPercent || 0
  }
}

async function onSave() {
  formError.value = ''
  try {
    const body = { name: form.value.name, serviceName: form.value.serviceName, type: form.value.type }
    // 按策略类型带上对应参数（后端 TrafficPolicy 字段）
    if (form.value.type === 'timeout' || form.value.type === 'retry') body.timeout = form.value.timeout
    if (form.value.type === 'retry') body.retries = form.value.retries
    if (form.value.type === 'circuit_breaker') body.maxConns = form.value.maxConns
    if (form.value.type === 'mirror') body.mirrorPercent = form.value.mirrorPercent
    if (form.value.id) await updateTrafficPolicy(form.value.id, body)
    else await createTrafficPolicy(body)
    form.value = null
    toast.success(t('traffic.saved'))
    await fetchPolicies()
  } catch (e) {
    formError.value = e.j?.error || t('traffic.save_failed')
  }
}

async function onToggle(row) {
  try {
    if (row.status === 'active') await disableTrafficPolicy(row.id)
    else await enableTrafficPolicy(row.id)
    toast.success(row.status === 'active' ? t('traffic.disabled') : t('traffic.enabled'))
    await fetchPolicies()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('traffic.toggle_failed')
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
    await deleteTrafficPolicy(row.id)
    toast.success(t('traffic.deleted'))
    await fetchPolicies()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('traffic.delete_failed')
    errorConfirm.show = true
  }
}

onMounted(fetchPolicies)
</script>

<style scoped>
.row-actions { display: flex; gap: 4px; }
.status-pill {
  display: inline-flex; align-items: center;
  padding: 1px 9px; border-radius: 999px; font-size: 12px; font-weight: 600;
}
.status-pill.ok { background: var(--accent-soft); color: var(--accent); }
.status-pill.off { background: var(--surface-3); color: var(--text-3); }
.policy-form { display: flex; flex-direction: column; gap: 12px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field > label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.btnbar { margin-top: 8px; }
</style>
