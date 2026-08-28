<template>
  <div>
    <h2 data-testid="incident-title">{{ $t('incident.title') }}</h2>
    <p class="muted">{{ $t('incident.desc') }}</p>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <!-- 指标卡片 -->
    <div class="metrics-row">
      <MetricsCard :title="$t('incident.mttd')" icon="clock" accent="--info">
        <div class="metric-value">{{ store.metrics?.mttd || '-' }}</div>
      </MetricsCard>
      <MetricsCard :title="$t('incident.mttr')" icon="task" accent="--warn">
        <div class="metric-value">{{ store.metrics?.mttr || '-' }}</div>
      </MetricsCard>
      <MetricsCard :title="$t('incident.open')" icon="warning" accent="--fail">
        <div class="metric-value">{{ store.openIncidents.length }}</div>
      </MetricsCard>
      <MetricsCard :title="$t('incident.total')" icon="alerts" accent="--accent">
        <div class="metric-value">{{ store.incidents.length }}</div>
      </MetricsCard>
    </div>

    <div class="row">
      <!-- 左：事件列表 -->
      <div class="col">
        <div class="card">
          <div class="flowbar">
            <h3 style="margin: 0">{{ $t('incident.incidents') }}</h3>
            <button class="xs primary" @click="openAddIncident" data-testid="incident-add-btn">{{ $t('incident.add') }}</button>
            <button class="xs outline" @click="store.fetchIncidents()">↻ {{ $t('common.refresh') }}</button>
          </div>
          <div v-if="store.loading && !store.incidents.length" class="muted">{{ $t('common.loading') }}</div>
          <DataTable v-else :columns="incidentCols" :rows="store.incidents" row-key="id" :empty-text="$t('incident.noIncidents')">
            <template #cell-title="{ row }">
              <b>{{ row.title }}</b><br><code>{{ row.id }}</code>
            </template>
            <template #cell-severity="{ value }">
              <StatusBadge :status="severityStatus(value)" :text="value || '-'" />
            </template>
            <template #cell-status="{ value }">
              <StatusBadge :status="incidentStatus(value)" :text="value || '-'" />
            </template>
            <template #cell-assignee="{ value }">{{ value || '-' }}</template>
            <template #cell-actions="{ row }">
              <div class="row-actions" @click.stop>
                <button class="xs outline" @click="viewDetail(row.id)" data-testid="incident-detail-btn">{{ $t('incident.detail') }}</button>
              </div>
            </template>
          </DataTable>
        </div>
      </div>

      <!-- 右：事件详情 + 时间线 -->
      <div class="col">
        <div class="card">
          <h3>{{ $t('incident.timeline') }}</h3>
          <p class="hint">{{ store.currentIncident ? store.currentIncident.title : $t('incident.selectIncidentHint') }}</p>
          <div v-if="store.timelineLoading && !store.timeline.length" class="muted">{{ $t('common.loading') }}</div>
          <div v-else-if="!store.timeline.length" class="muted">{{ $t('incident.noTimeline') }}</div>
          <div v-else class="timeline">
            <div v-for="evt in store.timeline" :key="evt.id" class="timeline-item">
              <div class="timeline-dot" :class="'dot-' + evt.type" />
              <div class="timeline-content">
                <div class="timeline-header">
                  <span class="timeline-type">{{ evt.type }}</span>
                  <span class="timeline-time">{{ evt.timestamp || '' }}</span>
                </div>
                <div class="timeline-text">{{ evt.content }}</div>
                <div v-if="evt.author" class="timeline-author">{{ evt.author }}</div>
              </div>
            </div>
          </div>
        </div>

        <!-- 复盘报告 -->
        <div class="card">
          <div class="flowbar">
            <h3 style="margin: 0">{{ $t('incident.postmortem') }}</h3>
            <button class="xs outline" @click="onGeneratePostmortem" :disabled="!store.currentIncident" data-testid="incident-postmortem-btn">{{ $t('incident.generate') }}</button>
          </div>
          <pre v-if="store.postmortemContent" class="code-block postmortem-content">{{ store.postmortemContent }}</pre>
          <p v-else class="muted">{{ $t('incident.noPostmortem') }}</p>
        </div>
      </div>
    </div>

    <!-- 添加事件对话框 -->
    <div v-if="addOpen" class="modal-mask" data-testid="incident-add-modal" @click.self="addOpen = false">
      <div class="modal">
        <header class="modal-head">
          <h3>{{ $t('incident.add') }}</h3>
          <button class="xs outline" @click="addOpen = false">✕</button>
        </header>
        <div class="modal-body">
          <div class="field">
            <label>{{ $t('incident.title') }}</label>
            <input v-model="addForm.title" required data-testid="incident-add-title" />
          </div>
          <div class="field">
            <label>{{ $t('incident.severity') }}</label>
            <select v-model="addForm.severity" data-testid="incident-add-severity">
              <option value="critical">{{ $t('incident.sev.critical') }}</option>
              <option value="high">{{ $t('incident.sev.high') }}</option>
              <option value="medium">{{ $t('incident.sev.medium') }}</option>
              <option value="low">{{ $t('incident.sev.low') }}</option>
            </select>
          </div>
          <div class="field">
            <label>{{ $t('incident.description') }}</label>
            <textarea v-model="addForm.description" rows="4" data-testid="incident-add-desc" />
          </div>
          <div class="field">
            <label>{{ $t('incident.assignee') }}</label>
            <input v-model="addForm.assignee" data-testid="incident-add-assignee" />
          </div>
          <div class="btnbar">
            <button class="primary" @click="confirmAdd" :disabled="adding" data-testid="incident-add-confirm">{{ $t('common.confirm') }}</button>
            <button class="outline" @click="addOpen = false">{{ $t('common.cancel') }}</button>
          </div>
          <p v-if="addMsg" :class="['msg', addOk ? 'ok' : 'err']">{{ addMsg }}</p>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { useIncidentStore } from '@/stores/incident'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import MetricsCard from '@/components/MetricsCard.vue'
import Icon from '@/components/Icon.vue'

const store = useIncidentStore()

const incidentCols = computed(() => [
  { key: 'title', title: t('incident.col.title'), slot: 'cell-title' },
  { key: 'severity', title: t('incident.col.severity'), slot: 'cell-severity' },
  { key: 'status', title: t('incident.col.status'), slot: 'cell-status' },
  { key: 'assignee', title: t('incident.col.assignee'), slot: 'cell-assignee' },
  { key: 'actions', title: t('incident.col.action'), slot: 'cell-actions', width: '100px' }
])

function severityStatus(s) {
  if (s === 'critical') return 'failed'
  if (s === 'high') return 'warn'
  if (s === 'medium') return 'info'
  return 'ok'
}
function incidentStatus(s) {
  if (s === 'open') return 'warn'
  if (s === 'resolved' || s === 'closed') return 'ok'
  if (s === 'investigating') return 'info'
  return 'info'
}

async function viewDetail(id) {
  await store.fetchIncident(id)
  await store.fetchTimeline(id)
}

// ---- 添加事件 ----
const addOpen = ref(false)
const addForm = ref({ title: '', severity: 'medium', description: '', assignee: '' })
const addMsg = ref('')
const addOk = ref(false)
const adding = ref(false)

function openAddIncident() {
  addOpen.value = true
  addForm.value = { title: '', severity: 'medium', description: '', assignee: '' }
  addMsg.value = ''
}

async function confirmAdd() {
  if (!addForm.value.title) { addMsg.value = t('incident.titleRequired'); addOk.value = false; return }
  adding.value = true
  try {
    const r = await store.addIncident(addForm.value.title, addForm.value.severity, addForm.value.description, addForm.value.assignee)
    if (r.s >= 200 && r.s < 300) {
      addMsg.value = t('incident.addSuccess'); addOk.value = true
      await store.fetchIncidents()
      setTimeout(() => { addOpen.value = false }, 1200)
    } else {
      addMsg.value = r.j?.error || t('incident.addFail'); addOk.value = false
    }
  } catch (e) {
    addMsg.value = e.j?.error || t('incident.addFail'); addOk.value = false
  } finally {
    adding.value = false
  }
}

async function onGeneratePostmortem() {
  if (!store.currentIncident) return
  await store.fetchPostmortem(store.currentIncident.id)
}

onMounted(() => {
  store.fetchIncidents()
  store.fetchMetrics()
})
</script>

<style scoped>
.row .col:nth-child(1) { flex: 45; }
.row .col:nth-child(2) { flex: 55; }

.poll-err {
  padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}
.flowbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 10px; }
.row-actions { display: inline-flex; gap: 6px; }
.metrics-row { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 12px; margin-bottom: 16px; }
.metric-value { font-size: 28px; font-weight: 700; color: var(--text); }

.timeline { max-height: 360px; overflow: auto; }
.timeline-item { display: flex; gap: 12px; padding: 8px 0; position: relative; }
.timeline-item:not(:last-child)::before {
  content: ''; position: absolute; left: 5px; top: 22px; bottom: 0; width: 2px;
  background: var(--border);
}
.timeline-dot {
  width: 12px; height: 12px; border-radius: 50%; flex-shrink: 0; margin-top: 4px;
  background: var(--info); border: 2px solid var(--surface);
}
.dot-detected, .dot-created { background: var(--warn); }
.dot-resolved, .dot-closed { background: var(--ok); }
.dot-escalated { background: var(--fail); }
.dot-comment { background: var(--info); }
.timeline-content { flex: 1; }
.timeline-header { display: flex; align-items: center; gap: 8px; margin-bottom: 2px; }
.timeline-type { font-size: 12px; font-weight: 600; text-transform: uppercase; }
.timeline-time { font-size: 11.5px; color: var(--text-3); }
.timeline-text { font-size: 13px; color: var(--text-2); }
.timeline-author { font-size: 11.5px; color: var(--text-3); margin-top: 2px; }

.code-block {
  background: var(--surface-3); color: var(--text); padding: 12px;
  border-radius: var(--radius-sm); overflow: auto;
  font-size: 12px; line-height: 1.5; white-space: pre-wrap; word-break: break-all;
  font-family: var(--font-mono);
}
.postmortem-content { max-height: 300px; }

.modal-mask {
  position: fixed; inset: 0; z-index: 50;
  background: rgba(31,37,64,.42); display: flex; align-items: center; justify-content: center;
}
.modal {
  width: 540px; max-width: 94vw; max-height: 88vh; overflow: auto;
  background: var(--surface); border-radius: var(--radius); box-shadow: var(--shadow);
  padding: 20px 22px;
}
.modal-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
.modal-body .field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 10px; }
.modal-body .field label { margin: 0; }
</style>
