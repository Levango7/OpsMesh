<template>
  <div>
    <h2>{{ $t('billing.title') }}</h2>
    <p class="muted">{{ $t('billing.subtitle') }}</p>

    <!-- tab 分区：订阅计划 / 订阅 / 账单 / 用量 -->
    <div class="btnbar tabs">
      <button
        v-for="tb in tabs"
        :key="tb.key"
        :class="['tab-btn', { active: tab === tb.key }]"
        @click="tab = tb.key"
      >
        {{ tb.label }}
      </button>
    </div>

    <!-- ============ 订阅计划 ============ -->
    <template v-if="tab === 'plans'">
      <div class="btnbar">
        <button class="primary" data-testid="billing-add-plan-btn" @click="openPlanCreate">
          <Icon name="add" :size="14" />
          {{ $t('billing.add_plan') }}
        </button>
        <button class="outline" @click="fetchPlans">
          <Icon name="refresh" :size="14" />
          {{ $t('common.refresh') }}
        </button>
      </div>
      <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
      <DataTable v-else :columns="planCols" :rows="plans" row-key="id" :empty-text="$t('billing.empty_plans')">
        <template #cell-name="{ value }"><code>{{ value }}</code></template>
        <template #cell-price="{ value }">{{ fmtPrice(value) }}</template>
        <template #cell-interval="{ value }">{{ fmtInterval(value) }}</template>
        <template #cell-features="{ value }">{{ (value || []).join('、') || '—' }}</template>
        <template #cell-createdAt="{ value }">{{ fmtTime(value) }}</template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <button class="xs outline" @click="openPlanEdit(row)"><Icon name="edit" :size="13" /></button>
            <button class="xs danger" @click="onDeletePlan(row)"><Icon name="delete" :size="13" /></button>
          </div>
        </template>
      </DataTable>
    </template>

    <!-- ============ 订阅 ============ -->
    <template v-else-if="tab === 'subscriptions'">
      <div class="btnbar">
        <button class="primary" data-testid="billing-add-sub-btn" @click="openSubCreate">
          <Icon name="add" :size="14" />
          {{ $t('billing.add_sub') }}
        </button>
        <button class="outline" @click="fetchSubs">
          <Icon name="refresh" :size="14" />
          {{ $t('common.refresh') }}
        </button>
      </div>
      <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
      <DataTable v-else :columns="subCols" :rows="subs" row-key="id" :empty-text="$t('billing.empty_subs')">
        <template #cell-planID="{ row }">{{ planName(row.planID) }}</template>
        <template #cell-status="{ value }">
          <StatusBadge :status="value === 'active' ? 'ok' : 'fail'" :text="fmtSubStatus(value)" />
        </template>
        <template #cell-startedAt="{ value }">{{ fmtTime(value) }}</template>
        <template #cell-expiresAt="{ value }">{{ fmtTime(value, '—') }}</template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <button class="xs outline" @click="openSubEdit(row)"><Icon name="edit" :size="13" /></button>
            <button class="xs danger" @click="onDeleteSub(row)"><Icon name="delete" :size="13" /></button>
          </div>
        </template>
      </DataTable>
    </template>

    <!-- ============ 账单（只读） ============ -->
    <template v-else-if="tab === 'invoices'">
      <div class="btnbar">
        <button class="outline" @click="fetchInvoices">
          <Icon name="refresh" :size="14" />
          {{ $t('common.refresh') }}
        </button>
      </div>
      <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
      <DataTable v-else :columns="invoiceCols" :rows="invoices" row-key="id" :empty-text="$t('billing.empty_invoices')">
        <template #cell-amount="{ value }">{{ fmtPrice(value) }}</template>
        <template #cell-status="{ value }">
          <StatusBadge
            :status="value === 'paid' ? 'ok' : (value === 'overdue' ? 'fail' : 'info')"
            :text="fmtInvoiceStatus(value)"
          />
        </template>
        <template #cell-periodStart="{ value }">{{ fmtTime(value) }}</template>
        <template #cell-periodEnd="{ value }">{{ fmtTime(value) }}</template>
        <template #cell-createdAt="{ value }">{{ fmtTime(value) }}</template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <button class="xs outline" @click="openInvoiceDetail(row)">{{ $t('billing.view_items') }}</button>
          </div>
        </template>
      </DataTable>
    </template>

    <!-- ============ 用量统计（只读） ============ -->
    <template v-else>
      <div class="btnbar">
        <button class="outline" @click="fetchUsage">
          <Icon name="refresh" :size="14" />
          {{ $t('common.refresh') }}
        </button>
      </div>
      <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
      <div v-else-if="!usage" class="muted">{{ $t('billing.empty_usage') }}</div>
      <div v-else class="usage-grid">
        <div class="usage-card">
          <div class="usage-num">{{ usage.deviceCount ?? 0 }}</div>
          <div class="usage-label">{{ $t('billing.usage_devices') }}</div>
        </div>
        <div class="usage-card">
          <div class="usage-num">{{ usage.taskCount ?? 0 }}</div>
          <div class="usage-label">{{ $t('billing.usage_tasks') }}</div>
        </div>
        <div class="usage-card">
          <div class="usage-num">{{ usage.alertCount ?? 0 }}</div>
          <div class="usage-label">{{ $t('billing.usage_alerts') }}</div>
        </div>
        <div class="usage-card">
          <div class="usage-num">{{ usage.metricsCount ?? 0 }}</div>
          <div class="usage-label">{{ $t('billing.usage_metrics') }}</div>
        </div>
        <div class="usage-card wide">
          <div class="usage-num small">{{ fmtTime(usage.calculatedAt) }}</div>
          <div class="usage-label">{{ $t('billing.usage_calculated_at') }}</div>
        </div>
      </div>
    </template>

    <!-- 计划 新增/编辑抽屉 -->
    <DetailDrawer :open="!!planForm" :title="planForm && planForm.id ? $t('billing.edit_plan') : $t('billing.add_plan')" @close="planForm = null">
      <form v-if="planForm" class="entity-form" @submit.prevent="onSavePlan">
        <div class="field">
          <label>{{ $t('billing.plan_name') }}</label>
          <input v-model.trim="planForm.name" type="text" required />
        </div>
        <div class="field">
          <label>{{ $t('billing.plan_price') }}</label>
          <input v-model.number="planForm.price" type="number" min="0" step="1" required />
        </div>
        <div class="field">
          <label>{{ $t('billing.plan_interval') }}</label>
          <select v-model="planForm.interval">
            <option value="monthly">{{ $t('billing.interval_monthly') }}</option>
            <option value="yearly">{{ $t('billing.interval_yearly') }}</option>
          </select>
        </div>
        <div class="field">
          <label>{{ $t('billing.plan_features') }}</label>
          <textarea v-model.trim="planForm.featuresText" rows="3" :placeholder="$t('billing.plan_features_ph')" />
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('billing.save') }}</button>
          <button type="button" class="outline" @click="planForm = null">{{ $t('billing.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 订阅 新增/编辑抽屉 -->
    <DetailDrawer :open="!!subForm" :title="subForm && subForm.id ? $t('billing.edit_sub') : $t('billing.add_sub')" @close="subForm = null">
      <form v-if="subForm" class="entity-form" @submit.prevent="onSaveSub">
        <div class="field">
          <label>{{ $t('billing.sub_plan') }}</label>
          <select v-model="subForm.planID" required>
            <option value="" disabled>{{ $t('billing.sub_plan_ph') }}</option>
            <option v-for="p in plans" :key="p.id" :value="p.id">{{ p.name }}</option>
          </select>
        </div>
        <div class="field">
          <label>{{ $t('billing.sub_status') }}</label>
          <select v-model="subForm.status">
            <option value="active">{{ $t('billing.sub_status_active') }}</option>
            <option value="canceled">{{ $t('billing.sub_status_canceled') }}</option>
            <option value="expired">{{ $t('billing.sub_status_expired') }}</option>
          </select>
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('billing.save') }}</button>
          <button type="button" class="outline" @click="subForm = null">{{ $t('billing.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 账单明细抽屉（只读） -->
    <DetailDrawer :open="!!invoiceDetail" :title="$t('billing.invoice_items')" @close="invoiceDetail = null">
      <div v-if="invoiceDetail">
        <table>
          <thead>
            <tr>
              <th>{{ $t('billing.item_name') }}</th>
              <th>{{ $t('billing.item_qty') }}</th>
              <th>{{ $t('billing.item_amount') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(it, i) in invoiceDetail.items || []" :key="i">
              <td>{{ it.name }}</td>
              <td>{{ it.quantity }}</td>
              <td>{{ fmtPrice(it.amount) }}</td>
            </tr>
            <tr v-if="!(invoiceDetail.items || []).length">
              <td colspan="3" class="muted">{{ $t('common.none') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </DetailDrawer>

    <!-- 删除确认（替代 confirm） -->
    <ConfirmModal
      v-model="deleteConfirm.show"
      data-testid="billing-delete-confirm-modal"
      :title="$t('common.delete')"
      :message="deleteConfirm.msg"
      @confirm="onDeleteConfirm"
    />
    <!-- 错误提示（替代 alert） -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="billing-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// 计费管理页 — tab 分区：订阅计划 / 订阅 / 账单（只读）/ 用量统计（只读）
import { ref, reactive, computed, watch, onMounted } from 'vue'
import { listPlans, createPlan, updatePlan, deletePlan, listSubscriptions, createSubscription, updateSubscription, deleteSubscription, listInvoices, getUsage } from '@/api/billing'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { fmtTime } from '@/composables/useFormatTime'

const tab = ref('plans')
const loading = ref(false)
const formError = ref('')

const plans = ref([])
const subs = ref([])
const invoices = ref([])
const usage = ref(null)

const planForm = ref(null)
const subForm = ref(null)
const invoiceDetail = ref(null)

// 删除确认弹窗（替代 confirm）：domain 标记当前删除的实体类型
const deleteConfirm = reactive({ show: false, row: null, domain: '', msg: '' })
// 错误提示弹窗（替代 alert）
const errorConfirm = reactive({ show: false, message: '' })

const tabs = computed(() => [
  { key: 'plans', label: t('billing.tab_plans') },
  { key: 'subscriptions', label: t('billing.tab_subscriptions') },
  { key: 'invoices', label: t('billing.tab_invoices') },
  { key: 'usage', label: t('billing.tab_usage') }
])

const planCols = [
  { key: 'name', title: t('billing.plan_name'), slot: 'cell-name' },
  { key: 'price', title: t('billing.plan_price'), slot: 'cell-price', width: '110px' },
  { key: 'interval', title: t('billing.plan_interval'), slot: 'cell-interval', width: '90px' },
  { key: 'features', title: t('billing.plan_features'), slot: 'cell-features' },
  { key: 'createdAt', title: t('billing.created_at'), slot: 'cell-createdAt', width: '150px' },
  { key: 'actions', title: t('billing.actions'), slot: 'cell-actions', width: '90px' }
]

const subCols = [
  { key: 'planID', title: t('billing.sub_plan'), slot: 'cell-planID' },
  { key: 'status', title: t('billing.sub_status'), slot: 'cell-status', width: '90px' },
  { key: 'startedAt', title: t('billing.sub_started_at'), slot: 'cell-startedAt', width: '150px' },
  { key: 'expiresAt', title: t('billing.sub_expires_at'), slot: 'cell-expiresAt', width: '150px' },
  { key: 'actions', title: t('billing.actions'), slot: 'cell-actions', width: '90px' }
]

const invoiceCols = [
  { key: 'id', title: 'ID' },
  { key: 'amount', title: t('billing.invoice_amount'), slot: 'cell-amount', width: '110px' },
  { key: 'status', title: t('billing.invoice_status'), slot: 'cell-status', width: '90px' },
  { key: 'periodStart', title: t('billing.invoice_period_start'), slot: 'cell-periodStart', width: '150px' },
  { key: 'periodEnd', title: t('billing.invoice_period_end'), slot: 'cell-periodEnd', width: '150px' },
  { key: 'actions', title: t('billing.actions'), slot: 'cell-actions', width: '90px' }
]

// —— 格式化：价格（分 → 元） ——
function fmtPrice(cents) {
  if (cents == null) return '—'
  return `¥${(cents / 100).toFixed(2)}`
}
function fmtInterval(v) {
  return v === 'yearly' ? t('billing.interval_yearly') : t('billing.interval_monthly')
}
function fmtSubStatus(v) {
  const m = { active: 'billing.sub_status_active', canceled: 'billing.sub_status_canceled', expired: 'billing.sub_status_expired' }
  return m[v] ? t(m[v]) : (v || '—')
}
function fmtInvoiceStatus(v) {
  const m = { pending: 'billing.invoice_status_pending', paid: 'billing.invoice_status_paid', overdue: 'billing.invoice_status_overdue' }
  return m[v] ? t(m[v]) : (v || '—')
}
// 订阅行显示计划名（找不到时回退显示 ID）
function planName(id) {
  const p = plans.value.find((x) => x.id === id)
  return p ? p.name : (id || '—')
}

// ============ 数据拉取 ============
async function fetchPlans() {
  loading.value = true
  try {
    const r = await listPlans()
    plans.value = (r && r.plans) || []
  } catch {
    plans.value = []
  } finally {
    loading.value = false
  }
}

async function fetchSubs() {
  loading.value = true
  try {
    const r = await listSubscriptions()
    subs.value = (r && r.subscriptions) || []
  } catch {
    subs.value = []
  } finally {
    loading.value = false
  }
}

async function fetchInvoices() {
  loading.value = true
  try {
    const r = await listInvoices()
    invoices.value = (r && r.invoices) || []
  } catch {
    invoices.value = []
  } finally {
    loading.value = false
  }
}

async function fetchUsage() {
  loading.value = true
  try {
    usage.value = await getUsage()
  } catch {
    usage.value = null
  } finally {
    loading.value = false
  }
}

// 切 tab 时按需拉取对应分区数据（避免每次切换都全量请求）
watch(tab, (key) => {
  if (key === 'plans') fetchPlans()
  else if (key === 'subscriptions') fetchSubs()
  else if (key === 'invoices') fetchInvoices()
  else fetchUsage()
})

// ============ 订阅计划：新增/编辑/删除 ============
function openPlanCreate() {
  formError.value = ''
  planForm.value = { id: null, name: '', price: 0, interval: 'monthly', featuresText: '' }
}

function openPlanEdit(row) {
  formError.value = ''
  planForm.value = {
    id: row.id,
    name: row.name || '',
    price: row.price ?? 0,
    interval: row.interval || 'monthly',
    featuresText: (row.features || []).join('\n')
  }
}

async function onSavePlan() {
  formError.value = ''
  // features 由换行/逗号分隔文本转数组
  const features = String(planForm.value.featuresText || '')
    .split(/[\n,，]/)
    .map((s) => s.trim())
    .filter(Boolean)
  const body = {
    name: planForm.value.name,
    price: Number(planForm.value.price) || 0,
    interval: planForm.value.interval,
    features
  }
  try {
    if (planForm.value.id) {
      await updatePlan(planForm.value.id, body)
      toast.success(t('billing.update_ok'))
    } else {
      await createPlan(body)
      toast.success(t('billing.create_ok'))
    }
    planForm.value = null
    await fetchPlans()
  } catch (e) {
    formError.value = e.j?.error || t('billing.save_failed')
  }
}

function onDeletePlan(row) {
  deleteConfirm.row = row
  deleteConfirm.domain = 'plan'
  deleteConfirm.msg = t('billing.confirm_delete_plan', { name: row.name || row.id })
  deleteConfirm.show = true
}

// ============ 订阅：新增/编辑/删除 ============
function openSubCreate() {
  formError.value = ''
  subForm.value = { id: null, planID: '', status: 'active' }
}

function openSubEdit(row) {
  formError.value = ''
  subForm.value = { id: row.id, planID: row.planID || '', status: row.status || 'active' }
}

async function onSaveSub() {
  formError.value = ''
  if (!subForm.value.planID) {
    formError.value = t('billing.sub_plan_ph')
    return
  }
  const body = { planID: subForm.value.planID, status: subForm.value.status }
  try {
    if (subForm.value.id) {
      await updateSubscription(subForm.value.id, body)
      toast.success(t('billing.update_ok'))
    } else {
      await createSubscription(body)
      toast.success(t('billing.create_ok'))
    }
    subForm.value = null
    await fetchSubs()
  } catch (e) {
    formError.value = e.j?.error || t('billing.save_failed')
  }
}

function onDeleteSub(row) {
  deleteConfirm.row = row
  deleteConfirm.domain = 'sub'
  deleteConfirm.msg = t('billing.confirm_delete_sub', { id: row.id })
  deleteConfirm.show = true
}

// ============ 账单：明细查看（只读） ============
function openInvoiceDetail(row) {
  invoiceDetail.value = row
}

// ============ 删除确认统一回调 ============
async function onDeleteConfirm() {
  const { row, domain } = deleteConfirm
  if (!row) return
  try {
    if (domain === 'plan') {
      await deletePlan(row.id)
      await fetchPlans()
    } else if (domain === 'sub') {
      await deleteSubscription(row.id)
      await fetchSubs()
    }
    toast.success(t('billing.delete_ok'))
  } catch (e) {
    errorConfirm.message = e.j?.error || t('billing.delete_failed')
    errorConfirm.show = true
  }
}

// 计划列表在订阅抽屉中作为下拉数据源，两 tab 都需要，挂载时先拉
onMounted(() => {
  fetchPlans()
  fetchSubs()
  fetchInvoices()
  fetchUsage()
})
</script>

<style scoped>
.tabs { margin: 14px 0 4px; }
.tab-btn {
  border: 1px solid var(--border);
  background: var(--surface-2);
  color: var(--text-2);
  border-radius: var(--radius-sm);
  padding: 6px 14px;
  font-size: 13px;
  cursor: pointer;
  transition: .12s;
}
.tab-btn.active {
  background: var(--accent-soft);
  color: var(--accent);
  border-color: var(--accent);
  font-weight: 600;
}
.row-actions { display: flex; gap: 4px; }
.entity-form { display: flex; flex-direction: column; gap: 12px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field > label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.usage-grid { display: flex; flex-wrap: wrap; gap: 12px; margin-top: 14px; }
.usage-card {
  min-width: 150px; padding: 16px 20px;
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  text-align: center;
}
.usage-card.wide { flex: 1; min-width: 220px; }
.usage-num { font-size: 26px; font-weight: 700; color: var(--accent); }
.usage-num.small { font-size: 15px; color: var(--text); font-weight: 600; }
.usage-label { margin-top: 6px; font-size: 12.5px; color: var(--text-3); }
.btnbar { margin-top: 8px; }
</style>
