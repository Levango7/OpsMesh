<template>
  <div>
    <h2 data-testid="k8s-title">{{ $t('k8s.title') }}</h2>
    <p class="muted">{{ $t('k8s.desc') }}</p>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <div class="row">
      <!-- 左：集群管理 -->
      <div class="col">
        <div class="card">
          <div class="flowbar">
            <h3 style="margin: 0">{{ $t('k8s.clusters') }}</h3>
            <button class="xs primary" @click="openAddCluster" data-testid="k8s-add-cluster-btn">{{ $t('k8s.addCluster') }}</button>
            <button class="xs outline" @click="store.fetchClusters()">↻ {{ $t('common.refresh') }}</button>
          </div>
          <p class="hint">{{ $t('k8s.clustersHint') }}</p>
          <div v-if="store.loading && !store.clusters.length" class="muted">{{ $t('common.loading') }}</div>
          <DataTable
            v-else
            :columns="clusterCols"
            :rows="store.clusters"
            row-key="id"
            :empty-text="$t('k8s.noClusters')"
          >
            <template #cell-name="{ row }"><b>{{ row.name }}</b><br><code>{{ row.id }}</code></template>
            <template #cell-server="{ value }"><code>{{ value || '-' }}</code></template>
            <template #cell-status="{ value }">
              <StatusBadge :status="clusterStatus(value)" :text="clusterStatusText(value)" />
            </template>
            <template #cell-createdAt="{ value }">{{ value || '-' }}</template>
            <template #cell-actions="{ row }">
              <div class="row-actions" @click.stop>
                <button class="xs outline" @click="onTest(row.id)" data-testid="k8s-test-btn">{{ $t('k8s.test') }}</button>
                <button class="xs primary" @click="onManage(row.id)">{{ $t('k8s.resources') }}</button>
                <button class="xs outline" style="color: var(--fail); border-color: var(--fail)" @click="onDelete(row.id)" data-testid="k8s-delete-cluster-btn">{{ $t('k8s.delete') }}</button>
              </div>
            </template>
          </DataTable>
        </div>
      </div>

      <!-- 右：资源管理 -->
      <div class="col">
        <div class="card">
          <h3>{{ $t('k8s.resources') }}</h3>
          <p class="hint">{{ $t('k8s.resourcesHint') }}</p>
          <div class="flowbar">
            <div class="field">
              <label>{{ $t('k8s.selectCluster') }}</label>
              <select v-model="store.currentClusterID" @change="onClusterChange" data-testid="k8s-cluster-select">
                <option value="">— {{ $t('k8s.selectCluster') }} —</option>
                <option v-for="c in store.clusters" :key="c.id" :value="c.id">
                  {{ c.name }}{{ c.server ? ' (' + c.server + ')' : '' }}
                </option>
              </select>
            </div>
            <div class="field">
              <label>{{ $t('k8s.namespace') }}</label>
              <input
                v-model="nsInput"
                :placeholder="$t('k8s.namespaceHint')"
                data-testid="k8s-namespace-input"
                @keyup.enter="onNsChange"
              />
            </div>
            <button class="xs outline" @click="onNsChange">↻ {{ $t('common.refresh') }}</button>
          </div>

          <!-- 资源类型 tab -->
          <div class="res-tabs">
            <button
              v-for="rt in resourceTypes"
              :key="rt"
              class="res-tab"
              :class="{ active: store.resourceType === rt }"
              :data-testid="'k8s-tab-' + rt"
              @click="store.setResourceType(rt)"
            >{{ $t('k8s.' + rt) }}</button>
          </div>

          <div v-if="store.resourcesLoading && !store.resources.length" class="muted">{{ $t('common.loading') }}</div>
          <div v-else-if="!store.currentClusterID" class="muted">{{ $t('k8s.noClusterSelected') }}</div>
          <DataTable
            v-else
            :columns="resourceCols"
            :rows="store.resources"
            :row-key="resourceRowKey"
            :empty-text="$t('k8s.noResources')"
          >
            <template #cell-name="{ value }"><code>{{ value }}</code></template>
            <template #cell-status="{ value }">
              <StatusBadge :status="podStatus(value)" :text="value || '-'" />
            </template>
            <template #cell-image="{ value }"><code>{{ value || '-' }}</code></template>
            <template #cell-clusterIP="{ value }"><code>{{ value || '-' }}</code></template>
            <template #cell-externalIP="{ value }"><code>{{ value || '-' }}</code></template>
            <template #cell-ports="{ value }"><code>{{ value || '-' }}</code></template>
            <template #cell-dataKeys="{ value }">
              <span v-if="value && value.length">{{ value.join(', ') }}</span>
              <span v-else class="muted">—</span>
            </template>
            <template #cell-roles="{ value }">
              <span v-if="value && value.length">{{ value.join(', ') }}</span>
              <span v-else class="muted">—</span>
            </template>
            <template #cell-actions="{ row }">
              <div class="row-actions" @click.stop>
                <template v-if="store.resourceType === 'pods'">
                  <button class="xs outline" @click="onViewLogs(row)" data-testid="k8s-view-logs-btn">{{ $t('k8s.viewLogs') }}</button>
                  <button class="xs outline" style="color: var(--fail); border-color: var(--fail)" @click="onDeletePod(row)" data-testid="k8s-delete-pod-btn">{{ $t('k8s.delete') }}</button>
                </template>
                <template v-else-if="store.resourceType === 'deployments'">
                  <button class="xs outline" @click="onScale(row)" data-testid="k8s-scale-btn">{{ $t('k8s.scale') }}</button>
                  <button class="xs outline" @click="onRestart(row)" data-testid="k8s-restart-btn">{{ $t('k8s.restart') }}</button>
                </template>
                <span v-else class="muted">—</span>
              </div>
            </template>
          </DataTable>
        </div>
      </div>
    </div>

    <!-- 添加集群对话框 -->
    <div v-if="addOpen" class="modal-mask" data-testid="k8s-add-modal" @click.self="addOpen = false">
      <div class="modal">
        <header class="modal-head">
          <h3>{{ $t('k8s.addCluster') }}</h3>
          <button class="xs outline" @click="addOpen = false">✕</button>
        </header>
        <div class="modal-body">
          <div class="field">
            <label>{{ $t('k8s.clusterName') }}</label>
            <input v-model="addForm.name" required data-testid="k8s-add-name" />
          </div>
          <div class="field">
            <label>{{ $t('k8s.server') }}</label>
            <input v-model="addForm.server" placeholder="https://1.2.3.4:6443" required data-testid="k8s-add-server" />
          </div>
          <div class="field">
            <label>{{ $t('k8s.kubeconfig') }}</label>
            <textarea v-model="addForm.kubeconfig" rows="8" :placeholder="$t('k8s.kubeconfigHint')" data-testid="k8s-add-kubeconfig"></textarea>
          </div>
          <div class="btnbar">
            <button class="primary" @click="confirmAdd" :disabled="adding" data-testid="k8s-add-confirm">{{ $t('k8s.confirm') }}</button>
            <button class="outline" @click="addOpen = false">{{ $t('k8s.cancel') }}</button>
          </div>
          <p v-if="addMsg" :class="['msg', addOk ? 'ok' : 'err']">{{ addMsg }}</p>
        </div>
      </div>
    </div>

    <!-- Pod 日志对话框 -->
    <div v-if="logsOpen" class="modal-mask" data-testid="k8s-logs-modal" @click.self="logsOpen = false">
      <div class="modal modal-lg">
        <header class="modal-head">
          <h3>{{ $t('k8s.viewLogs') }} · <code>{{ logsTarget?.name }}</code></h3>
          <button class="xs outline" @click="logsOpen = false">✕</button>
        </header>
        <div class="modal-body">
          <div class="flowbar">
            <div class="field">
              <label>{{ $t('k8s.tailLines') }}</label>
              <input v-model.number="logsForm.tailLines" type="number" min="1" max="10000" style="width: 100px" />
            </div>
            <div class="field">
              <label>{{ $t('k8s.container') }}</label>
              <input v-model="logsForm.container" :placeholder="$t('k8s.containerHint')" />
            </div>
            <button class="xs primary" @click="fetchLogs">{{ $t('k8s.refreshLogs') }}</button>
          </div>
          <pre class="code-block logs-block">{{ logsContent || '—' }}</pre>
        </div>
      </div>
    </div>

    <!-- 扩缩容对话框 -->
    <div v-if="scaleOpen" class="modal-mask" data-testid="k8s-scale-modal" @click.self="scaleOpen = false">
      <div class="modal modal-sm">
        <header class="modal-head">
          <h3>{{ $t('k8s.scale') }} · <code>{{ scaleTarget?.name }}</code></h3>
          <button class="xs outline" @click="scaleOpen = false">✕</button>
        </header>
        <div class="modal-body">
          <p class="muted">{{ $t('k8s.scaleHint') }}</p>
          <p>{{ $t('k8s.scaleCurrent', { replicas: scaleTarget?.replicas || 0, available: scaleTarget?.availableReplicas || 0 }) }}</p>
          <div class="field">
            <label>{{ $t('k8s.targetReplicas') }}</label>
            <input v-model.number="scaleReplicas" type="number" min="0" data-testid="k8s-scale-replicas" />
          </div>
          <div class="btnbar">
            <button class="primary" @click="confirmScale" :disabled="scaling" data-testid="k8s-scale-confirm">{{ $t('k8s.confirm') }}</button>
            <button class="outline" @click="scaleOpen = false">{{ $t('k8s.cancel') }}</button>
          </div>
          <p v-if="scaleMsg" :class="['msg', scaleOk ? 'ok' : 'err']">{{ scaleMsg }}</p>
        </div>
      </div>
    </div>
    <ConfirmModal
      v-model="deleteConfirm.show"
      data-testid="k8s-delete-confirm-modal"
      :title="deleteConfirm.kind === 'restart' ? $t('k8s.restart') : $t('k8s.delete')"
      :message="deleteConfirm.kind === 'restart' ? $t('k8s.restartConfirm') : (deleteConfirm.kind === 'pod' ? $t('k8s.deletePodConfirm') : $t('k8s.deleteClusterConfirm'))"
      @confirm="onDeleteConfirm"
    />
  </div>
</template>

<script setup>
// K8s 管理 — 集群 CRUD + 测试连接 + 资源管理（Pod/Deployment/Service/ConfigMap/Secret/Node）
import { computed, onMounted, reactive, ref } from 'vue'
import { useK8sStore } from '@/stores/k8s'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import { toast } from '@/utils/toast'

const store = useK8sStore()

const resourceTypes = ['pods', 'deployments', 'services', 'configmaps', 'secrets', 'nodes']

const clusterCols = computed(() => [
  { key: 'name', title: t('k8s.col.cluster'), slot: 'cell-name' },
  { key: 'server', title: t('k8s.col.server'), slot: 'cell-server' },
  { key: 'status', title: t('k8s.col.status'), slot: 'cell-status' },
  { key: 'createdAt', title: t('k8s.col.createdAt'), slot: 'cell-createdAt' },
  { key: 'actions', title: t('k8s.col.action'), slot: 'cell-actions', width: '220px' }
])

// 按资源类型动态切换列定义
const resourceCols = computed(() => {
  switch (store.resourceType) {
    case 'pods':
      return [
        { key: 'name', title: 'Pod', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'status', title: t('k8s.col.status'), slot: 'cell-status' },
        { key: 'podIP', title: t('k8s.col.podIP') },
        { key: 'nodeIP', title: t('k8s.col.nodeIP') },
        { key: 'restarts', title: t('k8s.col.restarts') },
        { key: 'age', title: t('k8s.col.age') },
        { key: 'actions', title: t('k8s.col.action'), slot: 'cell-actions', width: '160px' }
      ]
    case 'deployments':
      return [
        { key: 'name', title: 'Deployment', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'replicas', title: t('k8s.col.replicas') },
        { key: 'availableReplicas', title: t('k8s.col.available') },
        { key: 'image', title: t('k8s.col.image'), slot: 'cell-image' },
        { key: 'actions', title: t('k8s.col.action'), slot: 'cell-actions', width: '140px' }
      ]
    case 'services':
      return [
        { key: 'name', title: 'Service', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'type', title: t('k8s.col.type') },
        { key: 'clusterIP', title: t('k8s.col.clusterIP'), slot: 'cell-clusterIP' },
        { key: 'externalIP', title: t('k8s.col.externalIP'), slot: 'cell-externalIP' },
        { key: 'ports', title: t('k8s.col.ports'), slot: 'cell-ports' }
      ]
    case 'configmaps':
      return [
        { key: 'name', title: 'ConfigMap', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'dataKeys', title: t('k8s.col.dataKeys'), slot: 'cell-dataKeys' }
      ]
    case 'secrets':
      return [
        { key: 'name', title: 'Secret', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'type', title: t('k8s.col.type') },
        { key: 'dataKeys', title: t('k8s.col.dataKeys'), slot: 'cell-dataKeys' }
      ]
    case 'nodes':
      return [
        { key: 'name', title: 'Node', slot: 'cell-name' },
        { key: 'status', title: t('k8s.col.status'), slot: 'cell-status' },
        { key: 'roles', title: t('k8s.col.roles'), slot: 'cell-roles' },
        { key: 'version', title: t('k8s.col.version') },
        { key: 'internalIP', title: t('k8s.col.internalIP') },
        { key: 'externalIP', title: t('k8s.col.externalIPNode') },
        { key: 'cpu', title: t('k8s.col.cpu') },
        { key: 'memory', title: t('k8s.col.memory') }
      ]
    default:
      return []
  }
})

const resourceRowKey = computed(() => {
  // nodes 没有 namespace，用 name 作 key
  return 'name'
})

const nsInput = ref('')

function clusterStatus(s) {
  if (s === 'online') return 'success'
  if (s === 'offline') return 'failed'
  return 'info'
}
function clusterStatusText(s) {
  if (s === 'online') return t('k8s.online')
  if (s === 'offline') return t('k8s.offline')
  return s || '-'
}
function podStatus(s) {
  if (!s) return 'info'
  const v = String(s).toLowerCase()
  if (v === 'running' || v === 'ready' || v === 'online') return 'success'
  if (v === 'failed' || v === 'error' || v === 'crashloopbackoff' || v === 'offline') return 'failed'
  if (v === 'pending' || v === 'unknown') return 'info'
  return 'warn'
}

// 集群选择变更
async function onClusterChange() {
  nsInput.value = ''
  await store.selectCluster(store.currentClusterID)
}
function onNsChange() {
  store.setNamespace(nsInput.value.trim())
}
function onManage(id) {
  store.selectCluster(id)
}

// ---- 添加集群 ----
const addOpen = ref(false)
const addForm = ref({ name: '', server: '', kubeconfig: '' })
const addMsg = ref('')
const addOk = ref(false)
const adding = ref(false)

function openAddCluster() {
  addOpen.value = true
  addForm.value = { name: '', server: '', kubeconfig: '' }
  addMsg.value = ''
}

async function confirmAdd() {
  if (!addForm.value.name || !addForm.value.server || !addForm.value.kubeconfig) {
    addMsg.value = t('k8s.addFormIncomplete'); addOk.value = false; return
  }
  adding.value = true
  addMsg.value = t('k8s.submitting'); addOk.value = true
  try {
    const r = await store.createCluster(addForm.value.name, addForm.value.server, addForm.value.kubeconfig)
    if (r.s >= 200 && r.s < 300 && r.j) {
      addMsg.value = t('k8s.addClusterSuccess'); addOk.value = true
      await store.fetchClusters()
      setTimeout(() => { addOpen.value = false }, 1200)
    } else {
      addMsg.value = t('k8s.addFailHttp', { code: (r.s || '?'), msg: (r.j?.error || r.j?.message || '') }); addOk.value = false
    }
  } catch (e) {
    addMsg.value = t('k8s.addFailError', { msg: (e.j?.error || e.message || e) }); addOk.value = false
  } finally {
    adding.value = false
  }
}

// ---- 删除确认弹窗（替代 confirm）：kind 存待执行动作类型，id/row 存目标 ----
const deleteConfirm = reactive({ show: false, kind: '', id: null, row: null })

// ---- 删除集群 ----
function onDelete(id) {
  deleteConfirm.kind = 'cluster'
  deleteConfirm.id = id
  deleteConfirm.row = null
  deleteConfirm.show = true
}

async function onDeleteConfirm() {
  const { kind, id, row } = deleteConfirm
  if (!kind) return
  if (kind === 'cluster') await doDeleteCluster(id)
  else if (kind === 'pod') await doDeletePod(row)
  else if (kind === 'restart') await doRestart()
}

async function doDeleteCluster(id) {
  try {
    const r = await store.removeCluster(id)
    if (r.s === 204 || (r.s >= 200 && r.s < 300)) {
      await store.fetchClusters()
      if (store.currentClusterID === id) {
        store.currentClusterID = ''
        store.resources = []
      }
    } else {
      toast.error(t('k8s.deleteFailHttp', { code: r.s }))
    }
  } catch (e) {
    toast.error(t('k8s.deleteFailError', { msg: (e.j?.error || e.message || e) }))
  }
}

// ---- 测试连接 ----
async function onTest(id) {
  try {
    const r = await store.testCluster(id)
    if (r.s >= 200 && r.s < 300 && r.j) {
      const ok = r.j.status === 'ok' || r.j.status === 'online' || r.j.status === 'success'
      const title = (ok ? t('k8s.testSuccess') : t('k8s.testFail')) + (r.j.message ? '：' + r.j.message : '')
      if (ok) toast.success(title)
      else toast.error(title)
    } else {
      toast.error(t('k8s.testFailHttp', { code: r.s }))
    }
  } catch (e) {
    toast.error(t('k8s.testFailError', { msg: (e.j?.error || e.message || e) }))
  }
}

// ---- Pod 日志 ----
const logsOpen = ref(false)
const logsTarget = ref(null)
const logsForm = ref({ tailLines: 100, container: '' })
const logsContent = ref('')

async function onViewLogs(row) {
  logsTarget.value = row
  logsOpen.value = true
  logsForm.value = { tailLines: 100, container: '' }
  logsContent.value = t('k8s.logsLoading')
  await fetchLogs()
}

async function fetchLogs() {
  if (!logsTarget.value || !store.currentClusterID) return
  try {
    const data = await store.fetchPodLogs(
      store.currentClusterID,
      logsTarget.value.namespace,
      logsTarget.value.name,
      logsForm.value.tailLines,
      logsForm.value.container
    )
    logsContent.value = (data && data.logs) || data || '—'
  } catch (e) {
    logsContent.value = t('k8s.logsFetchFail', { msg: (e.j?.error || e.message || e) })
  }
}

// ---- 删除 Pod ----
function onDeletePod(row) {
  deleteConfirm.kind = 'pod'
  deleteConfirm.id = null
  deleteConfirm.row = row
  deleteConfirm.show = true
}

async function doDeletePod(row) {
  try {
    const r = await store.removePod(store.currentClusterID, row.namespace, row.name)
    if (r.s === 204 || (r.s >= 200 && r.s < 300)) {
      store.fetchResources()
    } else {
      toast.error(t('k8s.deleteFailHttp', { code: r.s }))
    }
  } catch (e) {
    toast.error(t('k8s.deleteFailError', { msg: (e.j?.error || e.message || e) }))
  }
}

// ---- 扩缩容 ----
const scaleOpen = ref(false)
const scaleTarget = ref(null)
const scaleReplicas = ref(0)
const scaleMsg = ref('')
const scaleOk = ref(false)
const scaling = ref(false)

function onScale(row) {
  scaleTarget.value = row
  scaleReplicas.value = row.replicas || 0
  scaleMsg.value = ''
  scaleOpen.value = true
}

async function confirmScale() {
  if (!scaleTarget.value) return
  if (!Number.isInteger(scaleReplicas.value) || scaleReplicas.value < 0) {
    scaleMsg.value = t('k8s.scaleReplicasInvalid'); scaleOk.value = false; return
  }
  scaling.value = true
  scaleMsg.value = t('k8s.submitting'); scaleOk.value = true
  try {
    const r = await store.scaleDeployment(
      store.currentClusterID,
      scaleTarget.value.namespace,
      scaleTarget.value.name,
      scaleReplicas.value
    )
    if (r.s >= 200 && r.s < 300 && r.j) {
      scaleMsg.value = t('k8s.scaleSuccess', { name: (r.j.name || ''), replicas: (r.j.replicas != null ? r.j.replicas : scaleReplicas.value) })
      scaleOk.value = true
      store.fetchResources()
      setTimeout(() => { scaleOpen.value = false }, 1200)
    } else {
      scaleMsg.value = t('k8s.scaleFailHttp', { code: (r.s || '?'), msg: (r.j?.error || r.j?.message || '') }); scaleOk.value = false
    }
  } catch (e) {
    scaleMsg.value = t('k8s.scaleFailError', { msg: (e.j?.error || e.message || e) }); scaleOk.value = false
  } finally {
    scaling.value = false
  }
}

// ---- 重启 Deployment ----
function onRestart(row) {
  deleteConfirm.kind = 'restart'
  deleteConfirm.id = null
  deleteConfirm.row = row
  deleteConfirm.show = true
}

async function doRestart() {
  const row = deleteConfirm.row
  if (!row) return
  try {
    const r = await store.restartDeployment(store.currentClusterID, row.namespace, row.name)
    if (r.s >= 200 && r.s < 300 && r.j) {
      toast.success(t('k8s.restartTriggered', { msg: (r.j.restartedAt || '') }))
      store.fetchResources()
    } else {
      toast.error(t('k8s.restartFailHttp', { code: r.s }))
    }
  } catch (e) {
    toast.error(t('k8s.restartFailError', { msg: (e.j?.error || e.message || e) }))
  }
}

onMounted(() => {
  store.fetchClusters()
})
</script>

<style scoped>
/* 覆盖 tokens.css 全局 .col 的等宽布局，改为左 38 / 右 62 */
.row .col:nth-child(1) { flex: 38; }
.row .col:nth-child(2) { flex: 62; }

.poll-err {
  padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}
.flowbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 10px; }
.flowbar .field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 0; min-width: 200px; }
.flowbar .field label { margin: 0; }
.row-actions { display: inline-flex; gap: 6px; }
.res-tabs { display: flex; flex-wrap: wrap; gap: 6px; margin: 10px 0 12px; border-bottom: 1px solid var(--border); padding-bottom: 8px; }
.res-tab {
  padding: 5px 14px; font-size: 12.5px; border-radius: var(--radius-sm);
  background: var(--surface-3); border: 1px solid var(--border);
  color: var(--text-2); cursor: pointer; transition: .15s;
}
.res-tab:hover { background: var(--bg-soft); color: var(--text); }
.res-tab.active { background: var(--accent); color: #fff; border-color: var(--accent); }

/* 模态对话框 */
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
.modal-lg { width: 760px; }
.modal-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
.modal-body .field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 10px; }
.modal-body .field label { margin: 0; }
.code-block {
  background: var(--surface-3); color: var(--text); padding: 12px;
  border-radius: var(--radius-sm); overflow: auto;
  font-size: 12px; line-height: 1.5; white-space: pre-wrap; word-break: break-all;
  font-family: var(--font-mono);
}
.logs-block { max-height: 480px; min-height: 200px; }
</style>