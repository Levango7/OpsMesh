<template>
  <div>
    <h2>{{ $t('scripts.title') }}</h2>
    <p class="muted">{{ $t('scripts.subtitle') }}</p>

    <div class="btnbar">
      <button class="primary" @click="openCreate">
        <Icon name="add" :size="14" />
        {{ $t('scripts.add') }}
      </button>
      <button class="outline" @click="fetchScripts">
        <Icon name="refresh" :size="14" />
        {{ $t('common.refresh') }}
      </button>
    </div>

    <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
    <div v-else>
      <DataTable :columns="columns" :rows="scripts" row-key="id" :empty-text="$t('scripts.empty')">
        <template #cell-language="{ value }"><code>{{ value }}</code></template>
        <template #cell-enabled="{ row }">
          <span class="status-pill" :class="row.enabled ? 'ok' : 'off'">{{ row.enabled ? $t('scripts.enabled') : $t('scripts.disabled') }}</span>
        </template>
        <template #cell-updatedAt="{ value }">{{ fmtTime(value) }}</template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <button class="xs outline" :title="$t('scripts.execute')" @click="openExec(row)"><Icon name="task" :size="13" /></button>
            <button class="xs outline" :title="$t('scripts.executions')" @click="openExecutions(row)"><Icon name="clipboard" :size="13" /></button>
            <button class="xs outline" @click="openEdit(row)"><Icon name="edit" :size="13" /></button>
            <button class="xs danger" @click="onDelete(row)"><Icon name="delete" :size="13" /></button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- 新增/编辑抽屉 -->
    <DetailDrawer :open="!!form" :title="form && form.id ? $t('scripts.edit') : $t('scripts.add')" @close="form = null">
      <form v-if="form" class="script-form" @submit.prevent="onSave">
        <div class="field">
          <label>{{ $t('scripts.new_name') }}</label>
          <input v-model.trim="form.name" type="text" required />
        </div>
        <div class="field">
          <label>{{ $t('scripts.new_language') }}</label>
          <select v-model="form.language">
            <option value="shell">Shell</option>
            <option value="python">Python</option>
          </select>
        </div>
        <div class="field">
          <label>{{ $t('scripts.new_content') }}</label>
          <textarea v-model="form.content" rows="8" required></textarea>
        </div>
        <div class="field">
          <label>{{ $t('scripts.new_timeout') }}</label>
          <input v-model.number="form.timeoutSec" type="number" min="1" max="600" />
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('scripts.save') }}</button>
          <button type="button" class="outline" @click="form = null">{{ $t('scripts.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 执行抽屉 -->
    <DetailDrawer :open="!!execForm" :title="$t('scripts.execute_title', { name: execForm?.name || '' })" @close="execForm = null">
      <form v-if="execForm" class="script-form" @submit.prevent="onExecute">
        <div class="field">
          <label>{{ $t('scripts.exec_device') }}</label>
          <input v-model.trim="execForm.deviceID" type="text" required />
        </div>
        <div class="field">
          <label>{{ $t('scripts.exec_params') }}</label>
          <input v-model.trim="execForm.params" type="text" :placeholder="$t('scripts.exec_params_placeholder')" />
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="task" :size="14" /> {{ $t('scripts.execute') }}</button>
          <button type="button" class="outline" @click="execForm = null">{{ $t('scripts.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 执行记录抽屉 -->
    <DetailDrawer :open="!!execRow" :title="$t('scripts.executions_title', { name: execRow?.name || '' })" @close="execRow = null">
      <div v-if="execLoading" class="muted">{{ $t('common.loading') }}</div>
      <DataTable v-else :columns="execColumns" :rows="executions" row-key="id" :empty-text="$t('scripts.no_executions')">
        <template #cell-status="{ value }">
          <span class="status-pill" :class="value === 'succeeded' ? 'ok' : (value === 'failed' ? 'bad' : 'warn')">{{ value }}</span>
        </template>
        <template #cell-startedAt="{ value }">{{ fmtTime(value) }}</template>
      </DataTable>
    </DetailDrawer>

    <!-- 删除确认（替代 confirm） -->
    <ConfirmModal
      v-model="deleteConfirm.show"
      data-testid="scripts-delete-confirm-modal"
      :title="$t('scripts.delete')"
      :message="$t('scripts.confirm_delete')"
      @confirm="onDeleteConfirm"
    />
    <!-- 错误提示（替代 alert） -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="scripts-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// 自定义脚本管理页 — 脚本 CRUD + 下发执行 + 执行记录查看
import { ref, computed, reactive, onMounted } from 'vue'
import {
  getScripts, createScript, updateScript, deleteScript,
  executeScript, getScriptExecutions
} from '@/api/script'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import { fmtTime } from '@/composables/useFormatTime'

const scripts = ref([])
const loading = ref(false)
const form = ref(null)
const formError = ref('')

// 执行抽屉 / 执行记录抽屉状态
const execForm = ref(null)
const execRow = ref(null)
const executions = ref([])
const execLoading = ref(false)

// 删除确认弹窗（替代 confirm）
const deleteConfirm = reactive({ show: false, row: null })
// 错误提示弹窗（替代 alert）
const errorConfirm = reactive({ show: false, message: '' })

const columns = computed(() => [
  { key: 'name', title: t('scripts.name') },
  { key: 'language', title: t('scripts.language'), slot: 'cell-language' },
  { key: 'enabled', title: t('scripts.status'), slot: 'cell-enabled' },
  { key: 'timeoutSec', title: t('scripts.timeout') },
  { key: 'updatedAt', title: t('scripts.updated_at'), slot: 'cell-updatedAt' },
  { key: 'actions', title: t('scripts.actions'), slot: 'cell-actions', width: '120px' }
])

const execColumns = computed(() => [
  { key: 'deviceID', title: t('scripts.exec_device') },
  { key: 'status', title: t('scripts.exec_status'), slot: 'cell-status' },
  { key: 'startedAt', title: t('scripts.started_at'), slot: 'cell-startedAt' }
])

async function fetchScripts() {
  loading.value = true
  try {
    const r = await getScripts()
    scripts.value = (r && r.scripts) || []
  } catch {
    scripts.value = []
  } finally {
    loading.value = false
  }
}

function openCreate() {
  formError.value = ''
  form.value = { id: null, name: '', language: 'shell', content: '', timeoutSec: 60 }
}

function openEdit(row) {
  formError.value = ''
  form.value = {
    id: row.id,
    name: row.name || '',
    language: row.language || 'shell',
    content: row.content || '',
    timeoutSec: row.timeoutSec || 60
  }
}

async function onSave() {
  formError.value = ''
  try {
    const body = {
      name: form.value.name,
      language: form.value.language,
      content: form.value.content,
      timeoutSec: form.value.timeoutSec
    }
    if (form.value.id) await updateScript(form.value.id, body)
    else await createScript(body)
    form.value = null
    toast.success(t('scripts.saved'))
    await fetchScripts()
  } catch (e) {
    formError.value = e.j?.error || t('scripts.save_failed')
  }
}

function openExec(row) {
  formError.value = ''
  execForm.value = { id: row.id, name: row.name, deviceID: '', params: '' }
}

async function onExecute() {
  formError.value = ''
  if (!execForm.value.deviceID) {
    formError.value = t('scripts.need_device')
    return
  }
  try {
    const r = await executeScript(execForm.value.id, {
      deviceID: execForm.value.deviceID,
      params: execForm.value.params
    })
    const j = r?.j || r
    toast.success(t('scripts.executed') + (j?.taskID ? ` (task: ${j.taskID})` : ''))
    execForm.value = null
  } catch (e) {
    formError.value = e.j?.error || t('scripts.execute_failed')
  }
}

async function openExecutions(row) {
  execRow.value = row
  execLoading.value = true
  executions.value = []
  try {
    const r = await getScriptExecutions(row.id)
    executions.value = (r && r.executions) || []
  } catch {
    executions.value = []
  } finally {
    execLoading.value = false
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
    await deleteScript(row.id)
    toast.success(t('scripts.deleted'))
    await fetchScripts()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('scripts.delete_failed')
    errorConfirm.show = true
  }
}

onMounted(fetchScripts)
</script>

<style scoped>
.row-actions { display: flex; gap: 4px; }
.status-pill {
  display: inline-flex; align-items: center;
  padding: 1px 9px; border-radius: 999px; font-size: 12px; font-weight: 600;
}
.status-pill.ok { background: var(--accent-soft); color: var(--accent); }
.status-pill.off { background: var(--surface-3); color: var(--text-3); }
.status-pill.bad { background: var(--fail-bg); color: var(--fail); }
.status-pill.warn { background: var(--warn-bg); color: var(--warn); }
.script-form { display: flex; flex-direction: column; gap: 12px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field > label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.field textarea {
  font-family: var(--font-mono, monospace);
  font-size: 12.5px; resize: vertical;
}
.btnbar { margin-top: 8px; }
</style>
