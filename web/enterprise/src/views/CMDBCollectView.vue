<template>
  <div>
    <h2 data-testid="cmdb-collect-title">{{ $t('cmdbCollect.title') }}</h2>
    <p class="muted">{{ $t('cmdbCollect.subtitle') }}</p>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <!-- ========== 采集触发 ========== -->
    <div class="card">
      <h3>{{ $t('cmdbCollect.collect_title') }}</h3>
      <p class="muted">{{ $t('cmdbCollect.collect_hint') }}</p>
      <div class="btnbar">
        <button class="primary" :disabled="collecting" @click="onCollect" data-testid="cmdb-collect-btn">
          <Icon name="refresh" :size="14" /> {{ collecting ? $t('common.loading') : $t('cmdbCollect.collect') }}
        </button>
      </div>
      <div v-if="store.lastCollect" class="collect-result">
        <span class="status-pill ok">{{ $t('cmdbCollect.collected') }}: {{ store.lastCollect.collected ?? 0 }}</span>
        <span class="status-pill off">{{ $t('cmdbCollect.failed') }}: {{ store.lastCollect.failed ?? 0 }}</span>
      </div>
    </div>

    <!-- ========== CI 导入导出 ========== -->
    <div class="card">
      <h3>{{ $t('cmdbCollect.import_export_title') }}</h3>
      <div class="row-2">
        <div class="field">
          <label>{{ $t('cmdbCollect.import_label') }}</label>
          <textarea v-model="importRaw" rows="4" :placeholder="$t('cmdbCollect.import_placeholder')"></textarea>
          <button class="primary xs" :disabled="importing" @click="onImport" data-testid="cmdb-import-btn">
            {{ importing ? $t('common.loading') : $t('cmdbCollect.import') }}
          </button>
        </div>
        <div class="field">
          <label>{{ $t('cmdbCollect.export_label') }}</label>
          <button class="outline xs" :disabled="exporting" @click="onExport" data-testid="cmdb-export-btn">
            <Icon name="refresh" :size="13" /> {{ exporting ? $t('common.loading') : $t('cmdbCollect.export') }}
          </button>
          <div v-if="store.lastExport != null" class="muted">{{ $t('cmdbCollect.exported_count', { n: Array.isArray(store.lastExport) ? store.lastExport.length : 0 }) }}</div>
        </div>
      </div>
      <div v-if="store.lastImport" class="collect-result">
        <span class="status-pill ok">{{ $t('cmdbCollect.imported') }}: {{ store.lastImport.imported ?? 0 }}</span>
        <span class="status-pill off">{{ $t('cmdbCollect.failed') }}: {{ store.lastImport.failed ?? 0 }}</span>
      </div>
    </div>

    <!-- ========== 待审批 CI 列表 ========== -->
    <div class="card">
      <div class="flowbar">
        <h3>{{ $t('cmdbCollect.pending_title') }}</h3>
        <button class="xs outline" @click="store.fetchPendingCIs()"><Icon name="refresh" :size="13" /> {{ $t('common.refresh') }}</button>
      </div>
      <div v-if="store.loading && !store.pendingCIs.length" class="muted">{{ $t('common.loading') }}</div>
      <DataTable
        v-else
        :columns="ciColumns"
        :rows="store.pendingCIs"
        row-key="id"
        :loading="store.loading"
        :empty-text="$t('cmdbCollect.empty_pending')"
      >
        <template #cell-id="{ value }"><code>{{ value }}</code></template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <button class="xs outline" @click="openCIEdit(row)" data-testid="cmdb-ci-edit-btn"><Icon name="edit" :size="13" /></button>
            <button class="xs primary" @click="onCIApprove(row)" data-testid="cmdb-ci-approve-btn">{{ $t('cmdbCollect.approve') }}</button>
            <button class="xs danger" @click="onCIReject(row)" data-testid="cmdb-ci-reject-btn">{{ $t('cmdbCollect.reject') }}</button>
            <button class="xs danger" @click="onCIDelete(row)" data-testid="cmdb-ci-delete-btn"><Icon name="delete" :size="13" /></button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- CI 编辑抽屉 -->
    <DetailDrawer :open="!!ciForm" :title="$t('cmdbCollect.ci_edit_title')" @close="ciForm = null">
      <form v-if="ciForm" class="entity-form" @submit.prevent="onCISave">
        <div class="field">
          <label>{{ $t('cmdbCollect.ci_attributes') }}</label>
          <textarea v-model="ciForm.attrsRaw" rows="8" :placeholder="$t('cmdbCollect.attrs_placeholder')"></textarea>
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('common.save') }}</button>
          <button type="button" class="outline" @click="ciForm = null">{{ $t('common.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <ConfirmModal
      v-model="confirm.show"
      data-testid="cmdb-collect-confirm-modal"
      :title="confirm.title"
      :message="confirm.message"
      @confirm="onConfirm"
    />
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="cmdb-collect-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// CMDB 采集管理页 — 触发采集 + CI 导入导出 + 待审批 CI 列表 + CI 编辑/删除/审批
import { ref, reactive, computed, onMounted } from 'vue'
import { useCMDBAdvancedStore } from '@/stores/cmdb-advanced'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'

const store = useCMDBAdvancedStore()
const collecting = ref(false)
const importing = ref(false)
const exporting = ref(false)
const importRaw = ref('')
const formError = ref('')
const confirm = reactive({ show: false, title: '', message: '', action: '', id: null })
const errorConfirm = reactive({ show: false, message: '' })

const ciColumns = computed(() => [
  { key: 'id', title: t('cmdbCollect.col_id'), slot: 'cell-id' },
  { key: 'name', title: t('cmdbCollect.field_name') },
  { key: 'type', title: t('cmdbCollect.field_type') },
  { key: 'category', title: t('cmdbCollect.field_category') },
  { key: 'updatedAt', title: t('cmdbCollect.field_updatedAt') },
  { key: 'actions', title: t('cmdbCollect.col_actions'), slot: 'cell-actions', width: '220px' }
])

async function onCollect() {
  collecting.value = true
  try {
    await store.collect()
    toast.success(t('cmdbCollect.collect_success'))
  } catch (e) {
    errorConfirm.message = e.j?.error || t('error.cmdbCollectFailed')
    errorConfirm.show = true
  } finally {
    collecting.value = false
  }
}

async function onImport() {
  if (!importRaw.value.trim()) return
  importing.value = true
  let body
  try {
    body = JSON.parse(importRaw.value)
  } catch {
    errorConfirm.message = t('cmdbCollect.import_json_invalid')
    errorConfirm.show = true
    importing.value = false
    return
  }
  try {
    await store.importCIs(body)
    toast.success(t('cmdbCollect.import_success'))
  } catch (e) {
    errorConfirm.message = e.j?.error || t('error.cmdbImportFailed')
    errorConfirm.show = true
  } finally {
    importing.value = false
  }
}

async function onExport() {
  exporting.value = true
  try {
    const data = await store.exportCIs()
    const json = JSON.stringify(data || [], null, 2)
    const blob = new Blob([json], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `cmdb-ci-export-${Date.now()}.json`
    a.click()
    URL.revokeObjectURL(url)
  } catch (e) {
    errorConfirm.message = e.j?.error || t('error.cmdbExportFailed')
    errorConfirm.show = true
  } finally {
    exporting.value = false
  }
}

// ---------- CI 编辑 ----------
const ciForm = ref(null)

function openCIEdit(row) {
  formError.value = ''
  const attrs = row.attributes || row
  ciForm.value = { id: row.id, attrsRaw: JSON.stringify(attrs, null, 2) }
}

async function onCISave() {
  formError.value = ''
  let body
  try {
    body = JSON.parse(ciForm.value.attrsRaw)
  } catch {
    formError.value = t('cmdbCollect.import_json_invalid')
    return
  }
  try {
    await store.updateCI(ciForm.value.id, body)
    ciForm.value = null
    toast.success(t('cmdbCollect.ci_saved'))
  } catch (e) {
    formError.value = e.j?.error || t('cmdbCollect.ci_save_failed')
  }
}

function onCIApprove(row) {
  confirm.action = 'ciApprove'
  confirm.id = row.id
  confirm.title = t('cmdbCollect.approve')
  confirm.message = t('cmdbCollect.confirm_approve')
  confirm.show = true
}

function onCIReject(row) {
  confirm.action = 'ciReject'
  confirm.id = row.id
  confirm.title = t('cmdbCollect.reject')
  confirm.message = t('cmdbCollect.confirm_reject')
  confirm.show = true
}

function onCIDelete(row) {
  confirm.action = 'ciDelete'
  confirm.id = row.id
  confirm.title = t('common.delete')
  confirm.message = t('cmdbCollect.confirm_delete')
  confirm.show = true
}

async function onConfirm() {
  if (!confirm.id) return
  try {
    if (confirm.action === 'ciApprove') {
      await store.approveCI(confirm.id)
      toast.success(t('cmdbCollect.approved'))
    } else if (confirm.action === 'ciReject') {
      await store.rejectCI(confirm.id)
      toast.success(t('cmdbCollect.rejected'))
    } else if (confirm.action === 'ciDelete') {
      await store.removeCI(confirm.id)
      toast.success(t('cmdbCollect.deleted'))
    }
  } catch (e) {
    errorConfirm.message = e.j?.error || t('cmdbCollect.action_failed')
    errorConfirm.show = true
  }
}

onMounted(() => {
  store.fetchPendingCIs().catch((e) => {
    errorConfirm.message = e.j?.error || t('error.cmdbPendingFailed')
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
.card h3 { margin: 0 0 8px; font-size: 14px; }
.flowbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 10px; }
.flowbar h3 { margin: 0; }
.btnbar { display: flex; gap: 8px; margin-top: 8px; }
.field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 10px; min-width: 220px; }
.field label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.row-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }

.collect-result { display: flex; gap: 8px; margin-top: 10px; }

.poll-err {
  padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}

@media (max-width: 768px) {
  .row-2 { grid-template-columns: 1fr; }
}
</style>