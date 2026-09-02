<template>
  <div>
    <h2 data-testid="cmdb-changes-title">{{ $t('cmdbChanges.title') }}</h2>
    <p class="muted">{{ $t('cmdbChanges.subtitle') }}</p>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <!-- 状态筛选 -->
    <div class="filter-bar">
      <button
        v-for="s in statusFilters"
        :key="s.key"
        :class="['tab', { active: statusFilter === s.key }]"
        @click="setStatusFilter(s.key)"
      >{{ $t(s.label) }}</button>
      <button class="outline" @click="loadList"><Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}</button>
    </div>

    <!-- 变更列表 -->
    <div v-if="store.loading && !store.changes.length" class="muted">{{ $t('common.loading') }}</div>
    <DataTable
      v-else
      :columns="columns"
      :rows="filteredChanges"
      row-key="id"
      :loading="store.loading"
      :empty-text="$t('cmdbChanges.empty')"
    >
      <template #cell-id="{ value }"><code>{{ value }}</code></template>
      <template #cell-ciID="{ value }"><code>{{ value }}</code></template>
      <template #cell-changeType="{ value }">
        <span class="status-pill" :class="changeTypeClass(value)">{{ value }}</span>
      </template>
      <template #cell-status="{ value }">
        <span class="status-pill" :class="statusClass(value)">{{ $t('cmdbChanges.status_' + value) }}</span>
      </template>
      <template #cell-actions="{ row }">
        <div class="row-actions">
          <button class="xs outline" @click="viewDetail(row)" data-testid="cmdb-change-detail-btn">{{ $t('cmdbChanges.detail') }}</button>
          <button v-if="row.status === 'pending'" class="xs primary" @click="onApprove(row)" data-testid="cmdb-change-approve-btn">{{ $t('cmdbChanges.approve') }}</button>
          <button v-if="row.status === 'pending'" class="xs danger" @click="onReject(row)" data-testid="cmdb-change-reject-btn">{{ $t('cmdbChanges.reject') }}</button>
        </div>
      </template>
    </DataTable>

    <!-- 详情抽屉 -->
    <DetailDrawer :open="!!store.currentChange" :title="$t('cmdbChanges.detail_title', { id: store.currentChange?.id || '—' })" @close="store.currentChange = null">
      <div v-if="store.currentChange" class="detail-body">
        <div class="kv"><span class="k">{{ $t('cmdbChanges.field_ciID') }}</span><span class="v"><code>{{ store.currentChange.ciID || '—' }}</code></span></div>
        <div class="kv"><span class="k">{{ $t('cmdbChanges.field_changeType') }}</span><span class="v"><span class="status-pill" :class="changeTypeClass(store.currentChange.changeType)">{{ store.currentChange.changeType }}</span></span></div>
        <div class="kv"><span class="k">{{ $t('cmdbChanges.field_status') }}</span><span class="v"><span class="status-pill" :class="statusClass(store.currentChange.status)">{{ $t('cmdbChanges.status_' + store.currentChange.status) }}</span></span></div>
        <div class="kv"><span class="k">{{ $t('cmdbChanges.field_requester') }}</span><span class="v">{{ store.currentChange.requester || '—' }}</span></div>
        <div class="kv"><span class="k">{{ $t('cmdbChanges.field_approver') }}</span><span class="v">{{ store.currentChange.approver || '—' }}</span></div>
        <div class="kv"><span class="k">{{ $t('cmdbChanges.field_createdAt') }}</span><span class="v">{{ store.currentChange.createdAt || '—' }}</span></div>
        <div class="kv"><span class="k">{{ $t('cmdbChanges.field_approvedAt') }}</span><span class="v">{{ store.currentChange.approvedAt || '—' }}</span></div>
        <h4 v-if="store.currentChange.before">{{ $t('cmdbChanges.field_before') }}</h4>
        <pre v-if="store.currentChange.before" class="json-pre">{{ formatJson(store.currentChange.before) }}</pre>
        <h4 v-if="store.currentChange.after">{{ $t('cmdbChanges.field_after') }}</h4>
        <pre v-if="store.currentChange.after" class="json-pre">{{ formatJson(store.currentChange.after) }}</pre>
      </div>
    </DetailDrawer>

    <ConfirmModal
      v-model="confirm.show"
      data-testid="cmdb-change-confirm-modal"
      :title="confirm.title"
      :message="confirm.message"
      @confirm="onConfirm"
    />
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="cmdb-change-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// CMDB 变更审批页 — 列表 + 状态筛选 + 详情 + approve/reject
import { ref, reactive, computed, onMounted } from 'vue'
import { useCMDBAdvancedStore } from '@/stores/cmdb-advanced'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'

const store = useCMDBAdvancedStore()
const statusFilter = ref('pending')
const confirm = reactive({ show: false, title: '', message: '', action: '', id: null })
const errorConfirm = reactive({ show: false, message: '' })

const statusFilters = [
  { key: 'pending', label: 'cmdbChanges.filter_pending' },
  { key: 'approved', label: 'cmdbChanges.filter_approved' },
  { key: 'rejected', label: 'cmdbChanges.filter_rejected' },
  { key: '', label: 'common.all' }
]

const columns = computed(() => [
  { key: 'id', title: t('cmdbChanges.col_id'), slot: 'cell-id' },
  { key: 'ciID', title: t('cmdbChanges.field_ciID'), slot: 'cell-ciID' },
  { key: 'changeType', title: t('cmdbChanges.field_changeType'), slot: 'cell-changeType' },
  { key: 'status', title: t('cmdbChanges.field_status'), slot: 'cell-status' },
  { key: 'requester', title: t('cmdbChanges.field_requester') },
  { key: 'createdAt', title: t('cmdbChanges.field_createdAt') },
  { key: 'actions', title: t('cmdbChanges.col_actions'), slot: 'cell-actions', width: '180px' }
])

const filteredChanges = computed(() => {
  if (!statusFilter.value) return store.changes
  return store.changes.filter((c) => c.status === statusFilter.value)
})

function statusClass(s) {
  if (s === 'approved') return 'ok'
  if (s === 'pending') return 'warn'
  return 'off'
}

function changeTypeClass(t) {
  if (t === 'create') return 'ok'
  if (t === 'update') return 'warn'
  return 'off'
}

function formatJson(obj) {
  try { return JSON.stringify(obj, null, 2) } catch { return String(obj) }
}

function setStatusFilter(key) {
  statusFilter.value = key
}

function loadList() {
  store.fetchChanges().catch((e) => {
    errorConfirm.message = e.j?.error || t('error.cmdbChangesFailed')
    errorConfirm.show = true
  })
}

async function viewDetail(row) {
  try { await store.fetchChange(row.id) }
  catch (e) {
    errorConfirm.message = e.j?.error || t('error.cmdbChangeDetailFailed')
    errorConfirm.show = true
  }
}

function onApprove(row) {
  confirm.action = 'approve'
  confirm.id = row.id
  confirm.title = t('cmdbChanges.approve')
  confirm.message = t('cmdbChanges.confirm_approve')
  confirm.show = true
}

function onReject(row) {
  confirm.action = 'reject'
  confirm.id = row.id
  confirm.title = t('cmdbChanges.reject')
  confirm.message = t('cmdbChanges.confirm_reject')
  confirm.show = true
}

async function onConfirm() {
  if (!confirm.id) return
  try {
    if (confirm.action === 'approve') {
      await store.approveChange(confirm.id)
      toast.success(t('cmdbChanges.approved'))
    } else {
      await store.rejectChange(confirm.id)
      toast.success(t('cmdbChanges.rejected'))
    }
  } catch (e) {
    errorConfirm.message = e.j?.error || t('cmdbChanges.action_failed')
    errorConfirm.show = true
  }
}

onMounted(() => { loadList() })
</script>

<style scoped>
.filter-bar { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; margin: 10px 0; }
.btnbar { display: flex; gap: 8px; }

.detail-body .kv { display: flex; flex-direction: column; gap: 4px; margin-bottom: 8px; }
.detail-body .k { font-size: 11.5px; color: var(--text-3); }
.detail-body .v { font-size: 13px; color: var(--text); word-break: break-all; }
.detail-body h4 { margin: 14px 0 6px; font-size: 13px; }
.json-pre {
  background: var(--surface-2); border: 1px solid var(--border);
  border-radius: var(--radius-sm); padding: 8px; font-size: 12px;
  white-space: pre-wrap; word-break: break-all; max-height: 240px; overflow: auto;
}

.poll-err {
  padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}
.status-pill.warn { background: var(--warn-soft, #fef3c7); color: var(--warn, #b45309); }
</style>