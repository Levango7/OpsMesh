<template>
  <PageShell
    :title="$t('scripts.title')"
    :desc="$t('scripts.subtitle')"
    testid="scripts"
    :error="loadErr"
  >
    <div class="btnbar btnbar--bottom">
      <Button variant="primary" size="md" @click="openCreate" data-testid="scripts-opencreate-btn" :disabled="!canWrite" :title="canWrite ? '' : $t('common.write_denied')">
        <Icon name="add" :size="14" />
        {{ $t('scripts.add') }}
      </Button>
      <Button variant="ghost" size="md" @click="fetchScripts">
        <Icon name="refresh" :size="14" />
        {{ $t('common.refresh') }}
      </Button>
    </div>

    <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
    <div v-else>
      <div v-if="!canWrite" class="muted" data-testid="write-denied-hint">{{ $t('common.write_denied') }}</div>
      <DataTable :loading="loading" :columns="columns" :rows="scripts" row-key="id" :empty-text="$t('scripts.empty')">
        <template #cell-language="{ value }"><code>{{ value }}</code></template>
        <template #cell-enabled="{ row }">
          <span class="status-pill" :class="row.enabled ? 'ok' : 'off'">{{ row.enabled ? $t('scripts.enabled') : $t('scripts.disabled') }}</span>
        </template>
        <template #cell-updatedAt="{ value }">{{ fmtTime(value) }}</template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <Button variant="ghost" size="sm" :title="canWrite ? ($t('scripts.execute')) : $t('common.write_denied')" @click="openExec(row)" data-testid="scripts-openexec-btn" :disabled="!canWrite"><Icon name="task" :size="13" /></Button>
            <Button variant="ghost" size="sm" :title="$t('scripts.executions')" @click="openExecutions(row)"><Icon name="clipboard" :size="13" /></Button>
            <Button variant="ghost" size="sm" @click="openEdit(row)" data-testid="scripts-openedit-btn" :disabled="!canWrite" :title="canWrite ? '' : $t('common.write_denied')"><Icon name="edit" :size="13" /></Button>
            <Button variant="danger" size="sm" @click="onDelete(row)" data-testid="scripts-ondelete-btn" :disabled="!canWrite" :title="canWrite ? '' : $t('common.write_denied')"><Icon name="delete" :size="13" /></Button>
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
        <div class="btnbar btnbar--bottom">
          <Button variant="primary" size="md" native-type="submit" data-testid="scripts-onsave-btn" :disabled="!canWrite || saving" :title="canWrite ? '' : $t('common.write_denied')"><Icon name="success" :size="14" /> {{ $t('scripts.save') }}</Button>
          <Button variant="ghost" size="md" native-type="button" @click="form = null">{{ $t('scripts.cancel') }}</Button>
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
        <div class="btnbar btnbar--bottom">
          <Button variant="primary" size="md" native-type="submit" data-testid="scripts-onexecute-btn" :disabled="!canWrite || executing" :title="canWrite ? '' : $t('common.write_denied')"><Icon name="task" :size="14" /> {{ $t('scripts.execute') }}</Button>
          <Button variant="ghost" size="md" native-type="button" @click="execForm = null">{{ $t('scripts.cancel') }}</Button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 执行记录抽屉 -->
    <DetailDrawer :open="!!execRow" :title="$t('scripts.executions_title', { name: execRow?.name || '' })" @close="execRow = null">
      <div v-if="execLoading" class="muted">{{ $t('common.loading') }}</div>
      <DataTable :loading="loading" v-else :columns="execColumns" :rows="executions" row-key="id" :empty-text="$t('scripts.no_executions')">
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
  </PageShell>
</template>

<script setup>
import PageShell from '@/components/PageShell.vue'
import { useAuthStore } from '@/stores/auth'

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
import Button from "@/components/ui/Button.vue"

// 写操作门（按钮级门范式，2026-11-06 批量批）：后端该域 handler 一律 requirePermission('script:write')
// ——只读者进得来、点写按钮必 403。这里显式禁用 + 说明，不再让用户撞墙。
const auth = useAuthStore()
const canWrite = computed(() => auth.hasPerm('script:write'))

const scripts = ref([])
const loading = ref(false)
const form = ref(null)
const formError = ref('')
const loadErr = ref('')

// 执行抽屉 / 执行记录抽屉状态
const execForm = ref(null)
const execRow = ref(null)
const executions = ref([])
const execLoading = ref(false)

// 在飞态（防重复提交）：脚本执行对生产机不可幂等——慢网络下连点两次就是两次下发
// （2026-11-06 五维排查批 2 发现：此文件此前全文件零 :disabled）。保存同理（重复创建）。
const saving = ref(false)
const executing = ref(false)

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
  loadErr.value = ''
  try {
    const r = await getScripts()
    scripts.value = (r && r.scripts) || []
  } catch (e) {
    scripts.value = []
    // 此前静默吞错（只见空表）——统一骨架批次补上可见的加载错误
    loadErr.value = e.j?.error || t('common.load_failed')
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
  if (saving.value) return
  saving.value = true
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
  } finally {
    saving.value = false
  }
}

function openExec(row) {
  formError.value = ''
  execForm.value = { id: row.id, name: row.name, deviceID: '', params: '' }
}

async function onExecute() {
  if (executing.value) return
  executing.value = true
  formError.value = ''
  if (!execForm.value.deviceID) {
    formError.value = t('scripts.need_device')
    executing.value = false
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
  } finally {
    executing.value = false
  }
}

async function openExecutions(row) {
  execRow.value = row
  execLoading.value = true
  executions.value = []
  try {
    const r = await getScriptExecutions(row.id)
    executions.value = (r && r.executions) || []
  } catch (e) {
    executions.value = []
    // 此前静默吞错（只见空表）——统一骨架批次补上可见的加载错误
    loadErr.value = e.j?.error || t('common.load_failed')
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

.script-form { display: flex; flex-direction: column; gap: 12px; }
.field textarea {
  font-family: var(--font-mono, monospace);
  font-size: 12.5px; resize: vertical;
}
</style>
