<template>
  <div class="batch-view">
    <h2 data-testid="batch-title">{{ $t('batch.title') }}</h2>
    <p class="muted">{{ $t('batch.subtitle') }}</p>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <!-- 批量下发表单（详情页不显示） -->
    <div v-if="!detailId" class="card">
      <h3>{{ $t('batch.dispatch_form_title') }}</h3>
      <form @submit.prevent="onSubmit">
        <div class="field">
          <label>{{ $t('batch.agent_select') }}</label>
          <div class="agent-picker">
            <div class="agent-toolbar">
              <Input v-model="agentFilter" :placeholder="$t('batch.filter_placeholder')" />
              <Button variant="ghost" size="small" @click="selectAllAgents">{{ $t('batch.select_all') }}</Button>
              <Button variant="ghost" size="small" @click="agentIDs = []">{{ $t('batch.clear_selection') }}</Button>
              <span class="muted count">{{ agentIDs.length }} / {{ filteredAgents.length }}</span>
            </div>
            <div class="agent-list">
              <label v-for="a in filteredAgents" :key="a.agentID" class="agent-item">
                <input type="checkbox" :value="a.agentID" v-model="agentIDs" />
                <span>{{ a.agentID }}</span>
                <small class="muted">({{ a.hostname || a.ip || '—' }})</small>
              </label>
              <div v-if="!filteredAgents.length" class="muted">{{ $t('batch.no_agents') }}</div>
            </div>
          </div>
        </div>

        <div class="row-2">
          <div class="field">
            <label>{{ $t('batch.type') }}</label>
            <Select v-model="form.type" :options="typeOptions" placeholder="shell" />
          </div>
          <div class="field">
            <label>{{ $t('batch.command') }}</label>
            <Input v-model="form.command" :placeholder="$t('batch.command_placeholder')" required />
          </div>
        </div>
        <div class="row-2">
          <div class="field">
            <label>{{ $t('batch.path') }}</label>
            <Input v-model="form.path" :placeholder="$t('batch.optional')" />
          </div>
          <div class="field">
            <label>{{ $t('batch.content') }}</label>
            <Input v-model="form.content" :placeholder="$t('batch.optional')" />
          </div>
        </div>
        <div class="btnbar">
          <Button
            variant="primary"
            native-type="submit"
            :disabled="submitting || !agentIDs.length"
            data-testid="batch-submit-btn"
          >
            {{ submitting ? $t('common.loading') : $t('batch.submit') }}
          </Button>
        </div>
        <p v-if="msg" :class="['msg', msgOk ? 'ok' : 'err']">{{ msg }}</p>
      </form>
    </div>

    <!-- 当前批量详情 -->
    <div v-if="store.current" class="card">
      <div class="flowbar">
        <h3>{{ $t('batch.status_title') }} · {{ store.current.batchID || '—' }}</h3>
        <Button v-if="store.current.batchID" variant="ghost" size="small" @click="refreshCurrent">
          <Icon name="refresh" :size="13" /> {{ $t('common.refresh') }}
        </Button>
        <Button variant="ghost" size="small" @click="store.clearCurrent()">{{ $t('common.close') }}</Button>
      </div>
      <div class="stats">
        <div class="stat indigo">
          <div class="stat-v">{{ store.current.total || 0 }}</div>
          <div class="stat-l">{{ $t('batch.stat_total') }}</div>
        </div>
        <div class="stat teal">
          <div class="stat-v">{{ store.current.succeeded || 0 }}</div>
          <div class="stat-l">{{ $t('batch.stat_succeeded') }}</div>
        </div>
        <div class="stat rose">
          <div class="stat-v">{{ store.current.failed || 0 }}</div>
          <div class="stat-l">{{ $t('batch.stat_failed') }}</div>
        </div>
        <div class="stat amber">
          <div class="stat-v">{{ store.current.status || '—' }}</div>
          <div class="stat-l">{{ $t('batch.stat_status') }}</div>
        </div>
      </div>
      <Table
        :data="store.current.tasks || []"
        :columns="taskColumns"
        row-key="taskID"
        :empty-text="$t('batch.no_tasks')"
      >
        <template #cell-taskID="{ value }"><code>{{ value }}</code></template>
        <template #cell-status="{ value }">
          <span :class="['status-badge', 'status-' + value]">{{ value }}</span>
        </template>
      </Table>
    </div>

    <!-- 历史批量 -->
    <div v-if="store.history.length" class="card">
      <h3>{{ $t('batch.history_title') }}</h3>
      <Table
        :data="store.history"
        :columns="historyColumns"
        row-key="batchID"
        :empty-text="$t('batch.no_history')"
      >
        <template #cell-batchID="{ value }"><code>{{ value }}</code></template>
        <template #cell-status="{ value }">
          <span :class="['status-badge', 'status-' + value]">{{ value }}</span>
        </template>
        <template #cell-actions="{ row }">
          <Button variant="ghost" size="small" @click="viewBatch(row.batchID)" data-testid="batch-view-btn">{{ $t('batch.view') }}</Button>
        </template>
      </Table>
    </div>

    <!-- 错误提示 -->
    <Modal v-model="errorConfirm.show" :title="$t('common.error')">
      {{ errorConfirm.message }}
    </Modal>
  </div>
</template>

<script setup>
// 批量运维页 — 选择设备 + 批量下发 + 状态查看
// 路由：/batch | /batch-exec | /batch/:id
import { ref, reactive, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useBatchStore } from '@/stores/batch'
import { getAgents } from '@/api/device'
import { t } from '@/i18n'
import { Button, Input, Select, Table, Modal } from '@/components/ui'
import Icon from '@/components/Icon.vue'

const route = useRoute()
const store = useBatchStore()
const agents = ref([])
const agentIDs = ref([])
const agentFilter = ref('')
const form = ref({ type: 'shell', command: '', path: '', content: '' })
const msg = ref('')
const msgOk = ref(false)
const submitting = ref(false)

const errorConfirm = reactive({ show: false, message: '' })

const detailId = computed(() => route.params.id || '')

const typeOptions = [
  { value: 'shell', label: 'shell' },
  { value: 'file', label: 'file' },
  { value: 'service', label: 'service' }
]

const filteredAgents = computed(() => {
  const q = agentFilter.value.trim().toLowerCase()
  if (!q) return agents.value
  return agents.value.filter((a) =>
    (a.agentID && a.agentID.toLowerCase().includes(q)) ||
    (a.hostname && a.hostname.toLowerCase().includes(q)) ||
    (a.ip && a.ip.toLowerCase().includes(q))
  )
})

function selectAllAgents() {
  agentIDs.value = filteredAgents.value.map((a) => a.agentID)
}

const taskColumns = computed(() => [
  { prop: 'taskID', label: t('batch.col_task_id') },
  { prop: 'agentID', label: t('batch.col_agent') },
  { prop: 'status', label: t('batch.col_status') },
  { prop: 'output', label: t('batch.col_output') }
])

const historyColumns = computed(() => [
  { prop: 'batchID', label: t('batch.col_batch_id') },
  { prop: 'total', label: t('batch.stat_total') },
  { prop: 'succeeded', label: t('batch.stat_succeeded') },
  { prop: 'failed', label: t('batch.stat_failed') },
  { prop: 'status', label: t('batch.stat_status') },
  { prop: 'actions', label: t('batch.col_actions'), width: '80px' }
])

async function onSubmit() {
  if (submitting.value) return
  if (!agentIDs.value.length) return
  submitting.value = true
  msg.value = ''
  try {
    const body = {
      deviceIDs: agentIDs.value,
      taskType: form.value.type,
      command: form.value.command,
      path: form.value.path,
      content: form.value.content
    }
    const r = await store.exec(body)
    msg.value = t('batch.dispatch_success', { id: r.j?.batchID || '—' })
    msgOk.value = true
    if (r.j?.batchID) await store.fetchStatus(r.j.batchID)
  } catch (e) {
    msg.value = t('batch.dispatch_failed') + (e.j?.error || e.message || '')
    msgOk.value = false
  } finally {
    submitting.value = false
  }
}

async function viewBatch(id) {
  try { await store.fetchStatus(id) }
  catch (e) {
    errorConfirm.message = e.j?.error || t('batch.fetch_status_failed')
    errorConfirm.show = true
  }
}

async function refreshCurrent() {
  if (!store.current?.batchID) return
  try { await store.fetchStatus(store.current.batchID) }
  catch (e) {
    errorConfirm.message = e.j?.error || t('batch.fetch_status_failed')
    errorConfirm.show = true
  }
}

onMounted(async () => {
  if (detailId.value) {
    try { await store.fetchStatus(detailId.value) }
    catch (e) {
      errorConfirm.message = e.j?.error || t('batch.fetch_status_failed')
      errorConfirm.show = true
    }
  }
  try { agents.value = await getAgents() || [] }
  catch (e) { console.error('fetch agents failed:', e) }
})
</script>

<style scoped>
.card {
  background: var(--surface); border: 1px solid var(--border);
  border-radius: var(--radius); padding: 16px; margin-top: 14px;
  box-shadow: var(--shadow);
}
.card h3 { margin: 0 0 12px; font-size: 14px; }
.flowbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 10px; }
.flowbar h3 { margin: 0; }

.field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 10px; min-width: 220px; }
.field label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.row-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.btnbar { display: flex; gap: 8px; margin-top: 8px; }

.agent-picker { display: flex; flex-direction: column; gap: 8px; }
.agent-toolbar { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.count { font-size: 12px; }
.agent-list {
  display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 6px;
  max-height: 240px; overflow-y: auto;
  border: 1px solid var(--border); border-radius: var(--radius-sm); padding: 8px;
  background: var(--surface-2);
}
.agent-item {
  display: flex; align-items: center; gap: 6px;
  font-size: 12.5px; cursor: pointer; padding: 2px 0;
}
.agent-item small { color: var(--text-3); }

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
.stat-v { font-size: 22px; font-weight: 700; color: var(--text); line-height: 1.1; font-variant-numeric: tabular-nums; }
.stat-l { font-size: 11.5px; color: var(--text-3); margin-top: 4px; }

.status-badge { font-size: 11.5px; font-weight: 600; padding: 2px 8px; border-radius: 999px; }
.status-pending { background: var(--accent-soft); color: var(--accent); }
.status-running { background: var(--accent-soft); color: var(--accent); }
.status-completed { background: var(--ok-soft); color: var(--ok); }
.status-failed { background: var(--fail-soft); color: var(--fail); }
.status-cancelled { background: var(--surface-3); color: var(--text-3); }

.poll-err {
  padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}

@media (max-width: 768px) {
  .stats { grid-template-columns: repeat(2, 1fr); }
  .row-2 { grid-template-columns: 1fr; }
}
</style>
