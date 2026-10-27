<template>
  <PageShell :title="$t('audit_events.title')" :desc="$t('audit_events.subtitle')" testid="audit-events" :error="loadErr">
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
        <label>
          <input type="checkbox" v-model="highRiskOnly" />
          {{ $t('audit_events.high_risk_only') }}
        </label>
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
        <Button variant="primary" size="md" native-type="submit" data-testid="audit-search-btn">
          <Icon name="search" :size="14" />
          {{ $t('audit_events.search') }}
        </Button>
        <Button variant="ghost" size="md" native-type="button" @click="onReset">{{ $t('audit_events.reset') }}</Button>
        <!-- 导出：GET /audit/export 下载 JSON 文件 -->
        <Button variant="ghost" size="md" native-type="button" data-testid="audit-export-btn" :disabled="exporting" @click="onExport">
          <Icon name="clipboard" :size="14" />
          {{ exporting ? $t('audit_events.exporting') : $t('audit_events.export') }}
        </Button>
        <Button variant="ghost" size="md" native-type="button" data-testid="audit-verify-btn" :disabled="verifying" @click="onVerify">
          {{ verifying ? $t('audit_events.verifying') : $t('audit_events.verify_chain') }}
        </Button>
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
    <div v-if="verifyResult" class="verify-result" :class="verifyResult.ok ? 'ok' : 'err'">
      <strong>{{ verifyResult.ok ? $t('audit_events.verify_ok') : $t('audit_events.verify_fail') }}</strong>
      <span v-if="verifyResult.checked"> — {{ $t('audit_events.verify_checked', { n: verifyResult.checked }) }}</span>
      <span v-if="!verifyResult.ok && verifyResult.firstBadID"> ({{ verifyResult.firstBadID }}: {{ verifyResult.reason }})</span>
    </div>

    <!-- 错误提示（替代 alert） -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="audit-events-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </PageShell>
</template>

<script setup>
// 审计事件页 — 只读：筛选表单（时间范围/动作/用户）+ DataTable + 前端分页 + 导出下载
import { ref, reactive, computed, onMounted } from 'vue'
import PageShell from '@/components/PageShell.vue'
import { getEvents, exportEvents, getHighRiskAudits, verifyAuditChain } from '@/api/audit'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import Pagination from '@/components/Pagination.vue'
import { fmtTime } from '@/composables/useFormatTime'
import Button from "@/components/ui/Button.vue"

const events = ref([])
const loading = ref(false)
const loadErr = ref('')
const exporting = ref(false)
const verifying = ref(false)
const highRiskOnly = ref(false)
const verifyResult = ref(null)
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
  loadErr.value = ''
  try {
    const params = buildParams()
    const r = highRiskOnly.value
      ? await getHighRiskAudits(params)
      : await getEvents(params)
    events.value = (r && (r.events || r)) || []
    page.value = 1
  } catch (e) {
    // 此前静默吞错（只见空表）——统一骨架批次补上可见的加载错误
    events.value = []
    loadErr.value = e.j?.error || t('common.load_failed')
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

// 校验哈希链
async function onVerify() {
  verifying.value = true
  verifyResult.value = null
  try {
    const r = await verifyAuditChain({ limit: 1000 })
    verifyResult.value = r
  } catch (e) {
    verifyResult.value = { ok: false, reason: e?.message || 'verify failed' }
  } finally {
    verifying.value = false
  }
}
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
.verify-result { margin-top: 10px; padding: 8px 12px; border-radius: var(--radius-sm); font-size: 12.5px; }
.verify-result.ok { background: var(--success-bg); color: var(--teal); }
.verify-result.err { background: var(--fail-soft); color: var(--fail); }
</style>
