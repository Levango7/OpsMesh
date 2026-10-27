<template>
  <PageShell
    :title="$t('cost.title')"
    :desc="$t('cost.desc')"
    testid="cost"
    :error="store.error || loadErr"
  >
    <!-- 预算概览 -->
    <div class="metrics-row" v-if="store.budget">
      <MetricsCard :title="$t('cost.daily_used')" icon="cmdb">
        <div class="metric-value">{{ store.budget.usedToday || 0 }}</div>
        <div class="metric-sub">/ {{ store.budget.dailyLimit || 0 }}</div>
      </MetricsCard>
      <MetricsCard :title="$t('cost.monthly_used')" icon="cmdb">
        <div class="metric-value">{{ store.budget.usedMonth || 0 }}</div>
        <div class="metric-sub">/ {{ store.budget.monthlyLimit || 0 }}</div>
      </MetricsCard>
      <MetricsCard :title="$t('cost.alerts')" icon="alerts">
        <div class="metric-value">{{ store.alerts.length }}</div>
      </MetricsCard>
      <MetricsCard :title="$t('cost.total_cost')" icon="cmdb">
        <div class="metric-value">${{ (store.report?.totalCost || 0).toFixed(4) }}</div>
      </MetricsCard>
    </div>

    <!-- 预算设置 -->
    <div class="card">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('cost.budget_title') }}</h3>
      </div>
      <div class="row">
        <div class="field">
          <label>{{ $t('cost.daily_limit') }}</label>
          <input v-model.number="budgetForm.daily" type="number" min="0" />
        </div>
        <div class="field">
          <label>{{ $t('cost.monthly_limit') }}</label>
          <input v-model.number="budgetForm.monthly" type="number" min="0" />
        </div>
        <div class="field">
          <label>{{ $t('cost.threshold') }}</label>
          <input v-model.number="budgetForm.threshold" type="number" min="0" max="1" step="0.1" />
        </div>
        <div class="field">
          <Button variant="primary" size="sm" @click="updateBudget">{{ $t('cost.save_budget') }}</Button>
        </div>
      </div>
    </div>

    <!-- 告警列表 -->
    <div class="card" v-if="store.alerts.length">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('cost.alerts_title') }}</h3>
      </div>
      <DataTable
        :columns="alertColumns"
        :rows="store.alerts"
        row-key="timestamp"
      />
    </div>

    <!-- 成本报告 -->
    <div class="card">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('cost.report_title') }}</h3>
        <Button variant="primary" size="sm" @click="fetchReport">{{ $t('common.refresh') }}</Button>
        <Button variant="ghost" size="sm" @click="exportMarkdown">{{ $t('cost.export_md') }}</Button>
      </div>
      <div v-if="store.report">
        <div class="report-summary">
          <div class="summary-item">
            <span class="summary-label">{{ $t('cost.total_cost') }}</span>
            <span class="summary-value">${{ store.report.totalCost.toFixed(4) }}</span>
          </div>
          <div class="summary-item">
            <span class="summary-label">{{ $t('cost.input_tokens') }}</span>
            <span class="summary-value">{{ store.report.totalInputTokens }}</span>
          </div>
          <div class="summary-item">
            <span class="summary-label">{{ $t('cost.output_tokens') }}</span>
            <span class="summary-value">{{ store.report.totalOutputTokens }}</span>
          </div>
          <div class="summary-item">
            <span class="summary-label">{{ $t('cost.records') }}</span>
            <span class="summary-value">{{ store.report.recordCount }}</span>
          </div>
        </div>
        <!-- 按提供商 -->
        <div v-if="store.report.byProvider" class="report-section">
          <h4>{{ $t('cost.by_provider') }}</h4>
          <DataTable :columns="providerColumns" :rows="providerRows" row-key="name" />
        </div>
        <!-- 按模型 -->
        <div v-if="store.report.byModel" class="report-section">
          <h4>{{ $t('cost.by_model') }}</h4>
          <DataTable :columns="modelColumns" :rows="modelRows" row-key="name" />
        </div>
      </div>
      <div v-else class="muted">{{ $t('cost.no_report') }}</div>
    </div>

    <!-- 模型路由 -->
    <div class="card">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('cost.routes_title') }}</h3>
        <Button variant="ghost" size="sm" @click="store.fetchRoutes()">{{ $t('common.refresh') }}</Button>
      </div>
      <DataTable
        v-if="store.routes.length"
        :columns="routeColumns"
        :rows="store.routes"
        row-key="taskType"
      />
      <div v-else class="muted">{{ $t('cost.no_routes') }}</div>
    </div>
  </PageShell>
</template>

<script setup>
import PageShell from '@/components/PageShell.vue'

import { ref, computed, onMounted } from 'vue'
import { useCostStore } from '@/stores/cost'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import MetricsCard from '@/components/MetricsCard.vue'
import { Button } from '@/components/ui'

const store = useCostStore()

// 预算保存/报告刷新/导出此前静默吞错——统一骨架批次补上可见错误（与 store.error 并列展示）
const loadErr = ref('')

const budgetForm = ref({ daily: 100000, monthly: 1000000, threshold: 0.8 })

const alertColumns = [
  { key: 'type', title: t('cost.alert_type') },
  { key: 'used', title: t('cost.used') },
  { key: 'limit', title: t('cost.limit') },
  { key: 'threshold', title: t('cost.threshold') },
  { key: 'timestamp', title: t('cost.time') }
]

const providerColumns = [
  { key: 'name', title: t('cost.provider') },
  { key: 'cost', title: t('cost.cost') }
]

const modelColumns = [
  { key: 'name', title: t('cost.model') },
  { key: 'cost', title: t('cost.cost') }
]

const routeColumns = [
  { key: 'taskType', title: t('cost.task_type') },
  { key: 'tier', title: t('cost.tier') },
  { key: 'provider', title: t('cost.provider') },
  { key: 'model', title: t('cost.model') },
  { key: 'maxTokens', title: t('cost.max_tokens') }
]

const providerRows = computed(() => {
  if (!store.report?.byProvider) return []
  return Object.entries(store.report.byProvider).map(([name, cost]) => ({ name, cost: `$${cost.toFixed(4)}` }))
})

const modelRows = computed(() => {
  if (!store.report?.byModel) return []
  return Object.entries(store.report.byModel).map(([name, cost]) => ({ name, cost: `$${cost.toFixed(4)}` }))
})

async function updateBudget() {
  try { await store.updateBudget('default', budgetForm.value.daily, budgetForm.value.monthly, budgetForm.value.threshold) }
  catch (e) { loadErr.value = e.j?.error || t('common.load_failed') }
}

async function fetchReport() {
  try { await store.fetchReport('default') }
  catch (e) { loadErr.value = e.j?.error || t('common.load_failed') }
}

async function exportMarkdown() {
  try {
    const md = await store.generateMarkdownReport('AI Cost Report', [
      { title: 'Summary', content: `Total Cost: $${store.report?.totalCost?.toFixed(4) || '0.0000'}` }
    ])
    // 下载 markdown 文件
    const blob = new Blob([md], { type: 'text/markdown' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'cost-report.md'
    a.click()
    URL.revokeObjectURL(url)
  } catch (e) { loadErr.value = e.j?.error || t('common.load_failed') }
}

onMounted(() => {
  store.fetchBudget('default')
  store.fetchAlerts('default')
  store.fetchReport('default')
  store.fetchRoutes()
})
</script>

<style scoped>
.metrics-row { display: grid; grid-template-columns: repeat(4, 1fr); gap: 14px; margin-bottom: 18px; }
.card { background: var(--color-surface-1, #ffffff); border: 1px solid var(--color-hairline, #d3dbe8); border-radius: var(--radius-md, 8px); padding: 16px 18px; margin-bottom: 16px; }
.card__title { margin: 0 0 12px 0; font-size: 13px; font-weight: 600; color: var(--color-ink-muted, #6b7280); }
.flowbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 12px; }
.row { display: flex; gap: 16px; flex-wrap: wrap; margin-bottom: 16px; }
.field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 8px; min-width: 160px; }
.field label { font-size: 11px; font-weight: 500; color: var(--color-ink-muted, #6b7280); }
.field input { padding: 7px 9px; border: 1px solid var(--color-hairline, #d3dbe8); border-radius: var(--radius-sm, 6px); font-size: 13px; background: var(--color-surface-1, #ffffff); }
.btnbar { display: flex; gap: 8px; margin-bottom: 12px; }
.row-actions { display: inline-flex; gap: 4px; flex-wrap: wrap; }
.muted { color: var(--color-ink-muted, #6b7280); font-size: 12.5px; }
.metric-value { font-size: 24px; font-weight: 700; }
.metric-sub { font-size: 11px; color: var(--color-ink-muted, #6b7280); }
.report-summary { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; margin-bottom: 16px; }
.summary-item { display: flex; flex-direction: column; gap: 4px; padding: 8px 10px; background: var(--color-surface-2, #f7f9fc); border-radius: var(--radius-sm, 6px); }
.summary-label { font-size: 11px; color: var(--color-ink-muted, #6b7280); }
.summary-value { font-size: 16px; font-weight: 600; }
.report-section { margin-top: 12px; }
.report-section h4 { margin: 0 0 8px 0; font-size: 13px; font-weight: 600; }
</style>