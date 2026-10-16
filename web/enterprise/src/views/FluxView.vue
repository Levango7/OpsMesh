<template>
  <div>
    <h2>{{ $t('flux.title') }}</h2>
    <p class="muted">{{ $t('flux.subtitle') }}</p>

    <div class="btnbar">
      <button class="primary" @click="openCreate">
        <Icon name="add" :size="14" />
        {{ $t('flux.add') }}
      </button>
      <button class="outline" @click="fetchApps">
        <Icon name="refresh" :size="14" />
        {{ $t('common.refresh') }}
      </button>
    </div>

    <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
    <div v-else>
      <DataTable :columns="columns" :rows="apps" row-key="id" :empty-text="$t('flux.empty')">
        <template #cell-name="{ value }"><code>{{ value }}</code></template>
        <template #cell-namespace="{ value }"><span class="tag">{{ value }}</span></template>
        <template #cell-type="{ value }">{{ $t('flux.type_' + value) }}</template>
        <template #cell-sourceURL="{ value }"><code class="repo">{{ value }}</code></template>
        <template #cell-sourcePath="{ value }"><code>{{ value }}</code></template>
        <template #cell-syncPolicy="{ value }">{{ $t('flux.policy_' + value) }}</template>
        <template #cell-status="{ value }">
          <span class="tag" :class="'st-' + value">{{ $t('flux.status_' + value) }}</span>
        </template>
        <template #cell-healthStatus="{ value }">
          <span class="tag" :class="'hst-' + value">{{ $t('flux.health_' + value) }}</span>
        </template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <button class="xs outline" :title="$t('flux.sync')" @click="onSync(row)"><Icon name="success" :size="13" /></button>
            <button class="xs outline" @click="openEdit(row)"><Icon name="edit" :size="13" /></button>
            <button class="xs danger" @click="onDelete(row)"><Icon name="delete" :size="13" /></button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- 新增/编辑应用抽屉 -->
    <DetailDrawer :open="!!form" :title="form && form.id ? $t('flux.edit') : $t('flux.add')" @close="form = null">
      <form v-if="form" class="app-form" @submit.prevent="onSave">
        <div class="field">
          <label>{{ $t('flux.new_name') }}</label>
          <input v-model.trim="form.name" type="text" required />
        </div>
        <div class="field">
          <label>{{ $t('flux.new_namespace') }}</label>
          <input v-model.trim="form.namespace" type="text" required />
        </div>
        <div class="field">
          <label>{{ $t('flux.new_type') }}</label>
          <select v-model="form.type">
            <option value="kustomization">{{ $t('flux.type_kustomization') }}</option>
            <option value="helmrelease">{{ $t('flux.type_helmrelease') }}</option>
          </select>
        </div>
        <div class="field">
          <label>{{ $t('flux.new_repo') }}</label>
          <input v-model.trim="form.sourceURL" type="text" placeholder="https://git.example.com/org/infra.git" required />
        </div>
        <div class="field">
          <label>{{ $t('flux.new_path') }}</label>
          <input v-model.trim="form.sourcePath" type="text" placeholder="clusters/prod" />
        </div>
        <div class="field">
          <label>{{ $t('flux.new_revision') }}</label>
          <input v-model.trim="form.targetRevision" type="text" placeholder="main" />
        </div>
        <div class="field">
          <label>{{ $t('flux.new_cluster') }}</label>
          <input v-model.trim="form.clusterURL" type="text" placeholder="https://kubernetes.default.svc" />
        </div>
        <div class="field">
          <label>{{ $t('flux.new_policy') }}</label>
          <select v-model="form.syncPolicy">
            <option value="manual">{{ $t('flux.policy_manual') }}</option>
            <option value="auto">{{ $t('flux.policy_auto') }}</option>
          </select>
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('flux.save') }}</button>
          <button type="button" class="outline" @click="form = null">{{ $t('flux.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 删除确认 -->
    <ConfirmModal
      v-model="deleteConfirm.show"
      :title="$t('flux.delete')"
      :message="$t('flux.confirm_delete', { name: deleteConfirm.row?.name })"
      @confirm="onConfirmDelete"
    />
  </div>
</template>

<script setup>
import { reactive, ref } from 'vue'
import { listFluxApps, createFluxApp, getFluxApp, updateFluxApp, deleteFluxApp, syncFluxApp } from '@/api/flux'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'

const columns = [
  { key: 'name', title: t('flux.name'), slot: 'cell-name' },
  { key: 'namespace', title: t('flux.namespace'), slot: 'cell-namespace', width: '110px' },
  { key: 'type', title: t('flux.type'), slot: 'cell-type', width: '90px' },
  { key: 'sourceURL', title: t('flux.source'), slot: 'cell-sourceURL' },
  { key: 'sourcePath', title: t('flux.path'), slot: 'cell-sourcePath' },
  { key: 'syncPolicy', title: t('flux.policy'), slot: 'cell-syncPolicy', width: '80px' },
  { key: 'status', title: t('flux.status'), slot: 'cell-status', width: '90px' },
  { key: 'healthStatus', title: t('flux.health'), slot: 'cell-healthStatus', width: '90px' },
  { key: 'actions', title: t('flux.actions'), slot: 'cell-actions', width: '110px' }
]

const form = ref(null)
const formError = ref('')
const apps = ref([])
const loading = ref(false)
const deleteConfirm = reactive({ show: false, row: null })

async function fetchApps() {
  loading.value = true
  try {
    const resp = await listFluxApps()
    apps.value = (resp && resp.apps) || []
  } catch (e) {
    apps.value = []
  } finally {
    loading.value = false
  }
}

function openCreate() {
  formError.value = ''
  form.value = {
    id: null, name: '', namespace: '', type: 'kustomization',
    sourceURL: '', sourcePath: '', targetRevision: '',
    clusterURL: '', syncPolicy: 'manual'
  }
}

function openEdit(row) {
  formError.value = ''
  form.value = {
    id: row.id,
    name: row.name || '',
    namespace: row.namespace || '',
    type: row.type || 'kustomization',
    sourceURL: row.sourceURL || '',
    sourcePath: row.sourcePath || '',
    targetRevision: row.targetRevision || '',
    clusterURL: row.clusterURL || '',
    syncPolicy: row.syncPolicy || 'manual'
  }
}

async function onSave() {
  formError.value = ''
  if (!form.value.name || !form.value.namespace || !form.value.sourceURL) {
    formError.value = t('flux.form_required')
    return
  }
  try {
    const body = {
      name: form.value.name,
      namespace: form.value.namespace,
      type: form.value.type,
      sourceURL: form.value.sourceURL,
      sourcePath: form.value.sourcePath,
      targetRevision: form.value.targetRevision,
      clusterURL: form.value.clusterURL,
      syncPolicy: form.value.syncPolicy,
    }
    if (form.value.id) {
      await updateFluxApp(form.value.id, body)
    } else {
      await createFluxApp(body)
    }
    toast.success(t('flux.saved'))
    form.value = null
    await fetchApps()
  } catch (e) {
    formError.value = e.j?.error || t('flux.save_failed')
  }
}

async function onSync(row) {
  try {
    await syncFluxApp(row.id)
    await fetchApps()
  } catch (e) {
    formError.value = e.j?.error || t('flux.sync_failed')
  }
}

async function onDelete(row) {
  deleteConfirm.row = row
  deleteConfirm.show = true
}

async function onConfirmDelete() {
  if (deleteConfirm.row) {
    try {
      await deleteFluxApp(deleteConfirm.row.id)
      await fetchApps()
    } catch (e) {
      formError.value = e.j?.error || t('flux.delete_failed')
    }
  }
  deleteConfirm.show = false
  deleteConfirm.row = null
}

fetchApps()
</script>

<style scoped>
.btnbar {
  display: flex;
  gap: 10px;
  margin-bottom: 14px;
}
.app-form {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 12px;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.field label {
  font-size: 12px;
  color: var(--text-2);
}
.field input,
.field select {
  padding: 6px 8px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  font-size: 13px;
}
.msg.err {
  grid-column: 1 / -1;
  color: var(--fail);
  font-size: 12.5px;
  margin-bottom: 8px;
}
.btnbar {
  grid-column: 1 / -1;
  display: flex;
  gap: 8px;
  justify-content: flex-end;
  margin-top: 8px;
}
.row-actions {
  display: flex;
  gap: 6px;
}
</style>