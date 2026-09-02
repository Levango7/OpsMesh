<template>
  <div>
    <h2 data-testid="approval-requests-title">{{ $t('approval.requests_title') }}</h2>
    <p class="muted">{{ $t('approval.requests_subtitle') }}</p>

    <!-- Tab 切换 -->
    <div class="tabbar">
      <button
        v-for="tab in tabs"
        :key="tab.key"
        :class="['tab', { active: activeTab === tab.key }]"
        :data-testid="'approval-requests-tab-' + tab.key"
        @click="switchTab(tab.key)"
      >{{ $t(tab.label) }}</button>
    </div>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <!-- ========== Tab 1: 审批请求列表 ========== -->
    <div v-show="activeTab === 'requests'">
      <div class="filter-bar">
        <div class="field">
          <label>{{ $t('approval.filter_status') }}</label>
          <select v-model="statusFilter">
            <option value="">{{ $t('common.all') }}</option>
            <option value="pending">{{ $t('approval.status_pending') }}</option>
            <option value="approved">{{ $t('approval.status_approved') }}</option>
            <option value="rejected">{{ $t('approval.status_rejected') }}</option>
            <option value="cancelled">{{ $t('approval.status_cancelled') }}</option>
          </select>
        </div>
        <button class="primary" @click="searchRequests" data-testid="approval-requests-search-btn">
          <Icon name="search" :size="14" /> {{ $t('common.search') }}
        </button>
        <button class="outline" @click="store.fetchRequests()"><Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}</button>
      </div>
      <div v-if="store.loading && !store.requests.length" class="muted">{{ $t('common.loading') }}</div>
      <DataTable
        v-else
        :columns="requestColumns"
        :rows="store.requests"
        row-key="id"
        :loading="store.loading"
        :empty-text="$t('approval.empty_requests')"
      >
        <template #cell-status="{ value }">
          <span class="status-pill" :class="statusClass(value)">{{ statusText(value) }}</span>
        </template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <button class="xs outline" @click="viewHistory(row)" data-testid="approval-request-history-btn">
              <Icon name="clock" :size="13" />
            </button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- ========== Tab 2: 待我审批 ========== -->
    <div v-show="activeTab === 'pending'">
      <div class="btnbar">
        <button class="outline" @click="store.fetchPending()"><Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}</button>
      </div>
      <div v-if="store.loading && !store.pending.length" class="muted">{{ $t('common.loading') }}</div>
      <DataTable
        v-else
        :columns="pendingColumns"
        :rows="store.pending"
        row-key="id"
        :loading="store.loading"
        :empty-text="$t('approval.empty_pending')"
      >
        <template #cell-status="{ value }">
          <span class="status-pill" :class="statusClass(value)">{{ statusText(value) }}</span>
        </template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <button class="xs primary" @click="openApprove(row)" data-testid="approval-approve-btn">{{ $t('approval.approve') }}</button>
            <button class="xs danger" @click="openReject(row)" data-testid="approval-reject-btn">{{ $t('approval.reject') }}</button>
            <button class="xs outline" @click="openCancel(row)" data-testid="approval-cancel-btn">{{ $t('approval.cancel') }}</button>
            <button class="xs outline" @click="viewHistory(row)" data-testid="approval-pending-history-btn">
              <Icon name="clock" :size="13" />
            </button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- approve/reject/cancel 评论输入抽屉 -->
    <DetailDrawer :open="!!actionForm" :title="actionTitle" @close="actionForm = null">
      <form v-if="actionForm" class="entity-form" @submit.prevent="onActionSubmit">
        <div class="field">
          <label>{{ $t('approval.field_comment') }}</label>
          <textarea v-model.trim="actionForm.comment" rows="3" :placeholder="$t('approval.comment_placeholder')"></textarea>
        </div>
        <div v-if="actionError" class="msg err">{{ actionError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('common.confirm') }}</button>
          <button type="button" class="outline" @click="actionForm = null">{{ $t('common.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 审批历史抽屉 -->
    <DetailDrawer :open="historyOpen" :title="$t('approval.history_title')" @close="historyOpen = false">
      <div v-if="store.loading" class="muted">{{ $t('common.loading') }}</div>
      <DataTable
        v-else
        :columns="historyColumns"
        :rows="store.history"
        row-key="id"
        :empty-text="$t('approval.empty_history')"
      >
        <template #cell-action="{ value }">
          <span class="status-pill" :class="value === 'approve' ? 'ok' : 'off'">{{ value }}</span>
        </template>
      </DataTable>
    </DetailDrawer>

    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="approval-requests-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// 审批请求页 — 两 Tab：审批请求列表（筛选 pending/approved/rejected）+ 待我审批（approve/reject/cancel + 查看历史）
import { ref, computed, reactive, onMounted } from 'vue'
import { useApprovalStore } from '@/stores/approval'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'

const store = useApprovalStore()
const activeTab = ref('requests')
const statusFilter = ref('')
const errorConfirm = reactive({ show: false, message: '' })

const tabs = [
  { key: 'requests', label: 'approval.tab_requests' },
  { key: 'pending', label: 'approval.tab_pending' }
]

const requestColumns = computed(() => [
  { key: 'id', title: t('approval.field_id'), width: '120px' },
  { key: 'flowName', title: t('approval.field_flowName') },
  { key: 'requester', title: t('approval.field_requester') },
  { key: 'resourceType', title: t('approval.field_resourceType') },
  { key: 'resourceID', title: t('approval.field_resourceID') },
  { key: 'status', title: t('approval.field_status'), slot: 'cell-status' },
  { key: 'currentStep', title: t('approval.field_currentStep') },
  { key: 'createdAt', title: t('approval.field_createdAt') },
  { key: 'actions', title: t('approval.col_actions'), slot: 'cell-actions', width: '70px' }
])

const pendingColumns = computed(() => [
  { key: 'id', title: t('approval.field_id'), width: '120px' },
  { key: 'flowName', title: t('approval.field_flowName') },
  { key: 'requester', title: t('approval.field_requester') },
  { key: 'resourceType', title: t('approval.field_resourceType') },
  { key: 'resourceID', title: t('approval.field_resourceID') },
  { key: 'currentStep', title: t('approval.field_currentStep') },
  { key: 'createdAt', title: t('approval.field_createdAt') },
  { key: 'actions', title: t('approval.col_actions'), slot: 'cell-actions', width: '220px' }
])

const historyColumns = computed(() => [
  { key: 'step', title: t('approval.field_step') },
  { key: 'approver', title: t('approval.field_approver') },
  { key: 'action', title: t('approval.field_action'), slot: 'cell-action' },
  { key: 'comment', title: t('approval.field_comment') },
  { key: 'timestamp', title: t('approval.field_timestamp') }
])

function statusClass(s) {
  if (s === 'approved') return 'ok'
  if (s === 'rejected') return 'off'
  if (s === 'cancelled') return 'off'
  if (s === 'pending') return 'warn'
  return 'off'
}

function statusText(s) {
  if (s === 'pending') return t('approval.status_pending')
  if (s === 'approved') return t('approval.status_approved')
  if (s === 'rejected') return t('approval.status_rejected')
  if (s === 'cancelled') return t('approval.status_cancelled')
  return s || '—'
}

function switchTab(key) {
  activeTab.value = key
  if (key === 'requests' && !store.requests.length) store.fetchRequests()
  else if (key === 'pending' && !store.pending.length) store.fetchPending()
}

function searchRequests() {
  const params = {}
  if (statusFilter.value) params.status = statusFilter.value
  store.fetchRequests(params)
}

// ---------- approve/reject/cancel ----------
const actionForm = ref(null)
const actionError = ref('')
const actionTitle = ref('')

function openApprove(row) {
  actionError.value = ''
  actionTitle.value = t('approval.approve_title')
  actionForm.value = { type: 'approve', id: row.id, comment: '' }
}

function openReject(row) {
  actionError.value = ''
  actionTitle.value = t('approval.reject_title')
  actionForm.value = { type: 'reject', id: row.id, comment: '' }
}

function openCancel(row) {
  actionError.value = ''
  actionTitle.value = t('approval.cancel_title')
  actionForm.value = { type: 'cancel', id: row.id, comment: '' }
}

async function onActionSubmit() {
  actionError.value = ''
  const { type, id, comment } = actionForm.value
  const body = { comment }
  try {
    if (type === 'approve') await store.approve(id, body)
    else if (type === 'reject') await store.reject(id, body)
    else if (type === 'cancel') await store.cancel(id, body)
    actionForm.value = null
    toast.success(t('approval.action_success'))
  } catch (e) {
    actionError.value = e.j?.error || t('approval.action_failed')
  }
}

// ---------- 审批历史 ----------
const historyOpen = ref(false)

async function viewHistory(row) {
  historyOpen.value = true
  await store.fetchHistory(row.id)
}

onMounted(() => {
  store.fetchRequests().catch((e) => {
    errorConfirm.message = e.j?.error || t('error.approvalRequestsFailed')
    errorConfirm.show = true
  })
})
</script>

<style scoped>
.tabbar {
  display: flex; gap: 4px; margin-bottom: 16px;
  border-bottom: 1px solid var(--border);
}
.tab {
  background: transparent; border: none; border-bottom: 2px solid transparent;
  padding: 8px 16px; cursor: pointer; color: var(--text-2);
  font-size: 13px; font-weight: 500;
}
.tab.active { color: var(--accent); border-bottom-color: var(--accent); }
.tab:hover { color: var(--text); }

.btnbar { display: flex; gap: 8px; margin-bottom: 12px; }
.row-actions { display: flex; gap: 4px; }
.status-pill {
  display: inline-flex; align-items: center;
  padding: 1px 9px; border-radius: 999px; font-size: 12px; font-weight: 600;
}
.status-pill.ok { background: var(--accent-soft); color: var(--accent); }
.status-pill.off { background: var(--surface-3); color: var(--text-3); }
.status-pill.warn { background: var(--warn-soft, #fef3c7); color: var(--warn, #b45309); }

.filter-bar { display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin: 10px 0; }
.filter-bar .field { display: flex; flex-direction: column; gap: 5px; }
.filter-bar .field > label { margin: 0; font-size: 12px; color: var(--text-2); }

.poll-err {
  padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}

.entity-form { display: flex; flex-direction: column; gap: 12px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field > label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.btnbar { margin-top: 8px; }
</style>