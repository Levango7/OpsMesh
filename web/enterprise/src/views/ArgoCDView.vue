<template>
  <PageShell
    :title="$t('argocd.title')"
    :desc="$t('argocd.subtitle')"
    testid="argo-cd"
    :error="loadErr"
  >
    <div class="btnbar btnbar--bottom">
      <Button variant="primary" size="md" @click="openCreate">
        <Icon name="add" :size="14" />
        {{ $t('argocd.add') }}
      </Button>
      <Button variant="ghost" size="md" @click="fetchApps">
        <Icon name="refresh" :size="14" />
        {{ $t('common.refresh') }}
      </Button>
    </div>

    <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
    <div v-else>
      <DataTable :columns="columns" :rows="apps" row-key="id" :empty-text="$t('argocd.empty')">
        <template #cell-name="{ value }"><code>{{ value }}</code></template>
        <template #cell-namespace="{ value }">
          <span class="tag">{{ value }}</span>
        </template>
        <template #cell-repoURL="{ value }"><code class="repo">{{ value }}</code></template>
        <template #cell-syncPolicy="{ value }">{{ $t('argocd.policy_' + value) }}</template>
        <template #cell-status="{ value }">
          <span class="tag" :class="'st-' + value">{{ $t('argocd.status_' + value) }}</span>
        </template>
        <template #cell-healthStatus="{ value }">
          <span class="tag" :class="'hst-' + value">{{ $t('argocd.health_' + value) }}</span>
        </template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <Button variant="ghost" size="sm" :title="$t('argocd.sync')" @click="onSync(row)"><Icon name="success" :size="13" /></Button>
            <Button variant="ghost" size="sm" @click="openEdit(row)"><Icon name="edit" :size="13" /></Button>
            <Button variant="danger" size="sm" @click="onDelete(row)"><Icon name="delete" :size="13" /></Button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- 新增/编辑应用抽屉 -->
    <DetailDrawer :open="!!form" :title="form && form.id ? $t('argocd.edit') : $t('argocd.add')" @close="form = null">
      <form v-if="form" class="app-form" @submit.prevent="onSave">
        <div class="field">
          <label>{{ $t('argocd.new_name') }}</label>
          <input v-model.trim="form.name" type="text" required />
        </div>
        <div class="field">
          <label>{{ $t('argocd.new_namespace') }}</label>
          <input v-model.trim="form.namespace" type="text" required />
        </div>
        <div class="field">
          <label>{{ $t('argocd.new_repo') }}</label>
          <input v-model.trim="form.repoURL" type="text" placeholder="https://git.example.com/org/app.git" required />
        </div>
        <div class="field">
          <label>{{ $t('argocd.new_path') }}</label>
          <input v-model.trim="form.path" type="text" placeholder="k8s/overlays/prod" />
        </div>
        <div class="field">
          <label>{{ $t('argocd.new_revision') }}</label>
          <input v-model.trim="form.targetRevision" type="text" placeholder="main" />
        </div>
        <div class="field">
          <label>{{ $t('argocd.new_cluster') }}</label>
          <input v-model.trim="form.clusterURL" type="text" placeholder="https://kubernetes.default.svc" />
        </div>
        <div class="field">
          <label>{{ $t('argocd.new_policy') }}</label>
          <select v-model="form.syncPolicy">
            <option value="manual">{{ $t('argocd.policy_manual') }}</option>
            <option value="auto">{{ $t('argocd.policy_auto') }}</option>
          </select>
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar btnbar--bottom">
          <Button variant="primary" size="md" native-type="submit"><Icon name="success" :size="14" /> {{ $t('argocd.save') }}</Button>
          <Button variant="ghost" size="md" native-type="button" @click="form = null">{{ $t('argocd.cancel') }}</Button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 同步确认（替代 confirm） -->
    <ConfirmModal
      v-model="syncConfirm.show"
      data-testid="argocd-sync-confirm-modal"
      :title="$t('argocd.sync')"
      :message="$t('argocd.confirm_sync')"
      @confirm="onSyncConfirm"
    />
    <!-- 删除确认（替代 confirm） -->
    <ConfirmModal
      v-model="deleteConfirm.show"
      data-testid="argocd-delete-confirm-modal"
      :title="$t('argocd.delete')"
      :message="$t('argocd.confirm_delete')"
      @confirm="onDeleteConfirm"
    />
    <!-- 错误提示（替代 alert） -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="argocd-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </PageShell>
</template>

<script setup>
import PageShell from '@/components/PageShell.vue'

// ArgoCD 应用管理页 — GitOps 应用 CRUD + 手动同步
import { ref, reactive, onMounted } from 'vue'
import * as argocdApi from '@/api/argocd'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import Button from "@/components/ui/Button.vue"

const apps = ref([])
const loading = ref(false)
const form = ref(null)
const formError = ref('')
const loadErr = ref('')

// 同步确认弹窗
const syncConfirm = reactive({ show: false, row: null })
// 删除确认弹窗
const deleteConfirm = reactive({ show: false, row: null })
// 错误提示弹窗
const errorConfirm = reactive({ show: false, message: '' })

const columns = [
  { key: 'name', title: t('argocd.name'), slot: 'cell-name' },
  { key: 'namespace', title: t('argocd.namespace'), slot: 'cell-namespace', width: '110px' },
  { key: 'repoURL', title: t('argocd.repo'), slot: 'cell-repoURL' },
  { key: 'syncPolicy', title: t('argocd.policy'), slot: 'cell-syncPolicy', width: '80px' },
  { key: 'status', title: t('argocd.status'), slot: 'cell-status', width: '90px' },
  { key: 'healthStatus', title: t('argocd.health'), slot: 'cell-healthStatus', width: '90px' },
  { key: 'actions', title: t('argocd.actions'), slot: 'cell-actions', width: '110px' }
]

async function fetchApps() {
  loading.value = true
  loadErr.value = ''
  try {
    const r = await argocdApi.listApps()
    apps.value = (r && r.apps) || []
  } catch (e) {
    // 此前静默吞错（只见空表）——统一骨架批次补上可见的加载错误
    apps.value = []
    loadErr.value = e.j?.error || t('common.load_failed')
  } finally {
    loading.value = false
  }
}

function openCreate() {
  formError.value = ''
  form.value = {
    id: null, name: '', namespace: '', repoURL: '', path: '',
    targetRevision: 'main', clusterURL: '', syncPolicy: 'manual'
  }
}

function openEdit(row) {
  formError.value = ''
  form.value = {
    id: row.id,
    name: row.name || '',
    namespace: row.namespace || '',
    repoURL: row.repoURL || '',
    path: row.path || '',
    targetRevision: row.targetRevision || 'main',
    clusterURL: row.clusterURL || '',
    syncPolicy: row.syncPolicy || 'manual'
  }
}

async function onSave() {
  formError.value = ''
  if (!form.value.name || !form.value.namespace || !form.value.repoURL) {
    formError.value = t('argocd.form_required')
    return
  }
  try {
    const body = {
      name: form.value.name,
      namespace: form.value.namespace,
      repoURL: form.value.repoURL,
      path: form.value.path,
      targetRevision: form.value.targetRevision,
      clusterURL: form.value.clusterURL,
      syncPolicy: form.value.syncPolicy
    }
    if (form.value.id) {
      await argocdApi.updateApp(form.value.id, body)
    } else {
      await argocdApi.createApp(body)
    }
    toast.success(t('argocd.saved'))
    form.value = null
    await fetchApps()
  } catch (e) {
    formError.value = e.j?.error || t('argocd.save_failed')
  }
}

function onSync(row) {
  syncConfirm.row = row
  syncConfirm.show = true
}

async function onSyncConfirm() {
  const row = syncConfirm.row
  if (!row) return
  try {
    await argocdApi.syncApp(row.id)
    toast.success(t('argocd.sync_started'))
    await fetchApps()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('argocd.sync_failed')
    errorConfirm.show = true
  }
}

function onDelete(row) {
  deleteConfirm.row = row
  deleteConfirm.show = true
}

async function onDeleteConfirm() {
  const row = deleteConfirm.row
  if (!row) return
  try {
    await argocdApi.deleteApp(row.id)
    await fetchApps()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('argocd.delete_failed')
    errorConfirm.show = true
  }
}

onMounted(fetchApps)
</script>

<style scoped>
.tag {
  display: inline-block; padding: 2px 8px; border-radius: 999px;
  font-size: 12px; background: var(--accent-soft); color: var(--accent);
}
.tag.st-synced { background: var(--ok-bg); color: var(--ok); }
.tag.st-outofsync { background: var(--warn-bg); color: var(--warn); }
.tag.hst-healthy { background: var(--ok-bg); color: var(--ok); }
.tag.hst-degraded { background: var(--fail-bg); color: var(--fail); }
.tag.hst-missing { background: var(--warn-bg); color: var(--warn); }
.repo { font-size: 12px; }
.app-form { display: flex; flex-direction: column; gap: 12px; }
</style>
