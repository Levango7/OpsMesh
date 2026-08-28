<template>
  <div>
    <h2 data-testid="runbook-title">{{ $t('runbook.title') }}</h2>
    <p class="muted">{{ $t('runbook.desc') }}</p>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <div class="row">
      <!-- 左：Runbook 列表 -->
      <div class="col">
        <div class="card">
          <div class="flowbar">
            <h3 style="margin: 0">{{ $t('runbook.runbooks') }}</h3>
            <button class="xs primary" @click="openAddRunbook" data-testid="runbook-add-btn">{{ $t('runbook.add') }}</button>
            <button class="xs outline" @click="store.fetchRunbooks()">↻ {{ $t('common.refresh') }}</button>
          </div>
          <div v-if="store.loading && !store.runbooks.length" class="muted">{{ $t('common.loading') }}</div>
          <DataTable v-else :columns="runbookCols" :rows="store.runbooks" row-key="id" :empty-text="$t('runbook.noRunbooks')">
            <template #cell-name="{ row }">
              <b>{{ row.name }}</b><br><code>{{ row.id }}</code>
            </template>
            <template #cell-status="{ value }">
              <StatusBadge :status="runbookStatus(value)" :text="value || '-'" />
            </template>
            <template #cell-actions="{ row }">
              <div class="row-actions" @click.stop>
                <button class="xs outline" @click="store.selectRunbook(row.id)">{{ $t('runbook.executions') }}</button>
                <button class="xs primary" @click="onExecute(row.id)" data-testid="runbook-execute-btn">{{ $t('runbook.execute') }}</button>
                <button class="xs outline" style="color: var(--fail); border-color: var(--fail)" @click="onDelete(row.id)" data-testid="runbook-delete-btn">{{ $t('common.delete') }}</button>
              </div>
            </template>
          </DataTable>
        </div>
      </div>

      <!-- 右：执行历史 + 编辑器 -->
      <div class="col">
        <div class="card">
          <h3>{{ $t('runbook.executions') }} · <code v-if="store.currentRunbook">{{ store.currentRunbook.name }}</code></h3>
          <p class="hint">{{ store.currentRunbook ? store.currentRunbook.name : $t('runbook.selectRunbookHint') }}</p>
          <div v-if="store.executionsLoading && !store.executions.length" class="muted">{{ $t('common.loading') }}</div>
          <DataTable v-else :columns="executionCols" :rows="store.executions" row-key="id" :empty-text="$t('runbook.noExecutions')">
            <template #cell-status="{ value }">
              <StatusBadge :status="executionStatus(value)" :text="value || '-'" />
            </template>
            <template #cell-actions="{ row }">
              <div class="row-actions" @click.stop>
                <button class="xs outline" @click="viewLogs(row.id)" data-testid="runbook-view-logs-btn">{{ $t('runbook.viewLogs') }}</button>
              </div>
            </template>
          </DataTable>
        </div>

        <!-- 编辑器 -->
        <div class="card">
          <div class="flowbar">
            <h3 style="margin: 0">{{ $t('runbook.editor') }}</h3>
            <select v-model="editorFormat" data-testid="runbook-editor-format">
              <option value="yaml">YAML</option>
              <option value="json">JSON</option>
            </select>
          </div>
          <textarea
            v-model="editorContent"
            class="code-editor"
            rows="12"
            :placeholder="$t('runbook.editorPlaceholder')"
            data-testid="runbook-editor"
          />
          <div class="btnbar" style="margin-top: 10px;">
            <button class="primary" @click="saveEditor" data-testid="runbook-save-editor">{{ $t('runbook.save') }}</button>
            <button class="outline" @click="editorContent = ''">{{ $t('runbook.clear') }}</button>
          </div>
        </div>
      </div>
    </div>

    <!-- 添加 Runbook 对话框 -->
    <div v-if="addOpen" class="modal-mask" data-testid="runbook-add-modal" @click.self="addOpen = false">
      <div class="modal">
        <header class="modal-head">
          <h3>{{ $t('runbook.add') }}</h3>
          <button class="xs outline" @click="addOpen = false">✕</button>
        </header>
        <div class="modal-body">
          <div class="field">
            <label>{{ $t('runbook.name') }}</label>
            <input v-model="addForm.name" required data-testid="runbook-add-name" />
          </div>
          <div class="field">
            <label>{{ $t('runbook.description') }}</label>
            <input v-model="addForm.description" data-testid="runbook-add-desc" />
          </div>
          <div class="field">
            <label>{{ $t('runbook.content') }}</label>
            <textarea v-model="addForm.content" rows="8" :placeholder="$t('runbook.contentPlaceholder')" data-testid="runbook-add-content" />
          </div>
          <div class="btnbar">
            <button class="primary" @click="confirmAdd" :disabled="adding" data-testid="runbook-add-confirm">{{ $t('common.confirm') }}</button>
            <button class="outline" @click="addOpen = false">{{ $t('common.cancel') }}</button>
          </div>
          <p v-if="addMsg" :class="['msg', addOk ? 'ok' : 'err']">{{ addMsg }}</p>
        </div>
      </div>
    </div>

    <!-- 日志对话框 -->
    <div v-if="logsOpen" class="modal-mask" data-testid="runbook-logs-modal" @click.self="logsOpen = false">
      <div class="modal modal-lg">
        <header class="modal-head">
          <h3>{{ $t('runbook.executionLogs') }}</h3>
          <button class="xs outline" @click="logsOpen = false">✕</button>
        </header>
        <div class="modal-body">
          <pre class="code-block logs-block">{{ store.logsContent || '—' }}</pre>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRunbookStore } from '@/stores/runbook'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import Icon from '@/components/Icon.vue'

const store = useRunbookStore()
const editorContent = ref('')
const editorFormat = ref('yaml')

const runbookCols = computed(() => [
  { key: 'name', title: t('runbook.col.name'), slot: 'cell-name' },
  { key: 'status', title: t('runbook.col.status'), slot: 'cell-status' },
  { key: 'updatedAt', title: t('runbook.col.updatedAt') },
  { key: 'actions', title: t('runbook.col.action'), slot: 'cell-actions', width: '240px' }
])

const executionCols = computed(() => [
  { key: 'id', title: t('runbook.col.executionId') },
  { key: 'status', title: t('runbook.col.status'), slot: 'cell-status' },
  { key: 'startedAt', title: t('runbook.col.startedAt') },
  { key: 'endedAt', title: t('runbook.col.endedAt') },
  { key: 'actions', title: t('runbook.col.action'), slot: 'cell-actions', width: '100px' }
])

function runbookStatus(s) {
  if (s === 'active' || s === 'enabled') return 'success'
  if (s === 'draft') return 'info'
  if (s === 'disabled') return 'warn'
  return 'info'
}
function executionStatus(s) {
  if (s === 'success' || s === 'completed') return 'success'
  if (s === 'failed' || s === 'error') return 'failed'
  if (s === 'running') return 'warn'
  return 'info'
}

// ---- 添加 Runbook ----
const addOpen = ref(false)
const addForm = ref({ name: '', description: '', content: '' })
const addMsg = ref('')
const addOk = ref(false)
const adding = ref(false)

function openAddRunbook() {
  addOpen.value = true
  addForm.value = { name: '', description: '', content: '' }
  addMsg.value = ''
}

async function confirmAdd() {
  if (!addForm.value.name) { addMsg.value = t('runbook.nameRequired'); addOk.value = false; return }
  adding.value = true
  try {
    const r = await store.addRunbook(addForm.value.name, addForm.value.description, addForm.value.content, [])
    if (r.s >= 200 && r.s < 300) {
      addMsg.value = t('runbook.addSuccess'); addOk.value = true
      await store.fetchRunbooks()
      setTimeout(() => { addOpen.value = false }, 1200)
    } else {
      addMsg.value = r.j?.error || t('runbook.addFail'); addOk.value = false
    }
  } catch (e) {
    addMsg.value = e.j?.error || t('runbook.addFail'); addOk.value = false
  } finally {
    adding.value = false
  }
}

async function onDelete(id) {
  if (!confirm(t('runbook.deleteConfirm'))) return
  try {
    const r = await store.removeRunbook(id)
    if (r.s === 204 || (r.s >= 200 && r.s < 300)) {
      await store.fetchRunbooks()
      if (store.currentRunbookId === id) {
        store.currentRunbookId = ''
        store.executions = []
      }
    }
  } catch (e) {
    alert(e.j?.error || t('runbook.deleteFail'))
  }
}

async function onExecute(id) {
  if (!confirm(t('runbook.executeConfirm'))) return
  try {
    const r = await store.runRunbook(id)
    if (r.s >= 200 && r.s < 300) {
      alert(t('runbook.executeSuccess'))
      if (store.currentRunbookId === id) {
        await store.fetchExecutions(id)
      }
    } else {
      alert(r.j?.error || t('runbook.executeFail'))
    }
  } catch (e) {
    alert(e.j?.error || t('runbook.executeFail'))
  }
}

// ---- 日志 ----
const logsOpen = ref(false)
const logsExecutionId = ref('')

async function viewLogs(executionId) {
  logsExecutionId.value = executionId
  logsOpen.value = true
  await store.fetchLogs(store.currentRunbookId, executionId)
}

function saveEditor() {
  if (!store.currentRunbookId) {
    alert(t('runbook.selectRunbookFirst'))
    return
  }
  alert(t('runbook.saved'))
}

onMounted(() => {
  store.fetchRunbooks()
})
</script>

<style scoped>
.row .col:nth-child(1) { flex: 40; }
.row .col:nth-child(2) { flex: 60; }

.poll-err {
  padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}
.flowbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 10px; }
.row-actions { display: inline-flex; gap: 6px; }

.code-editor {
  width: 100%; font-family: var(--font-mono); font-size: 12.5px;
  background: var(--surface-3); color: var(--text); border: 1px solid var(--border);
  border-radius: var(--radius-sm); padding: 12px; resize: vertical;
}

.modal-mask {
  position: fixed; inset: 0; z-index: 50;
  background: rgba(31,37,64,.42); display: flex; align-items: center; justify-content: center;
}
.modal {
  width: 540px; max-width: 94vw; max-height: 88vh; overflow: auto;
  background: var(--surface); border-radius: var(--radius); box-shadow: var(--shadow);
  padding: 20px 22px;
}
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
