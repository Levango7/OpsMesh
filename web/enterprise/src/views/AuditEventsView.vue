<template>
  <div>
    <h2>{{ $t('audit_events.title') }}</h2>
    <p class="muted">{{ $t('audit_events.subtitle') }}</p>

    <!-- 筛选表单：时间范围 / 动作 / 用户 -->
    <form class="filter-bar" @submit.prevent="onSearch">
      <div class="field">
        <label>{{ $t('audit_events.filter_from') }}</label>
        <input v-model="filters.from" type="date" />
      </div>
      <div class="field">
        <label>{{ $t('audit_events.filter_to') }}</label>
        <input v-model="filters.to" type="date" />
      </div>
      <div class="field">
        <label>{{ $t('audit_events.filter_action') }}</label>
        <input v-model.trim="filters.action" type="text" placeholder="e.g. apikey_create" />
      </div>
      <div class="field">
        <label>{{ $t('audit_events.filter_user') }}</label>
        <input v-model.trim="filters.user" type="text" />
      </div>
      <div class="field">
        <label>{{ $t('audit_events.filter_limit') }}</label>
        <select v-model.number="filters.limit">
          <option :value="50">50</option>
          <option :value="100">100</option>
          <option :value="200">200</option>
          <option :value="500">500</option>
        </select>
      </div>
      <div class="filter-actions">
        <button type="submit" class="primary" data-testid="audit-search-btn">
          <Icon name="search" :size="14" />
          {{ $t('audit_events.search') }}
        </button>
        <button type="button" class="outline" @click="onReset">{{ $t('audit_events.reset') }}</button>
        <!-- 导出：GET /audit/export 下载 JSON 文件 -->
        <button type="button" class="outline" data-testid="audit-export-btn" :disabled="exporting" @click="onExport">
          <Icon name="clipboard" :size="14" />
          {{ exporting ? $t('audit_events.exporting') : $t('audit_events.export') }}
        </button>
      </div>
    </form>

    <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
    <DataTable
      v-else
      :columns="columns"
      :rows="pagedEvents"
      row-key="id"
      :empty-text="$t('audit_events.empty')"
    >
      <template #cell-createdAt="{ value }">{{ fmtTime(value) }}</template>
      <template #cell-action="{ value }"><code class="action-code">{{ value }}</code></template>
      <template #cell-userID="{ value }"><code>{{ value }}</code></template>
      <template #cell-target="{ value }">
        <span v-if="value" class="target-text">{{ value }}</span>
        <span v-else class="muted">—</span>
      </template>
      <template #cell-detail="{ value }">
        <span v-if="value" class="detail-text" :title="value">{{ value }}</span>
        <span v-else class="muted">—</span>
      </template>
    </DataTable>

    <!-- 前端分页（后端 limit 一次返回） -->
    <Pagination
      v-if="events.length > pageSize"
      :page="page"
      :page-size="pagedEvents.length"
      :limit="pageSize"
      @prev="page--"
      @next="page++"
    />
    <p v-else-if="events.length" class="muted count-hint">
      {{ $t('audit_events.count_hint', { n: events.length }) }}
    </p>

    <!-- 错误提示（替代 alert） -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="audit-events-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// 审计事件页 — 只读：筛选表单（时间范围/动作/用户）+ DataTable + 前端分页 + 导出下载
import { ref, reactive, computed, onMounted } from 'vue'
import { getEvents, exportEvents } from '@/api/audit'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import Pagination from '@/components/Pagination.vue'
import { fmtTime } from '@/composables/useFormatTime'

const events = ref([])
const loading = ref(false)
const exporting = ref(false)
const page = ref(1)
const pageSize = 20 // 前端分页大小（后端按 limit 一次拉取）

// 筛选条件（from/to 为 yyyy-mm-dd，提交时转 RFC3339）
const filters = reactive({ from: '', to: '', action: '', user: '', limit: 100 })

// 错误提示弹窗（替代 alert）
const errorConfirm = reactive({ show: false, message: '' })

const columns = [
  { key: 'createdAt', title: t('audit_events.created_at'), slot: 'cell-createdAt', width: '160px' },
  { key: 'action', title: t('audit_events.action'), slot: 'cell-action' },
  { key: 'userID', title: t('audit_events.user'), slot: 'cell-userID', width: '120px' },
  { key: 'target', title: t('audit_events.target'), slot: 'cell-target' },
  { key: 'detail', title: t('audit_events.detail'), slot: 'cell-detail' }
]

// 当前页切片
const pagedEvents = computed(() => {
  const start = (page.value - 1) * pageSize
  return events.value.slice(start, start + pageSize)
})

// 组装查询参数：空值剔除；from/to 补全为当天起止的 RFC3339
function buildParams() {
  const p = { action: filters.action, user: filters.user, limit: filters.limit }
  if (filters.from) p.from = new Date(`${filters.from}T00:00:00`).toISOString()
  if (filters.to) p.to = new Date(`${filters.to}T23:59:59`).toISOString()
  return p
}

async function fetchEvents() {
  loading.value = true
  try {
    const r = await getEvents(buildParams())
    events.value = (r && r.events) || []
    page.value = 1
  } catch {
    events.value = []
  } finally {
    loading.value = false
  }
}

function onSearch() {
  fetchEvents()
}

// 重置筛选后重新拉取
function onReset() {
  filters.from = ''
  filters.to = ''
  filters.action = ''
  filters.user = ''
  filters.limit = 100
  fetchEvents()
}

// 导出：调用 GET /audit/export 拿 JSON 数组，前端生成文件下载
async function onExport() {
  exporting.value = true
  try {
    // 导出不受分页影响，直接用当前筛选条件（limit 提到 1000 对齐后端默认）
    const data = await exportEvents({ ...buildParams(), limit: 1000 })
    const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `audit-export-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-')}.json`
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(url)
  } catch (e) {
    errorConfirm.message = (e && e.message) || t('audit_events.export_failed')
    errorConfirm.show = true
  } finally {
    exporting.value = false
  }
}

onMounted(() => {
  fetchEvents()
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
.target-text, .detail-text {
  display: inline-block; max-width: 260px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  vertical-align: bottom; font-size: 12.5px;
}
.count-hint { margin-top: 10px; font-size: 12.5px; }
</style>
