<template>
  <div>
    <h2>{{ $t('tickets.title') }}</h2>
    <p class="muted">{{ $t('tickets.subtitle') }}</p>

    <div class="btnbar">
      <button class="primary" @click="openCreate">
        <Icon name="add" :size="14" />
        {{ $t('tickets.add') }}
      </button>
      <button class="outline" @click="fetchTickets">
        <Icon name="refresh" :size="14" />
        {{ $t('common.refresh') }}
      </button>
    </div>

    <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
    <div v-else>
      <DataTable :columns="columns" :rows="tickets" row-key="id" :empty-text="$t('tickets.empty')">
        <template #cell-title="{ row }"><span class="t-title">{{ row.title }}</span></template>
        <template #cell-status="{ value }">
          <span class="status-pill" :class="statusClass(value)">{{ statusText(value) }}</span>
        </template>
        <template #cell-priority="{ value }">
          <span class="status-pill" :class="priorityClass(value)">{{ priorityText(value) }}</span>
        </template>
        <template #cell-category="{ value }">{{ categoryText(value) }}</template>
        <template #cell-createdAt="{ value }">{{ fmtTime(value) }}</template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <button
              v-if="row.status !== 'closed' && row.status !== 'resolved'"
              class="xs outline"
              :title="$t('tickets.close')"
              @click="onClose(row)"
            ><Icon name="close" :size="13" /></button>
            <button class="xs outline" @click="openEdit(row)"><Icon name="edit" :size="13" /></button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- 新增/编辑抽屉 -->
    <DetailDrawer :open="!!form" :title="form && form.id ? $t('tickets.edit') : $t('tickets.add')" @close="form = null">
      <form v-if="form" class="ticket-form" @submit.prevent="onSave">
        <div class="field">
          <label>{{ $t('tickets.new_title') }}</label>
          <input v-model.trim="form.title" type="text" required />
        </div>
        <div class="field">
          <label>{{ $t('tickets.new_description') }}</label>
          <textarea v-model.trim="form.description" rows="4"></textarea>
        </div>
        <div class="field">
          <label>{{ $t('tickets.new_priority') }}</label>
          <select v-model="form.priority">
            <option value="low">{{ $t('tickets.p_low') }}</option>
            <option value="medium">{{ $t('tickets.p_medium') }}</option>
            <option value="high">{{ $t('tickets.p_high') }}</option>
            <option value="urgent">{{ $t('tickets.p_urgent') }}</option>
          </select>
        </div>
        <div class="field">
          <label>{{ $t('tickets.new_category') }}</label>
          <select v-model="form.category">
            <option value="incident">{{ $t('tickets.c_incident') }}</option>
            <option value="change">{{ $t('tickets.c_change') }}</option>
            <option value="request">{{ $t('tickets.c_request') }}</option>
            <option value="problem">{{ $t('tickets.c_problem') }}</option>
          </select>
        </div>
        <div class="field">
          <label>{{ $t('tickets.new_status') }}</label>
          <select v-model="form.status" :disabled="!form.id">
            <option value="open">{{ $t('tickets.s_open') }}</option>
            <option value="in_progress">{{ $t('tickets.s_in_progress') }}</option>
            <option value="resolved">{{ $t('tickets.s_resolved') }}</option>
          </select>
        </div>
        <div class="field">
          <label>{{ $t('tickets.new_assignee') }}</label>
          <input v-model.trim="form.assigneeID" type="text" />
        </div>
        <div class="field">
          <label>{{ $t('tickets.new_device') }}</label>
          <input v-model.trim="form.relatedDevice" type="text" />
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('tickets.save') }}</button>
          <button type="button" class="outline" @click="form = null">{{ $t('tickets.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 关闭确认（替代 confirm） -->
    <ConfirmModal
      v-model="closeConfirm.show"
      data-testid="tickets-close-confirm-modal"
      :title="$t('tickets.close')"
      :message="$t('tickets.confirm_close')"
      @confirm="onCloseConfirm"
    />
    <!-- 错误提示（替代 alert） -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="tickets-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// 工单管理页 — 列表 + 创建/编辑 + 关闭工单
import { ref, computed, reactive, onMounted } from 'vue'
import { getTickets, createTicket, updateTicket, closeTicket } from '@/api/ticket'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import { fmtTime } from '@/composables/useFormatTime'

const tickets = ref([])
const loading = ref(false)
const form = ref(null)
const formError = ref('')

// 关闭确认弹窗（替代 confirm）
const closeConfirm = reactive({ show: false, row: null })
// 错误提示弹窗（替代 alert）
const errorConfirm = reactive({ show: false, message: '' })

const columns = computed(() => [
  { key: 'title', title: t('tickets.title_col'), slot: 'cell-title' },
  { key: 'status', title: t('tickets.status'), slot: 'cell-status' },
  { key: 'priority', title: t('tickets.priority'), slot: 'cell-priority' },
  { key: 'category', title: t('tickets.category'), slot: 'cell-category' },
  { key: 'assigneeID', title: t('tickets.assignee') },
  { key: 'createdAt', title: t('tickets.created_at'), slot: 'cell-createdAt' },
  { key: 'actions', title: t('tickets.actions'), slot: 'cell-actions', width: '80px' }
])

function statusText(s) {
  const m = { open: 's_open', in_progress: 's_in_progress', resolved: 's_resolved', closed: 's_closed' }
  return m[s] ? t(`tickets.${m[s]}`) : (s || '—')
}
function statusClass(s) {
  if (s === 'closed') return 'off'
  if (s === 'resolved') return 'ok'
  if (s === 'in_progress') return 'warn'
  return 'info'
}
function priorityText(p) {
  const m = { low: 'p_low', medium: 'p_medium', high: 'p_high', urgent: 'p_urgent' }
  return m[p] ? t(`tickets.${m[p]}`) : (p || '—')
}
function priorityClass(p) {
  if (p === 'urgent') return 'bad'
  if (p === 'high') return 'warn'
  return 'info'
}
function categoryText(c) {
  const m = { incident: 'c_incident', change: 'c_change', request: 'c_request', problem: 'c_problem' }
  return m[c] ? t(`tickets.${m[c]}`) : (c || '—')
}

async function fetchTickets() {
  loading.value = true
  try {
    const r = await getTickets()
    tickets.value = (r && r.tickets) || []
  } catch (e) {
    tickets.value = []
  } finally {
    loading.value = false
  }
}

function openCreate() {
  formError.value = ''
  form.value = {
    id: null, title: '', description: '',
    priority: 'medium', category: 'incident', status: 'open',
    assigneeID: '', relatedDevice: ''
  }
}

function openEdit(row) {
  formError.value = ''
  form.value = {
    id: row.id,
    title: row.title || '',
    description: row.description || '',
    priority: row.priority || 'medium',
    category: row.category || 'incident',
    status: row.status || 'open',
    assigneeID: row.assigneeID || '',
    relatedDevice: row.relatedDevice || ''
  }
}

async function onSave() {
  formError.value = ''
  try {
    const body = {
      title: form.value.title,
      description: form.value.description,
      priority: form.value.priority,
      category: form.value.category,
      assigneeID: form.value.assigneeID,
      relatedDevice: form.value.relatedDevice
    }
    if (form.value.id) {
      // 更新时带 status（后端 PUT 支持 status 字段流转）
      await updateTicket(form.value.id, { ...body, status: form.value.status })
    } else {
      await createTicket(body)
    }
    form.value = null
    toast.success(t('tickets.saved'))
    await fetchTickets()
  } catch (e) {
    formError.value = e.j?.error || t('tickets.save_failed')
  }
}

function onClose(row) {
  closeConfirm.row = row
  closeConfirm.show = true
}

async function onCloseConfirm() {
  const row = closeConfirm.row
  if (!row) return
  try {
    await closeTicket(row.id)
    toast.success(t('tickets.closed'))
    await fetchTickets()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('tickets.close_failed')
    errorConfirm.show = true
  }
}

onMounted(fetchTickets)
</script>

<style scoped>
.row-actions { display: flex; gap: 4px; }
.t-title { font-weight: 600; }
.status-pill {
  display: inline-flex; align-items: center;
  padding: 1px 9px; border-radius: 999px; font-size: 12px; font-weight: 600;
}
.status-pill.ok { background: var(--accent-soft); color: var(--accent); }
.status-pill.off { background: var(--surface-3); color: var(--text-3); }
.status-pill.bad { background: rgba(214,69,56,.12); color: #d64538; }
.status-pill.warn { background: rgba(214,158,46,.14); color: #d69e2e; }
.status-pill.info { background: rgba(90,141,238,.12); color: #5a8dee; }
.ticket-form { display: flex; flex-direction: column; gap: 12px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field > label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.field textarea { resize: vertical; }
.btnbar { margin-top: 8px; }
</style>
