<template>
  <div>
    <h2 data-testid="audits-title">{{ $t('audits.title') }}</h2>
    <p class="muted">{{ $t('audits.subtitle') }}</p>

    <!-- 筛选表单：action / user / 时间范围 + 分页 -->
    <form class="filter-bar" @submit.prevent="onSearch">
      <div class="field">
        <label>{{ $t('audits.filter_from') }}</label>
        <input v-model="filters.from" type="date" />
      </div>
      <div class="field">
        <label>{{ $t('audits.filter_to') }}</label>
        <input v-model="filters.to" type="date" />
      </div>
      <div class="field">
        <label>{{ $t('audits.filter_action') }}</label>
        <input v-model.trim="filters.action" type="text" placeholder="e.g. device_create" />
      </div>
      <div class="field">
        <label>{{ $t('audits.filter_user') }}</label>
        <input v-model.trim="filters.user" type="text" />
      </div>
      <div class="field">
        <label>{{ $t('audits.filter_limit') }}</label>
        <select v-model.number="filters.limit">
          <option :value="50">50</option>
          <option :value="100">100</option>
          <option :value="200">200</option>
          <option :value="500">500</option>
        </select>
      </div>
      <div class="filter-actions">
        <button type="submit" class="primary" data-testid="audits-search-btn">
          <Icon name="search" :size="14" /> {{ $t('audits.search') }}
        </button>
        <button type="button" class="outline" @click="onReset">{{ $t('audits.reset') }}</button>
      </div>
    </form>

    <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
    <DataTable
      v-else
      :columns="columns"
      :rows="pagedAudits"
      row-key="id"
      :empty-text="$t('audits.empty')"
    >
      <template #cell-timestamp="{ value }">{{ value || '—' }}</template>
      <template #cell-action="{ value }"><code class="action-code">{{ value }}</code></template>
      <template #cell-user="{ value }"><code>{{ value }}</code></template>
      <template #cell-ip="{ value }"><code>{{ value || '—' }}</code></template>
      <template #cell-details="{ value }">
        <span v-if="value" class="detail-text" :title="typeof value === 'string' ? value : JSON.stringify(value)">{{ formatDetails(value) }}</span>
        <span v-else class="muted">—</span>
      </template>
    </DataTable>

    <!-- 前端分页 -->
    <Pagination
      v-if="audits.length > pageSize"
      :page="page"
      :page-size="pagedAudits.length"
      :limit="pageSize"
      @prev="page--"
      @next="page++"
    />
    <p v-else-if="audits.length" class="muted count-hint">
      {{ $t('audits.count_hint', { n: audits.length }) }}
    </p>

    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="audits-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// 审计检索页 — 筛选 action/user/时间范围 + 分页列表
import { ref, reactive, computed, onMounted } from 'vue'
import { getAudits } from '@/api/audit'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import Pagination from '@/components/Pagination.vue'

const audits = ref([])
const loading = ref(false)
const page = ref(1)
const pageSize = 20

const filters = reactive({ from: '', to: '', action: '', user: '', limit: 100 })
const errorConfirm = reactive({ show: false, message: '' })

const columns = [
  { key: 'timestamp', title: t('audits.col_timestamp'), slot: 'cell-timestamp', width: '170px' },
  { key: 'action', title: t('audits.col_action'), slot: 'cell-action' },
  { key: 'user', title: t('audits.col_user'), slot: 'cell-user', width: '120px' },
  { key: 'resource', title: t('audits.col_resource') },
  { key: 'tenantID', title: t('audits.col_tenantID'), width: '120px' },
  { key: 'ip', title: t('audits.col_ip'), slot: 'cell-ip', width: '130px' },
  { key: 'userAgent', title: t('audits.col_userAgent') },
  { key: 'details', title: t('audits.col_details'), slot: 'cell-details' }
]

const pagedAudits = computed(() => {
  const start = (page.value - 1) * pageSize
  return audits.value.slice(start, start + pageSize)
})

function formatDetails(v) {
  if (v == null) return '—'
  if (typeof v === 'string') return v
  try { return JSON.stringify(v) } catch { return String(v) }
}

function buildParams() {
  const p = { action: filters.action, user: filters.user, limit: filters.limit }
  if (filters.from) p.from = new Date(`${filters.from}T00:00:00`).toISOString()
  if (filters.to) p.to = new Date(`${filters.to}T23:59:59`).toISOString()
  return p
}

async function fetchAudits() {
  loading.value = true
  try {
    const r = await getAudits(buildParams())
    audits.value = (r && r.audits) || []
    page.value = 1
  } catch (e) {
    audits.value = []
    errorConfirm.message = e.j?.error || t('error.auditsFailed')
    errorConfirm.show = true
  } finally {
    loading.value = false
  }
}

function onSearch() {
  fetchAudits()
}

function onReset() {
  filters.from = ''
  filters.to = ''
  filters.action = ''
  filters.user = ''
  filters.limit = 100
  fetchAudits()
}

onMounted(() => {
  fetchAudits()
})
</script>

<style scoped>
.filter-bar {
  display: flex; flex-wrap: wrap; gap: 12px; align-items: flex-end;
  padding: 14px; margin: 14px 0 4px;
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
}
.field { display: flex; flex-direction: column; gap: 5px; }
.field > label { margin: 0; font-size: 12px; color: var(--text-2); }
.field input, .field select {
  min-width: 140px; padding: 6px 10px;
  border: 1px solid var(--border); border-radius: var(--radius-sm);
  background: var(--surface); color: var(--text); font-size: 13px;
}
.filter-actions { display: flex; gap: 8px; margin-left: auto; }
.action-code {
  font-size: 12px; padding: 2px 8px; border-radius: 999px;
  background: var(--accent-soft); color: var(--accent);
}
.detail-text {
  display: inline-block; max-width: 260px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  vertical-align: bottom; font-size: 12.5px;
}
.count-hint { margin-top: 10px; font-size: 12.5px; }
</style>