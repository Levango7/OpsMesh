<template>
  <PageShell :title="$t('k8s.clusters')" :desc="$t('k8s.clustersHint')" testid="k8s" :error="store.error">
    <!-- 子页签：集群管理 / 资源管理（保留原位，位于骨架头部之下） -->
    <K8sSubNav />

    <div class="card">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('k8s.clusters') }}</h3>
        <Button variant="primary" size="sm" @click="openAddCluster" data-testid="k8s-add-cluster-btn">
          <Icon name="add" :size="13" /> {{ $t('k8s.addCluster') }}
        </Button>
        <Button variant="ghost" size="sm" @click="store.fetchClusters()">
          <Icon name="refresh" :size="13" /> {{ $t('common.refresh') }}
        </Button>
      </div>

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
            <Button variant="ghost" size="sm" @click="onTest(row.id)" data-testid="k8s-test-btn">
              {{ $t('k8s.test') }}
            </Button>
            <Button variant="primary" size="sm" @click="onManage(row.id)" data-testid="k8s-manage-btn">
              {{ $t('k8s.resources') }}
            </Button>
            <Button variant="danger" size="sm" @click="onDelete(row.id)" data-testid="k8s-delete-cluster-btn">
              {{ $t('k8s.delete') }}
            </Button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- 添加集群对话框 -->
    <div v-if="addOpen" class="modal-mask modal-mask--top" data-testid="k8s-add-modal" @click.self="addOpen = false">
      <div class="modal modal--fluid">
        <header class="modal-head modal-head--bordered">
          <h3>{{ $t('k8s.addCluster') }}</h3>
          <Button variant="ghost" size="sm" @click="addOpen = false">✕</Button>
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
          <div class="btnbar btnbar--bottom">
            <Button variant="primary" size="sm" @click="confirmAdd" :disabled="adding" data-testid="k8s-add-confirm">
              {{ $t('k8s.confirm') }}
            </Button>
            <Button variant="ghost" size="sm" @click="addOpen = false">{{ $t('k8s.cancel') }}</Button>
          </div>
          <p v-if="addMsg" :class="['msg', addOk ? 'ok' : 'err']">{{ addMsg }}</p>
        </div>
      </div>
    </div>

    <ConfirmModal
      v-model="deleteConfirm.show"
      data-testid="k8s-delete-confirm-modal"
      :title="$t('k8s.delete')"
      :message="$t('k8s.deleteClusterConfirm')"
      @confirm="onDeleteConfirm"
    />
  </PageShell>
</template>

<script setup>
import PageShell from '@/components/PageShell.vue'

// K8s 集群管理子页 — 集群注册 / 测试连接 / 删除 / 跳转到资源视图
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useK8sStore } from '@/stores/k8s'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import K8sSubNav from '@/components/K8sSubNav.vue'
import { Button } from '@/components/ui'
import { toast } from '@/utils/toast'

const router = useRouter()
const store = useK8sStore()

const clusterCols = computed(() => [
  { key: 'name', title: t('k8s.col.cluster'), slot: 'cell-name' },
  { key: 'server', title: t('k8s.col.server'), slot: 'cell-server' },
  { key: 'status', title: t('k8s.col.status'), slot: 'cell-status' },
  { key: 'createdAt', title: t('k8s.col.createdAt'), slot: 'cell-createdAt' },
  { key: 'actions', title: t('k8s.col.action'), slot: 'cell-actions', width: '220px' }
])

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

// ---- 删除确认 ----
const deleteConfirm = reactive({ show: false, id: null })

function onDelete(id) {
  deleteConfirm.id = id
  deleteConfirm.show = true
}

async function onDeleteConfirm() {
  const id = deleteConfirm.id
  if (!id) return
  await doDeleteCluster(id)
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

// ---- 跳转到该集群的资源视图 ----
async function onManage(id) {
  // 先预热命名空间与首批资源，再跳子页，避免资源页出现空白闪烁
  await store.selectCluster(id)
  router.push('/k8s/resources')
}

onMounted(() => {
  store.fetchClusters()
})
</script>

<style scoped>
/* .card / .card__title / .flowbar / .field / .field label / .btnbar / .row-actions /
   .muted / .msg / .msg.ok / .msg.err 及其暗色覆盖
   已改用 styles/pages.css 全局原语与全局暗色规则 */
.flowbar h3 { flex: 1 1 auto; }

.field input,
.field textarea,
.field select {
  padding: 7px 9px;
  border: 1px solid var(--color-hairline, #d3dbe8);
  border-radius: var(--radius-sm, 6px);
  font-size: 13px;
  background: var(--color-surface-1, #ffffff);
  color: var(--color-ink, #1f2937);
  box-sizing: border-box;
}
.field input:focus,
.field textarea:focus,
.field select:focus {
  outline: none;
  border-color: var(--color-primary-900, var(--color-primary, #2563eb));
}

/* 对话框已全部改用 styles/pages.css 全局家族 A（2026-10-30 收编）：
   .modal-mask--top（本页原 z-index 9999）/ .modal--fluid（原 100%+520px 无内距）/
   .modal-head--bordered（原带下边框页头）。模板已挂对应修饰类。 */
</style>
