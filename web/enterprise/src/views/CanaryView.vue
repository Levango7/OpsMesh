<template>
  <div>
    <h2 data-testid="canary-title">{{ $t('canary.title') }}</h2>
    <p class="muted">{{ $t('canary.subtitle') }}</p>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <!-- 创建灰度发布 -->
    <div class="card">
      <h3>{{ $t('canary.create_title') }}</h3>
      <form @submit.prevent="onCreate">
        <div class="row-2">
          <div class="field">
            <label>{{ $t('canary.field_service') }}</label>
            <input v-model.trim="form.serviceName" type="text" required data-testid="canary-service-input" />
          </div>
          <div class="field">
            <label>{{ $t('canary.field_target_version') }}</label>
            <input v-model.trim="form.targetVersion" type="text" required data-testid="canary-target-version-input" />
          </div>
        </div>
        <div class="row-2">
          <div class="field">
            <label>{{ $t('canary.field_baseline_version') }}</label>
            <input v-model.trim="form.baselineVersion" type="text" :placeholder="$t('canary.optional')" />
          </div>
          <div class="field">
            <label>{{ $t('canary.field_steps') }}</label>
            <input v-model.trim="form.steps" type="text" placeholder="10,30,50,100" />
          </div>
        </div>
        <div class="btnbar">
          <button type="submit" class="primary" :disabled="creating" data-testid="canary-create-btn">
            <Icon name="add" :size="14" />
            {{ creating ? $t('common.loading') : $t('canary.create_btn') }}
          </button>
        </div>
        <p v-if="createMsg" :class="['msg', createOk ? 'ok' : 'err']">{{ createMsg }}</p>
      </form>
    </div>

    <!-- 当前灰度状态 -->
    <div class="card" v-if="store.current">
      <div class="flowbar">
        <h3>{{ $t('canary.status_title') }} · {{ store.current.canaryID || '—' }}</h3>
        <button class="xs outline" @click="refreshCurrent" v-if="store.current.canaryID"><Icon name="refresh" :size="13" /> {{ $t('common.refresh') }}</button>
        <button class="xs outline" @click="store.clearCurrent()">{{ $t('common.close') }}</button>
      </div>

      <div class="stats">
        <div class="stat indigo">
          <div class="stat-v">{{ store.current.currentStep || 0 }}%</div>
          <div class="stat-l">{{ $t('canary.stat_step') }}</div>
        </div>
        <div class="stat teal">
          <div class="stat-v">{{ store.current.status || '—' }}</div>
          <div class="stat-l">{{ $t('canary.stat_status') }}</div>
        </div>
        <div class="stat amber">
          <div class="stat-v">{{ store.current.serviceName || '—' }}</div>
          <div class="stat-l">{{ $t('canary.stat_service') }}</div>
        </div>
        <div class="stat rose">
          <div class="stat-v">{{ store.current.targetVersion || '—' }}</div>
          <div class="stat-l">{{ $t('canary.stat_target') }}</div>
        </div>
      </div>

      <div class="btnbar">
        <button
          class="primary"
          @click="onAdvance"
          :disabled="advancing || !canAdvance"
          data-testid="canary-advance-btn"
        >
          <Icon name="arrow-right" :size="14" />
          {{ advancing ? $t('common.loading') : $t('canary.advance_btn') }}
        </button>
      </div>
    </div>

    <!-- 流量分割 -->
    <div class="card" v-if="store.current">
      <h3>{{ $t('canary.traffic_title') }}</h3>
      <div v-if="!trafficEdit" class="traffic-view">
        <div class="traffic-bar">
          <div class="seg canary" :style="{ width: canaryPercent + '%' }">{{ canaryPercent }}%</div>
          <div class="seg baseline" :style="{ width: baselinePercent + '%' }">{{ baselinePercent }}%</div>
        </div>
        <div class="legend">
          <span class="dot canary"></span> {{ $t('canary.legend_canary') }} {{ canaryPercent }}%
          <span class="dot baseline"></span> {{ $t('canary.legend_baseline') }} {{ baselinePercent }}%
        </div>
        <div class="btnbar">
          <button class="xs outline" @click="openTrafficEdit" data-testid="canary-traffic-edit-btn">{{ $t('canary.traffic_edit') }}</button>
        </div>
      </div>
      <form v-else class="traffic-form" @submit.prevent="onTrafficSave">
        <div class="field">
          <label>{{ $t('canary.field_canary_percent') }}（0-100）</label>
          <input v-model.number="trafficForm.canaryPercent" type="number" min="0" max="100" step="1" required />
        </div>
        <div class="btnbar">
          <button type="submit" class="primary" :disabled="trafficSaving">{{ $t('common.save') }}</button>
          <button type="button" class="outline" @click="trafficEdit = false">{{ $t('common.cancel') }}</button>
        </div>
      </form>
    </div>

    <!-- 灰度指标 -->
    <div class="card" v-if="store.current">
      <div class="flowbar">
        <h3>{{ $t('canary.metrics_title') }}</h3>
        <button class="xs outline" @click="refreshMetrics" v-if="store.current.canaryID"><Icon name="refresh" :size="13" /> {{ $t('common.refresh') }}</button>
      </div>
      <div v-if="!store.metrics" class="muted">{{ $t('canary.no_metrics') }}</div>
      <div v-else class="metrics-grid">
        <div class="metrics-block">
          <h4>{{ $t('canary.metrics_canary') }}</h4>
          <table>
            <tr v-for="(v, k) in canaryMetricsMap" :key="k">
              <th>{{ k }}</th><td>{{ v }}</td>
            </tr>
            <tr v-if="!Object.keys(canaryMetricsMap).length">
              <td class="muted">{{ $t('canary.no_metrics') }}</td>
            </tr>
          </table>
        </div>
        <div class="metrics-block">
          <h4>{{ $t('canary.metrics_baseline') }}</h4>
          <table>
            <tr v-for="(v, k) in baselineMetricsMap" :key="k">
              <th>{{ k }}</th><td>{{ v }}</td>
            </tr>
            <tr v-if="!Object.keys(baselineMetricsMap).length">
              <td class="muted">{{ $t('canary.no_metrics') }}</td>
            </tr>
          </table>
        </div>
      </div>
    </div>

    <!-- 历史灰度 -->
    <div class="card" v-if="store.list.length">
      <h3>{{ $t('canary.history_title') }}</h3>
      <DataTable
        :columns="historyColumns"
        :rows="store.list"
        row-key="canaryID"
        :empty-text="$t('canary.no_history')"
      >
        <template #cell-canaryID="{ value }"><code>{{ value }}</code></template>
        <template #cell-status="{ value }">
          <StatusBadge :status="value" :text="value" />
        </template>
        <template #cell-actions="{ row }">
          <button class="xs outline" @click="viewCanary(row.canaryID)" data-testid="canary-view-btn">{{ $t('canary.view') }}</button>
        </template>
      </DataTable>
    </div>

    <!-- 推进确认 -->
    <ConfirmModal
      v-model="advanceConfirm.show"
      data-testid="canary-advance-confirm-modal"
      :title="$t('canary.advance_btn')"
      :message="$t('canary.advance_confirm')"
      @confirm="onAdvanceConfirm"
    />
    <!-- 错误提示 -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="canary-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// 灰度发布页 — 创建 + 状态 + 推进 + 流量分割 + 指标
import { ref, reactive, computed } from 'vue'
import { useCanaryStore } from '@/stores/canary'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'

const store = useCanaryStore()
const form = ref({ serviceName: '', targetVersion: '', baselineVersion: '', steps: '10,30,50,100' })
const creating = ref(false)
const createMsg = ref('')
const createOk = ref(false)
const advancing = ref(false)
const trafficSaving = ref(false)
const trafficEdit = ref(false)
const trafficForm = ref({ canaryPercent: 0 })

const advanceConfirm = reactive({ show: false })
const errorConfirm = reactive({ show: false, message: '' })

const canAdvance = computed(() => {
  const c = store.current
  if (!c) return false
  // 已完成/已取消/已回滚则不可推进
  const status = c.status || ''
  return !['completed', 'cancelled', 'rolledback', 'failed'].includes(status)
})

const canaryPercent = computed(() => {
  const ts = store.trafficSplit
  if (!ts) return store.current?.currentStep || 0
  return ts.canaryPercent != null ? ts.canaryPercent : (store.current?.currentStep || 0)
})

const baselinePercent = computed(() => Math.max(0, 100 - canaryPercent.value))

const canaryMetricsMap = computed(() => {
  const m = store.metrics
  if (!m) return {}
  return m.canary || m.canaryMetrics || {}
})

const baselineMetricsMap = computed(() => {
  const m = store.metrics
  if (!m) return {}
  return m.baseline || m.baselineMetrics || {}
})

const historyColumns = computed(() => [
  { key: 'canaryID', title: t('canary.col_canary_id'), slot: 'cell-canaryID' },
  { key: 'serviceName', title: t('canary.stat_service') },
  { key: 'targetVersion', title: t('canary.stat_target') },
  { key: 'currentStep', title: t('canary.stat_step') },
  { key: 'status', title: t('canary.stat_status'), slot: 'cell-status' },
  { key: 'actions', title: t('canary.col_actions'), slot: 'cell-actions', width: '80px' }
])

async function onCreate() {
  if (creating.value) return
  creating.value = true
  createMsg.value = ''
  try {
    const body = {
      serviceName: form.value.serviceName,
      targetVersion: form.value.targetVersion
    }
    if (form.value.baselineVersion) body.baselineVersion = form.value.baselineVersion
    if (form.value.steps) {
      body.steps = form.value.steps.split(',').map((s) => parseInt(s.trim(), 10)).filter((n) => !isNaN(n))
    }
    const r = await store.create(body)
    createMsg.value = t('canary.create_success', { id: r.j?.canaryID || '—' })
    createOk.value = true
    if (r.j?.canaryID) await loadCanaryDetail(r.j.canaryID)
  } catch (e) {
    createMsg.value = t('canary.create_failed') + (e.j?.error || e.message || '')
    createOk.value = false
  } finally {
    creating.value = false
  }
}

async function loadCanaryDetail(id) {
  try {
    await store.fetchStatus(id)
    await Promise.allSettled([
      store.fetchTrafficSplit(id),
      store.fetchMetrics(id)
    ])
  } catch (e) {
    errorConfirm.message = e.j?.error || t('canary.fetch_status_failed')
    errorConfirm.show = true
  }
}

async function viewCanary(id) {
  await loadCanaryDetail(id)
}

async function refreshCurrent() {
  if (!store.current?.canaryID) return
  await loadCanaryDetail(store.current.canaryID)
}

async function refreshMetrics() {
  if (!store.current?.canaryID) return
  try { await store.fetchMetrics(store.current.canaryID) }
  catch (e) {
    errorConfirm.message = e.j?.error || t('canary.fetch_metrics_failed')
    errorConfirm.show = true
  }
}

function onAdvance() {
  advanceConfirm.show = true
}

async function onAdvanceConfirm() {
  if (!store.current?.canaryID || advancing.value) return
  advancing.value = true
  try {
    await store.advance(store.current.canaryID)
    toast.success(t('canary.advance_success'))
    await loadCanaryDetail(store.current.canaryID)
  } catch (e) {
    errorConfirm.message = e.j?.error || t('canary.advance_failed')
    errorConfirm.show = true
  } finally {
    advancing.value = false
  }
}

function openTrafficEdit() {
  trafficForm.value.canaryPercent = canaryPercent.value
  trafficEdit.value = true
}

async function onTrafficSave() {
  if (!store.current?.canaryID || trafficSaving.value) return
  trafficSaving.value = true
  try {
    await store.updateTrafficSplit(store.current.canaryID, { canaryPercent: trafficForm.value.canaryPercent })
    toast.success(t('canary.traffic_saved'))
    trafficEdit.value = false
  } catch (e) {
    errorConfirm.message = e.j?.error || t('canary.traffic_save_failed')
    errorConfirm.show = true
  } finally {
    trafficSaving.value = false
  }
}
</script>

<style scoped>
.card {
  background: var(--surface); border: 1px solid var(--border);
  border-radius: var(--radius); padding: 16px; margin-top: 14px;
  box-shadow: var(--shadow);
}
.card h3 { margin: 0 0 12px; font-size: 14px; }
.card h4 { margin: 0 0 8px; font-size: 13px; }
.flowbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 10px; }
.flowbar h3 { margin: 0; }

.field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 10px; min-width: 220px; }
.field label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.row-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.btnbar { display: flex; gap: 8px; margin-top: 8px; }

.stats { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; margin-bottom: 14px; }
.stat {
  background: var(--surface); border: 1px solid var(--border);
  border-radius: var(--radius); padding: 12px 14px;
  border-top: 3px solid var(--accent);
}
.stat.rose { border-top-color: var(--rose); }
.stat.amber { border-top-color: var(--amber); }
.stat.indigo { border-top-color: var(--indigo); }
.stat.teal { border-top-color: var(--teal); }
.stat-v { font-size: 20px; font-weight: 700; color: var(--text); line-height: 1.1; font-variant-numeric: tabular-nums; word-break: break-all; }
.stat-l { font-size: 11.5px; color: var(--text-3); margin-top: 4px; }

.traffic-bar {
  display: flex; height: 24px; border-radius: var(--radius-sm); overflow: hidden;
  border: 1px solid var(--border); margin-bottom: 8px;
}
.seg {
  display: flex; align-items: center; justify-content: center;
  font-size: 12px; font-weight: 600; color: var(--text); transition: width .2s;
  min-width: 0;
}
.seg.canary { background: var(--accent-soft); color: var(--accent); }
.seg.baseline { background: var(--surface-3); color: var(--text-2); }
.legend { display: flex; align-items: center; gap: 12px; font-size: 12.5px; color: var(--text-2); margin-bottom: 8px; }
.dot { display: inline-block; width: 10px; height: 10px; border-radius: 2px; margin-right: 4px; vertical-align: middle; }
.dot.canary { background: var(--accent); }
.dot.baseline { background: var(--surface-3); border: 1px solid var(--border); }

.traffic-form { max-width: 320px; }

.metrics-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.metrics-block table { width: 100%; font-size: 12.5px; border-collapse: collapse; }
.metrics-block th, .metrics-block td {
  text-align: left; padding: 6px 8px; border-bottom: 1px solid var(--border);
}
.metrics-block th { background: var(--surface-3); color: var(--text-2); font-weight: 600; font-size: 11.5px; }
.metrics-block tr:last-child td { border-bottom: none; }

.poll-err {
  padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}

@media (max-width: 768px) {
  .stats { grid-template-columns: repeat(2, 1fr); }
  .row-2 { grid-template-columns: 1fr; }
  .metrics-grid { grid-template-columns: 1fr; }
}
</style>