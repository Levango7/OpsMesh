<template>
  <div>
    <h2>{{ $t('slos.title') }}</h2>
    <p class="muted">{{ $t('slos.subtitle') }}</p>

    <div class="btnbar">
      <button class="primary" @click="openCreate">
        <Icon name="add" :size="14" />
        {{ $t('slos.add') }}
      </button>
      <button class="outline" @click="fetchSLOs">
        <Icon name="refresh" :size="14" />
        {{ $t('common.refresh') }}
      </button>
    </div>

    <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
    <div v-else>
      <DataTable :columns="columns" :rows="slos" row-key="id" :empty-text="$t('slos.empty')">
        <template #cell-target="{ value }"><span class="target-badge">{{ value }}%</span></template>
        <template #cell-window="{ value }"><code>{{ value }}</code></template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <button class="xs outline" :title="$t('slos.status_view')" @click="openStatus(row)"><Icon name="alerts" :size="13" /></button>
            <button class="xs outline" @click="openEdit(row)"><Icon name="edit" :size="13" /></button>
            <button class="xs danger" @click="onDelete(row)"><Icon name="delete" :size="13" /></button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- 新增/编辑抽屉 -->
    <DetailDrawer :open="!!form" :title="form && form.id ? $t('slos.edit') : $t('slos.add')" @close="form = null">
      <form v-if="form" class="slo-form" @submit.prevent="onSave">
        <div class="field">
          <label>{{ $t('slos.new_name') }}</label>
          <input v-model.trim="form.name" type="text" required />
        </div>
        <div class="field">
          <label>{{ $t('slos.new_description') }}</label>
          <input v-model.trim="form.description" type="text" />
        </div>
        <div class="field">
          <label>{{ $t('slos.new_service') }}</label>
          <input v-model.trim="form.serviceName" type="text" required />
        </div>
        <div class="field">
          <label>{{ $t('slos.new_target') }}</label>
          <input v-model.number="form.target" type="number" min="0" max="100" step="0.1" required />
        </div>
        <div class="field">
          <label>{{ $t('slos.new_window') }}</label>
          <select v-model="form.window">
            <option value="7d">7d</option>
            <option value="30d">30d</option>
            <option value="90d">90d</option>
          </select>
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('slos.save') }}</button>
          <button type="button" class="outline" @click="form = null">{{ $t('slos.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- SLI 状态抽屉 -->
    <DetailDrawer :open="!!statusRow" :title="$t('slos.status_title', { name: statusRow?.name || '' })" @close="statusRow = null">
      <div v-if="statusLoading" class="muted">{{ $t('common.loading') }}</div>
      <DataTable v-else :columns="statusColumns" :rows="sliStatus" row-key="sliName" :empty-text="$t('slos.no_status')">
        <template #cell-status="{ value }">
          <span class="status-pill" :class="value === 'met' ? 'ok' : (value === 'breached' ? 'bad' : 'off')">
            {{ value === 'met' ? $t('slos.st_met') : (value === 'breached' ? $t('slos.st_breached') : $t('slos.st_nodata')) }}
          </span>
        </template>
      </DataTable>
    </DetailDrawer>

    <!-- 删除确认（替代 confirm） -->
    <ConfirmModal
      v-model="deleteConfirm.show"
      data-testid="slos-delete-confirm-modal"
      :title="$t('slos.delete')"
      :message="$t('slos.confirm_delete')"
      @confirm="onDeleteConfirm"
    />
    <!-- 错误提示（替代 alert） -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="slos-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// SLO 目标管理页 — CRUD + SLI 状态查看
import { ref, computed, reactive, onMounted } from 'vue'
import { getSLOs, createSLO, updateSLO, deleteSLO, getSLOStatus } from '@/api/slo'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'

const slos = ref([])
const loading = ref(false)
const form = ref(null)
const formError = ref('')

// SLI 状态抽屉状态
const statusRow = ref(null)
const sliStatus = ref([])
const statusLoading = ref(false)

// 删除确认弹窗（替代 confirm）
const deleteConfirm = reactive({ show: false, row: null })
// 错误提示弹窗（替代 alert）
const errorConfirm = reactive({ show: false, message: '' })

const columns = computed(() => [
  { key: 'name', title: t('slos.name') },
  { key: 'serviceName', title: t('slos.service') },
  { key: 'target', title: t('slos.target'), slot: 'cell-target' },
  { key: 'window', title: t('slos.window'), slot: 'cell-window' },
  { key: 'description', title: t('slos.description') },
  { key: 'actions', title: t('slos.actions'), slot: 'cell-actions', width: '90px' }
])

const statusColumns = computed(() => [
  { key: 'sliName', title: t('slos.sli_name') },
  { key: 'currentValue', title: t('slos.sli_current') },
  { key: 'targetValue', title: t('slos.sli_target') },
  { key: 'status', title: t('slos.sli_status'), slot: 'cell-status' }
])

async function fetchSLOs() {
  loading.value = true
  try {
    const r = await getSLOs()
    slos.value = (r && r.slos) || []
  } catch (e) {
    slos.value = []
  } finally {
    loading.value = false
  }
}

function openCreate() {
  formError.value = ''
  form.value = { id: null, name: '', description: '', serviceName: '', target: 99.9, window: '30d' }
}

function openEdit(row) {
  formError.value = ''
  form.value = {
    id: row.id,
    name: row.name || '',
    description: row.description || '',
    serviceName: row.serviceName || '',
    target: row.target != null ? row.target : 99.9,
    window: row.window || '30d'
  }
}

async function onSave() {
  formError.value = ''
  try {
    const body = {
      name: form.value.name,
      description: form.value.description,
      serviceName: form.value.serviceName,
      target: form.value.target,
      window: form.value.window
    }
    if (form.value.id) await updateSLO(form.value.id, body)
    else await createSLO(body)
    form.value = null
    toast.success(t('slos.saved'))
    await fetchSLOs()
  } catch (e) {
    formError.value = e.j?.error || t('slos.save_failed')
  }
}

async function openStatus(row) {
  statusRow.value = row
  statusLoading.value = true
  sliStatus.value = []
  try {
    const r = await getSLOStatus(row.id)
    // 契约：{sliStatus: [...]} 或裸数组，两态兼容
    sliStatus.value = (r && (r.sliStatus || r.sliStatuses || r.slis)) || (Array.isArray(r) ? r : []) || []
  } catch (e) {
    sliStatus.value = []
  } finally {
    statusLoading.value = false
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
    await deleteSLO(row.id)
    toast.success(t('slos.deleted'))
    await fetchSLOs()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('slos.delete_failed')
    errorConfirm.show = true
  }
}

onMounted(fetchSLOs)
</script>

<style scoped>
.row-actions { display: flex; gap: 4px; }
.target-badge {
  display: inline-flex; align-items: center;
  padding: 1px 9px; border-radius: 999px; font-size: 12px; font-weight: 600;
  background: var(--accent-soft); color: var(--accent);
}
.status-pill {
  display: inline-flex; align-items: center;
  padding: 1px 9px; border-radius: 999px; font-size: 12px; font-weight: 600;
}
.status-pill.ok { background: var(--accent-soft); color: var(--accent); }
.status-pill.off { background: var(--surface-3); color: var(--text-3); }
.status-pill.bad { background: rgba(214,69,56,.12); color: #d64538; }
.slo-form { display: flex; flex-direction: column; gap: 12px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field > label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.btnbar { margin-top: 8px; }
</style>
