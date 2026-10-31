<template>
  <PageShell
    :title="$t('portal.title')"
    :desc="$t('portal.desc')"
    testid="portal"
    :error="store.error"
  >
    <!-- 成本概览 -->
    <div class="metrics-row">
      <MetricsCard :title="$t('portal.totalCost')" icon="cmdb" accent="--accent">
        <div class="metric-value">{{ formatCost(store.totalCost) }}</div>
      </MetricsCard>
      <MetricsCard :title="$t('portal.myRequests')" icon="task" accent="--info">
        <div class="metric-value">{{ store.myRequests.length }}</div>
      </MetricsCard>
      <MetricsCard :title="$t('portal.pendingApprovals')" icon="clock" accent="--warn">
        <div class="metric-value">{{ store.pendingApprovals.length }}</div>
      </MetricsCard>
    </div>

    <!-- 成本趋势图 -->
    <div class="card" v-if="store.costOverview?.trend">
      <h3>{{ $t('portal.costTrend') }}</h3>
      <div class="chart-area">
        <div class="mock-chart cost-chart">
          <div
            v-for="(point, idx) in store.costOverview.trend"
            :key="idx"
            class="chart-bar bar-cost"
            :style="{ height: costBarHeight(point.amount) + '%' }"
            :title="point.date + ': ' + formatCost(point.amount)"
          />
        </div>
      </div>
    </div>

    <div class="row">
      <!-- 左：资源请求 -->
      <div class="col">
        <div class="card">
          <div class="flowbar">
            <h3 style="margin: 0">{{ $t('portal.requestResource') }}</h3>
            <Button variant="primary" size="sm" @click="openRequestForm" data-testid="portal-request-btn">{{ $t('portal.submitRequest') }}</Button>
          </div>
          <div class="field">
            <label>{{ $t('portal.resourceType') }}</label>
            <select v-model="requestForm.type" data-testid="portal-request-type">
              <option value="vm">{{ $t('portal.type.vm') }}</option>
              <option value="container">{{ $t('portal.type.container') }}</option>
              <option value="gpu">{{ $t('portal.type.gpu') }}</option>
              <option value="storage">{{ $t('portal.type.storage') }}</option>
            </select>
          </div>
          <div class="field">
            <label>{{ $t('portal.resourceName') }}</label>
            <input v-model="requestForm.resource" placeholder="my-app-prod" data-testid="portal-request-resource" />
          </div>
          <div class="field">
            <label>{{ $t('portal.params') }}</label>
            <textarea v-model="requestForm.params" rows="3" :placeholder="$t('portal.paramsPlaceholder')" data-testid="portal-request-params" />
          </div>
          <div class="field">
            <label>{{ $t('portal.reason') }}</label>
            <input v-model="requestForm.reason" :placeholder="$t('portal.reasonPlaceholder')" data-testid="portal-request-reason" />
          </div>
          <div class="btnbar">
            <Button variant="primary" size="md" @click="submitRequest" :disabled="submitting" data-testid="portal-submit-btn">{{ $t('portal.submit') }}</Button>
          </div>
          <p v-if="requestMsg" :class="['msg', requestOk ? 'ok' : 'err']">{{ requestMsg }}</p>
        </div>
      </div>

      <!-- 右：我的请求 -->
      <div class="col">
        <div class="card">
          <div class="flowbar">
            <h3 style="margin: 0">{{ $t('portal.myRequests') }}</h3>
            <Button variant="ghost" size="sm" @click="store.fetchMyRequests()">↻ {{ $t('common.refresh') }}</Button>
          </div>
          <div v-if="store.loading && !store.myRequests.length" class="muted">{{ $t('common.loading') }}</div>
          <DataTable v-else :columns="requestCols" :rows="store.myRequests" row-key="id" :empty-text="$t('portal.noRequests')">
            <template #cell-type="{ value }"><code>{{ value }}</code></template>
            <template #cell-status="{ value }">
              <StatusBadge :status="requestStatus(value)" :text="value || '-'" />
            </template>
            <template #cell-cost="{ value }">{{ formatCost(value) }}</template>
          </DataTable>
        </div>
      </div>
    </div>

    <!-- 审批队列 -->
    <div class="card">
      <div class="flowbar">
        <h3 style="margin: 0">{{ $t('portal.approvalQueue') }}</h3>
        <Button variant="ghost" size="sm" @click="store.fetchApprovalQueue()">↻ {{ $t('common.refresh') }}</Button>
      </div>
      <div v-if="store.loading && !store.approvalQueue.length" class="muted">{{ $t('common.loading') }}</div>
      <DataTable v-else :columns="approvalCols" :rows="store.approvalQueue" row-key="id" :empty-text="$t('portal.noApprovals')">
        <template #cell-type="{ value }"><code>{{ value }}</code></template>
        <template #cell-requester="{ value }">{{ value || '-' }}</template>
        <template #cell-actions="{ row }">
          <div class="row-actions" @click.stop>
            <Button variant="primary" size="sm" @click="onApprove(row.id)" data-testid="portal-approve-btn">{{ $t('portal.approve') }}</Button>
            <Button variant="ghost" size="sm" style="color: var(--fail); border-color: var(--fail)" @click="onReject(row.id)" data-testid="portal-reject-btn">{{ $t('portal.reject') }}</Button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- 审批确认（替代 confirm） -->
    <ConfirmModal
      v-model="approveConfirm.show"
      data-testid="portal-approve-confirm-modal"
      :title="approveConfirm.action === 'reject' ? $t('portal.reject') : $t('portal.approve')"
      :message="approveConfirm.action === 'reject' ? $t('portal.rejectConfirm') : $t('portal.approveConfirm')"
      @confirm="onApproveConfirm"
    />
  </PageShell>
</template>

<script setup>
import PageShell from '@/components/PageShell.vue'

import { computed, onMounted, reactive, ref } from 'vue'
import { usePortalStore } from '@/stores/portal'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import MetricsCard from '@/components/MetricsCard.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import { toast } from '@/utils/toast'
import Button from "@/components/ui/Button.vue"

const store = usePortalStore()

const requestCols = computed(() => [
  { key: 'id', title: t('portal.col.requestId') },
  { key: 'type', title: t('portal.col.type'), slot: 'cell-type' },
  { key: 'resource', title: t('portal.col.resource') },
  { key: 'status', title: t('portal.col.status'), slot: 'cell-status' },
  { key: 'cost', title: t('portal.col.cost'), slot: 'cell-cost' },
  { key: 'createdAt', title: t('portal.col.createdAt') }
])

const approvalCols = computed(() => [
  { key: 'id', title: t('portal.col.requestId') },
  { key: 'type', title: t('portal.col.type'), slot: 'cell-type' },
  { key: 'resource', title: t('portal.col.resource') },
  { key: 'requester', title: t('portal.col.requester'), slot: 'cell-requester' },
  { key: 'actions', title: t('portal.col.action'), slot: 'cell-actions', width: '160px' }
])

function requestStatus(s) {
  if (s === 'approved' || s === 'completed') return 'success'
  if (s === 'rejected' || s === 'failed') return 'failed'
  if (s === 'pending') return 'warn'
  return 'info'
}

function formatCost(v) {
  if (v == null) return '-'
  return '$' + Number(v).toFixed(2)
}

function costBarHeight(amount) {
  if (!store.costOverview?.trend) return 0
  const max = Math.max(...store.costOverview.trend.map((t) => t.amount || 0), 1)
  return Math.max(10, (amount / max) * 100)
}

// ---- 提交请求 ----
const requestForm = ref({ type: 'vm', resource: '', params: '', reason: '' })
const requestMsg = ref('')
const requestOk = ref(false)
const submitting = ref(false)

function openRequestForm() {
  requestForm.value = { type: 'vm', resource: '', params: '', reason: '' }
  requestMsg.value = ''
}

async function submitRequest() {
  if (!requestForm.value.resource) { requestMsg.value = t('portal.resourceRequired'); requestOk.value = false; return }
  submitting.value = true
  try {
    const r = await store.submitRequest(requestForm.value.type, requestForm.value.resource, requestForm.value.params, requestForm.value.reason)
    if (r.s >= 200 && r.s < 300) {
      requestMsg.value = t('portal.submitSuccess'); requestOk.value = true
      await store.fetchMyRequests()
    } else {
      requestMsg.value = r.j?.error || t('portal.submitFail'); requestOk.value = false
    }
  } catch (e) {
    requestMsg.value = e.j?.error || t('portal.submitFail'); requestOk.value = false
  } finally {
    submitting.value = false
  }
}

// ---- 审批确认弹窗（替代 confirm）：action 存待执行动作 ----
const approveConfirm = reactive({ show: false, id: null, action: null })

function onApprove(id) {
  approveConfirm.id = id
  approveConfirm.action = 'approve'
  approveConfirm.show = true
}

async function doApprove(id) {
  try {
    const r = await store.approve(id)
    if (r.s >= 200 && r.s < 300) {
      await store.fetchApprovalQueue()
    }
  } catch (e) {
    toast.error(e.j?.error || t('portal.approveFail'))
  }
}

function onReject(id) {
  approveConfirm.id = id
  approveConfirm.action = 'reject'
  approveConfirm.show = true
}

async function doReject(id) {
  try {
    const r = await store.reject(id, 'Rejected by manager')
    if (r.s >= 200 && r.s < 300) {
      await store.fetchApprovalQueue()
    }
  } catch (e) {
    toast.error(e.j?.error || t('portal.rejectFail'))
  }
}

async function onApproveConfirm() {
  const { id, action } = approveConfirm
  if (!id || !action) return
  if (action === 'approve') await doApprove(id)
  else if (action === 'reject') await doReject(id)
}

onMounted(() => {
  store.fetchMyRequests()
  store.fetchApprovalQueue()
  store.fetchCostOverview()
})
</script>

<style scoped>
.row .col:nth-child(1) { flex: 40; }
.row .col:nth-child(2) { flex: 60; }

.metrics-row { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 12px; margin-bottom: 16px; }
.metric-value { font-size: 28px; font-weight: 700; color: var(--text); }

.chart-area { padding: 12px 0; }
.mock-chart {
  display: flex; align-items: flex-end; gap: 3px; height: 120px; padding: 0 4px;
}
.chart-bar {
  flex: 1; min-width: 6px; border-radius: 2px 2px 0 0;
  transition: height .3s; opacity: .8;
}

.bar-cost { background: var(--accent); }
</style>
