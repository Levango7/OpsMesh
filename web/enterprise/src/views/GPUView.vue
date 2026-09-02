<template>
  <div>
    <h2 data-testid="gpu-title">{{ $t('gpu.title') }}</h2>
    <p class="muted">{{ $t('gpu.desc') }}</p>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <!-- 指标卡片 -->
    <div class="metrics-row">
      <MetricsCard :title="$t('gpu.totalNodes')" icon="device" accent="--accent">
        <div class="metric-value">{{ store.totalGpu }}</div>
      </MetricsCard>
      <MetricsCard :title="$t('gpu.healthyNodes')" icon="success" accent="--ok">
        <div class="metric-value">{{ store.healthyNodes }}</div>
      </MetricsCard>
      <MetricsCard :title="$t('gpu.activeWorkloads')" icon="task" accent="--warn">
        <div class="metric-value">{{ store.activeWorkloads }}</div>
      </MetricsCard>
      <MetricsCard :title="$t('gpu.models')" icon="cmdb" accent="--info">
        <div class="metric-value">{{ store.models.length }}</div>
      </MetricsCard>
    </div>

    <div class="row">
      <!-- 左：GPU 节点 -->
      <div class="col">
        <div class="card">
          <div class="flowbar">
            <h3 style="margin: 0">{{ $t('gpu.nodes') }}</h3>
            <button class="xs outline" @click="store.fetchNodes()">↻ {{ $t('common.refresh') }}</button>
          </div>
          <div v-if="store.loading && !store.nodes.length" class="muted">{{ $t('common.loading') }}</div>
          <DataTable v-else :columns="nodeCols" :rows="store.nodes" row-key="id" :empty-text="$t('gpu.noNodes')">
            <template #cell-name="{ row }">
              <b>{{ row.name }}</b><br><code>{{ row.id }}</code>
            </template>
            <template #cell-model="{ value }"><code>{{ value || '-' }}</code></template>
            <template #cell-status="{ value }">
              <StatusBadge :status="nodeStatus(value)" :text="value || '-'" />
            </template>
            <template #cell-health="{ value }">
              <StatusBadge :status="healthStatus(value)" :text="value || '-'" />
            </template>
            <template #cell-utilization="{ value }">
              <div class="util-bar">
                <div class="util-fill" :style="{ width: (value || 0) + '%' }" />
                <span class="util-text">{{ value || 0 }}%</span>
              </div>
            </template>
            <template #cell-actions="{ row }">
              <div class="row-actions" @click.stop>
                <button class="xs outline" @click="store.selectNode(row.id)">{{ $t('gpu.metrics') }}</button>
              </div>
            </template>
          </DataTable>
        </div>
      </div>

      <!-- 右：指标图表 + 工作负载 -->
      <div class="col">
        <div class="card">
          <h3>{{ $t('gpu.utilizationChart') }}</h3>
          <p class="hint">{{ store.selectedNode ? store.selectedNode.name : $t('gpu.selectNodeHint') }}</p>
          <div class="chart-area">
            <div v-if="store.metricsLoading" class="muted">{{ $t('common.loading') }}</div>
            <div v-else-if="!store.metrics.length" class="muted chart-empty">{{ $t('gpu.noMetrics') }}</div>
            <div v-else class="mock-chart">
              <div
                v-for="(m, idx) in store.metrics"
                :key="idx"
                class="chart-bar"
                :style="{ height: (m.utilization || 0) + '%' }"
                :title="m.utilization + '%'"
              />
            </div>
          </div>
        </div>

        <div class="card">
          <div class="flowbar">
            <h3 style="margin: 0">{{ $t('gpu.workloads') }}</h3>
            <button class="xs primary" @click="openAddWorkload" data-testid="gpu-add-workload-btn">{{ $t('gpu.addWorkload') }}</button>
            <button class="xs outline" @click="store.fetchWorkloads()">↻ {{ $t('common.refresh') }}</button>
          </div>
          <div v-if="store.workloadsLoading && !store.workloads.length" class="muted">{{ $t('common.loading') }}</div>
          <DataTable v-else :columns="workloadCols" :rows="store.workloads" row-key="id" :empty-text="$t('gpu.noWorkloads')">
            <template #cell-name="{ row }"><b>{{ row.name }}</b><br><code>{{ row.id }}</code></template>
            <template #cell-type="{ value }"><StatusBadge :status="workloadTypeStatus(value)" :text="value || '-'" /></template>
            <template #cell-status="{ value }">
              <StatusBadge :status="workloadStatus(value)" :text="value || '-'" />
            </template>
            <template #cell-gpuCount="{ value }">{{ value || 0 }}</template>
            <template #cell-actions="{ row }">
              <div class="row-actions" @click.stop>
                <button class="xs outline" style="color: var(--fail); border-color: var(--fail)" @click="onDeleteWorkload(row.id)" data-testid="gpu-delete-workload-btn">{{ $t('common.delete') }}</button>
              </div>
            </template>
          </DataTable>
        </div>
      </div>
    </div>

    <!-- 模型管理 -->
    <div class="card">
      <div class="flowbar">
        <h3 style="margin: 0">{{ $t('gpu.models') }}</h3>
        <button class="xs primary" @click="openPullModel" data-testid="gpu-pull-model-btn">{{ $t('gpu.pullModel') }}</button>
        <button class="xs outline" @click="store.fetchModels()">↻ {{ $t('common.refresh') }}</button>
      </div>
      <div v-if="store.modelsLoading && !store.models.length" class="muted">{{ $t('common.loading') }}</div>
      <DataTable v-else :columns="modelCols" :rows="store.models" row-key="name" :empty-text="$t('gpu.noModels')">
        <template #cell-name="{ value }"><code>{{ value }}</code></template>
        <template #cell-size="{ value }">{{ value || '-' }}</template>
        <template #cell-status="{ value }">
          <StatusBadge :status="modelStatus(value)" :text="value || '-'" />
        </template>
        <template #cell-actions="{ row }">
          <div class="row-actions" @click.stop>
            <button class="xs outline" style="color: var(--fail); border-color: var(--fail)" @click="onDeleteModel(row.name)">{{ $t('common.delete') }}</button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- 配额概览 -->
    <div class="card">
      <div class="flowbar">
        <h3 style="margin: 0">{{ $t('gpu.quotas') }}</h3>
        <button class="xs outline" @click="store.fetchQuotas()">↻ {{ $t('common.refresh') }}</button>
      </div>
      <DataTable :columns="quotaCols" :rows="store.quotas" row-key="tenantId" :empty-text="$t('gpu.noQuotas')">
        <template #cell-tenantId="{ value }"><code>{{ value }}</code></template>
        <template #cell-usage="{ row }">
          <div class="util-bar">
            <div class="util-fill" :style="{ width: quotaPercent(row) + '%' }" />
            <span class="util-text">{{ row.usedGpu || 0 }}/{{ row.totalGpu || 0 }}</span>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- 添加工作负载对话框 -->
    <div v-if="addOpen" class="modal-mask" data-testid="gpu-add-workload-modal" @click.self="addOpen = false">
      <div class="modal">
        <header class="modal-head">
          <h3>{{ $t('gpu.addWorkload') }}</h3>
          <button class="xs outline" @click="addOpen = false">✕</button>
        </header>
        <div class="modal-body">
          <div class="field">
            <label>{{ $t('gpu.workloadName') }}</label>
            <input v-model="addForm.name" required data-testid="gpu-add-workload-name" />
          </div>
          <div class="field">
            <label>{{ $t('gpu.workloadType') }}</label>
            <select v-model="addForm.type" data-testid="gpu-add-workload-type">
              <option value="inference">{{ $t('gpu.typeInference') }}</option>
              <option value="training">{{ $t('gpu.typeTraining') }}</option>
              <option value="fine-tuning">{{ $t('gpu.typeFineTuning') }}</option>
            </select>
          </div>
          <div class="field">
            <label>{{ $t('gpu.model') }}</label>
            <input v-model="addForm.model" placeholder="llama3.1:8b" data-testid="gpu-add-workload-model" />
          </div>
          <div class="field">
            <label>{{ $t('gpu.gpuCount') }}</label>
            <input v-model.number="addForm.gpuCount" type="number" min="1" max="16" data-testid="gpu-add-workload-gpucount" />
          </div>
          <div class="field">
            <label>{{ $t('gpu.node') }}</label>
            <select v-model="addForm.nodeId" data-testid="gpu-add-workload-node">
              <option value="">— {{ $t('gpu.selectNode') }} —</option>
              <option v-for="n in store.nodes" :key="n.id" :value="n.id">{{ n.name }}</option>
            </select>
          </div>
          <div class="btnbar">
            <button class="primary" @click="confirmAddWorkload" :disabled="adding" data-testid="gpu-add-workload-confirm">{{ $t('common.confirm') }}</button>
            <button class="outline" @click="addOpen = false">{{ $t('common.cancel') }}</button>
          </div>
          <p v-if="addMsg" :class="['msg', addOk ? 'ok' : 'err']">{{ addMsg }}</p>
        </div>
      </div>
    </div>

    <!-- 拉取模型对话框 -->
    <div v-if="pullOpen" class="modal-mask" data-testid="gpu-pull-model-modal" @click.self="pullOpen = false">
      <div class="modal modal-sm">
        <header class="modal-head">
          <h3>{{ $t('gpu.pullModel') }}</h3>
          <button class="xs outline" @click="pullOpen = false">✕</button>
        </header>
        <div class="modal-body">
          <div class="field">
            <label>{{ $t('gpu.modelName') }}</label>
            <input v-model="pullForm.name" placeholder="llama3.1:8b" data-testid="gpu-pull-model-name" />
          </div>
          <div class="field">
            <label>{{ $t('gpu.node') }}</label>
            <select v-model="pullForm.nodeId" data-testid="gpu-pull-model-node">
              <option value="">— {{ $t('gpu.selectNode') }} —</option>
              <option v-for="n in store.nodes" :key="n.id" :value="n.id">{{ n.name }}</option>
            </select>
          </div>
          <div class="btnbar">
            <button class="primary" @click="confirmPullModel" :disabled="pulling" data-testid="gpu-pull-model-confirm">{{ $t('common.confirm') }}</button>
            <button class="outline" @click="pullOpen = false">{{ $t('common.cancel') }}</button>
          </div>
          <p v-if="pullMsg" :class="['msg', pullOk ? 'ok' : 'err']">{{ pullMsg }}</p>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { useGpuStore } from '@/stores/gpu'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import MetricsCard from '@/components/MetricsCard.vue'
import Icon from '@/components/Icon.vue'

const store = useGpuStore()

const nodeCols = computed(() => [
  { key: 'name', title: t('gpu.col.node'), slot: 'cell-name' },
  { key: 'model', title: t('gpu.col.model'), slot: 'cell-model' },
  { key: 'status', title: t('gpu.col.status'), slot: 'cell-status' },
  { key: 'health', title: t('gpu.col.health'), slot: 'cell-health' },
  { key: 'utilization', title: t('gpu.col.utilization'), slot: 'cell-utilization' },
  { key: 'actions', title: t('gpu.col.action'), slot: 'cell-actions', width: '100px' }
])

const workloadCols = computed(() => [
  { key: 'name', title: t('gpu.col.workload'), slot: 'cell-name' },
  { key: 'type', title: t('gpu.col.type'), slot: 'cell-type' },
  { key: 'status', title: t('gpu.col.status'), slot: 'cell-status' },
  { key: 'gpuCount', title: t('gpu.col.gpuCount'), slot: 'cell-gpuCount' },
  { key: 'actions', title: t('gpu.col.action'), slot: 'cell-actions', width: '100px' }
])

const modelCols = computed(() => [
  { key: 'name', title: t('gpu.col.modelName'), slot: 'cell-name' },
  { key: 'size', title: t('gpu.col.size'), slot: 'cell-size' },
  { key: 'status', title: t('gpu.col.status'), slot: 'cell-status' },
  { key: 'actions', title: t('gpu.col.action'), slot: 'cell-actions', width: '100px' }
])

const quotaCols = computed(() => [
  { key: 'tenantId', title: t('gpu.col.tenant'), slot: 'cell-tenantId' },
  { key: 'totalGpu', title: t('gpu.col.totalGpu') },
  { key: 'usage', title: t('gpu.col.usage'), slot: 'cell-usage' }
])

function nodeStatus(s) {
  if (s === 'online' || s === 'ready') return 'success'
  if (s === 'offline' || s === 'error') return 'failed'
  return 'info'
}
function healthStatus(s) {
  if (s === 'healthy') return 'success'
  if (s === 'degraded') return 'warn'
  if (s === 'unhealthy') return 'failed'
  return 'info'
}
function workloadStatus(s) {
  if (s === 'running') return 'success'
  if (s === 'failed' || s === 'error') return 'failed'
  if (s === 'pending') return 'info'
  return 'warn'
}
function workloadTypeStatus(s) {
  if (s === 'inference') return 'info'
  if (s === 'training') return 'warn'
  return 'info'
}
function modelStatus(s) {
  if (s === 'ready' || s === 'available') return 'success'
  if (s === 'pulling') return 'warn'
  return 'info'
}
function quotaPercent(row) {
  if (!row.totalGpu) return 0
  return Math.round(((row.usedGpu || 0) / row.totalGpu) * 100)
}

// ---- 添加工作负载 ----
const addOpen = ref(false)
const addForm = ref({ name: '', type: 'inference', model: '', gpuCount: 1, nodeId: '' })
const addMsg = ref('')
const addOk = ref(false)
const adding = ref(false)

function openAddWorkload() {
  addOpen.value = true
  addForm.value = { name: '', type: 'inference', model: '', gpuCount: 1, nodeId: '' }
  addMsg.value = ''
}

async function confirmAddWorkload() {
  if (!addForm.value.name) { addMsg.value = t('gpu.nameRequired'); addOk.value = false; return }
  adding.value = true
  try {
    const r = await store.addWorkload(addForm.value.name, addForm.value.type, addForm.value.model, addForm.value.gpuCount, addForm.value.nodeId)
    if (r.s >= 200 && r.s < 300) {
      addMsg.value = t('gpu.addWorkloadSuccess'); addOk.value = true
      await store.fetchWorkloads()
      setTimeout(() => { addOpen.value = false }, 1200)
    } else {
      addMsg.value = r.j?.error || t('gpu.addWorkloadFail'); addOk.value = false
    }
  } catch (e) {
    addMsg.value = e.j?.error || t('gpu.addWorkloadFail'); addOk.value = false
  } finally {
    adding.value = false
  }
}

async function onDeleteWorkload(id) {
  if (!confirm(t('gpu.deleteWorkloadConfirm'))) return
  try {
    const r = await store.removeWorkload(id)
    if (r.s === 204 || (r.s >= 200 && r.s < 300)) {
      await store.fetchWorkloads()
    }
  } catch (e) {
    alert(e.j?.error || t('gpu.deleteWorkloadFail'))
  }
}

// ---- 拉取模型 ----
const pullOpen = ref(false)
const pullForm = ref({ name: '', nodeId: '' })
const pullMsg = ref('')
const pullOk = ref(false)
const pulling = ref(false)

function openPullModel() {
  pullOpen.value = true
  pullForm.value = { name: '', nodeId: '' }
  pullMsg.value = ''
}

async function confirmPullModel() {
  if (!pullForm.value.name) { pullMsg.value = t('gpu.modelNameRequired'); pullOk.value = false; return }
  pulling.value = true
  try {
    const r = await store.pullModel(pullForm.value.name, pullForm.value.nodeId)
    if (r.s >= 200 && r.s < 300) {
      pullMsg.value = t('gpu.pullModelSuccess'); pullOk.value = true
      await store.fetchModels()
      setTimeout(() => { pullOpen.value = false }, 1200)
    } else {
      pullMsg.value = r.j?.error || t('gpu.pullModelFail'); pullOk.value = false
    }
  } catch (e) {
    pullMsg.value = e.j?.error || t('gpu.pullModelFail'); pullOk.value = false
  } finally {
    pulling.value = false
  }
}

async function onDeleteModel(name) {
  if (!confirm(t('gpu.deleteModelConfirm'))) return
  try {
    const r = await store.removeModel(name)
    if (r.s === 204 || (r.s >= 200 && r.s < 300)) {
      await store.fetchModels()
    }
  } catch (e) {
    alert(e.j?.error || t('gpu.deleteModelFail'))
  }
}

onMounted(() => {
  store.fetchNodes()
  store.fetchWorkloads()
  store.fetchModels()
  store.fetchQuotas()
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

.util-bar {
  position: relative; height: 20px; background: var(--surface-3);
  border-radius: 999px; overflow: hidden; min-width: 80px;
}
.util-fill {
  position: absolute; left: 0; top: 0; bottom: 0;
  background: var(--accent); border-radius: 999px; transition: width .3s;
}
.util-text {
  position: relative; z-index: 1; font-size: 11px; font-weight: 600;
  padding: 0 8px; line-height: 20px; color: var(--text);
}

.chart-area { min-height: 160px; padding: 12px 0; }
.chart-empty { display: flex; align-items: center; justify-content: center; height: 160px; }
.mock-chart {
  display: flex; align-items: flex-end; gap: 3px; height: 140px; padding: 0 4px;
}
.chart-bar {
  flex: 1; min-width: 6px; background: var(--accent); border-radius: 2px 2px 0 0;
  transition: height .3s; opacity: .8;
}
.chart-bar:hover { opacity: 1; }

.modal-mask {
  position: fixed; inset: 0; z-index: 50;
  background: var(--modal-mask); display: flex; align-items: center; justify-content: center;
}
.modal {
  width: 540px; max-width: 94vw; max-height: 88vh; overflow: auto;
  background: var(--surface); border-radius: var(--radius); box-shadow: var(--shadow);
  padding: 20px 22px;
}
.modal-sm { width: 420px; }
.modal-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
.modal-body .field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 10px; }
.modal-body .field label { margin: 0; }
</style>
