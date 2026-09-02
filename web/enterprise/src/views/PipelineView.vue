<template>
  <div>
    <h2>{{ $t('pipeline.title') }}</h2>
    <p class="muted">{{ $t('pipeline.subtitle') }}</p>

    <div class="btnbar">
      <button class="primary" @click="openCreate">
        <Icon name="add" :size="14" />
        {{ $t('pipeline.add') }}
      </button>
      <button class="outline" @click="fetchAll">
        <Icon name="refresh" :size="14" />
        {{ $t('common.refresh') }}
      </button>
    </div>

    <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
    <div v-else>
      <!-- 模板列表 -->
      <DataTable :columns="tplColumns" :rows="templates" row-key="id" :empty-text="$t('pipeline.empty')">
        <template #cell-name="{ value }"><code>{{ value }}</code></template>
        <template #cell-type="{ value }">
          <span class="tag">{{ value }}</span>
        </template>
        <template #cell-createdAt="{ value }">{{ fmtTime(value) }}</template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <button class="xs outline" :title="$t('pipeline.run')" @click="onRun(row)"><Icon name="success" :size="13" /></button>
            <button class="xs outline" @click="openEdit(row)"><Icon name="edit" :size="13" /></button>
            <button class="xs danger" @click="onDelete(row)"><Icon name="delete" :size="13" /></button>
          </div>
        </template>
      </DataTable>

      <!-- 运行记录 -->
      <h3 class="section-title">{{ $t('pipeline.runs') }}</h3>
      <DataTable :columns="runColumns" :rows="runs" row-key="id" :empty-text="$t('pipeline.no_runs')">
        <template #cell-templateName="{ value }"><code>{{ value }}</code></template>
        <template #cell-status="{ value }">
          <span class="tag" :class="'st-' + value">{{ $t('pipeline.status_' + value) }}</span>
        </template>
        <template #cell-startedAt="{ value }">{{ fmtTime(value) }}</template>
        <template #cell-finishedAt="{ value }">{{ value ? fmtTime(value) : '—' }}</template>
      </DataTable>
    </div>

    <!-- 新增/编辑模板抽屉 -->
    <DetailDrawer :open="!!form" :title="form && form.id ? $t('pipeline.edit') : $t('pipeline.add')" @close="form = null">
      <form v-if="form" class="tpl-form" @submit.prevent="onSave">
        <div class="field">
          <label>{{ $t('pipeline.new_name') }}</label>
          <input v-model.trim="form.name" type="text" required />
        </div>
        <div class="field">
          <label>{{ $t('pipeline.new_type') }}</label>
          <select v-model="form.type">
            <option value="tekton">tekton</option>
            <option value="jenkins">jenkins</option>
          </select>
        </div>
        <div class="field">
          <label>{{ $t('pipeline.new_agent') }}</label>
          <input v-model.trim="form.agentID" type="text" :placeholder="$t('pipeline.agent_hint')" />
        </div>
        <div class="field">
          <label>{{ $t('pipeline.new_description') }}</label>
          <input v-model.trim="form.description" type="text" />
        </div>
        <div class="field">
          <label>{{ $t('pipeline.new_yaml') }}</label>
          <textarea v-model="form.yaml" rows="8" :placeholder="$t('pipeline.yaml_hint')"></textarea>
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('pipeline.save') }}</button>
          <button type="button" class="outline" @click="form = null">{{ $t('pipeline.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 触发运行确认（替代 confirm） -->
    <ConfirmModal
      v-model="runConfirm.show"
      data-testid="pipeline-run-confirm-modal"
      :title="$t('pipeline.run')"
      :message="$t('pipeline.confirm_run')"
      @confirm="onRunConfirm"
    />
    <!-- 删除确认（替代 confirm） -->
    <ConfirmModal
      v-model="deleteConfirm.show"
      data-testid="pipeline-delete-confirm-modal"
      :title="$t('pipeline.delete')"
      :message="$t('pipeline.confirm_delete')"
      @confirm="onDeleteConfirm"
    />
    <!-- 错误提示（替代 alert） -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="pipeline-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// 流水线管理页 — 模板 CRUD + 触发运行 + 运行记录列表
import { ref, reactive, onMounted } from 'vue'
import * as pipelineApi from '@/api/pipeline'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import { fmtTime } from '@/composables/useFormatTime'

const templates = ref([])
const runs = ref([])
const loading = ref(false)
const form = ref(null)
const formError = ref('')

// 触发运行确认弹窗
const runConfirm = reactive({ show: false, row: null })
// 删除确认弹窗
const deleteConfirm = reactive({ show: false, row: null })
// 错误提示弹窗
const errorConfirm = reactive({ show: false, message: '' })

const tplColumns = [
  { key: 'name', title: t('pipeline.name'), slot: 'cell-name' },
  { key: 'type', title: t('pipeline.type'), slot: 'cell-type', width: '90px' },
  { key: 'description', title: t('pipeline.description') },
  { key: 'agentID', title: t('pipeline.agent') },
  { key: 'createdAt', title: t('pipeline.created_at'), slot: 'cell-createdAt' },
  { key: 'actions', title: t('pipeline.actions'), slot: 'cell-actions', width: '110px' }
]

const runColumns = [
  { key: 'id', title: 'ID' },
  { key: 'templateName', title: t('pipeline.template'), slot: 'cell-templateName' },
  { key: 'status', title: t('pipeline.status'), slot: 'cell-status', width: '90px' },
  { key: 'startedAt', title: t('pipeline.started_at'), slot: 'cell-startedAt' },
  { key: 'finishedAt', title: t('pipeline.finished_at'), slot: 'cell-finishedAt' }
]

async function fetchAll() {
  loading.value = true
  try {
    const [tr, rr] = await Promise.all([pipelineApi.listTemplates(), pipelineApi.listRuns()])
    templates.value = (tr && tr.templates) || []
    runs.value = (rr && rr.runs) || []
  } catch {
    templates.value = []
    runs.value = []
  } finally {
    loading.value = false
  }
}

function openCreate() {
  formError.value = ''
  form.value = { id: null, name: '', type: 'tekton', agentID: '', description: '', yaml: '' }
}

function openEdit(row) {
  formError.value = ''
  form.value = {
    id: row.id,
    name: row.name || '',
    type: row.type || 'tekton',
    agentID: row.agentID || '',
    description: row.description || '',
    yaml: row.yaml || ''
  }
}

async function onSave() {
  formError.value = ''
  if (!form.value.name) {
    formError.value = t('pipeline.name_required')
    return
  }
  try {
    const body = {
      name: form.value.name,
      type: form.value.type,
      agentID: form.value.agentID,
      description: form.value.description,
      yaml: form.value.yaml
    }
    if (form.value.id) {
      await pipelineApi.updateTemplate(form.value.id, body)
    } else {
      await pipelineApi.createTemplate(body)
    }
    toast.success(t('pipeline.saved'))
    form.value = null
    await fetchAll()
  } catch (e) {
    formError.value = e.j?.error || t('pipeline.save_failed')
  }
}

function onRun(row) {
  runConfirm.row = row
  runConfirm.show = true
}

async function onRunConfirm() {
  const row = runConfirm.row
  if (!row) return
  try {
    await pipelineApi.runTemplate(row.id, { parameters: {} })
    toast.success(t('pipeline.run_started'))
    await fetchAll()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('pipeline.run_failed')
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
    await pipelineApi.deleteTemplate(row.id)
    await fetchAll()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('pipeline.delete_failed')
    errorConfirm.show = true
  }
}

onMounted(fetchAll)
</script>

<style scoped>
.row-actions { display: flex; gap: 4px; }
.section-title { margin: 22px 0 4px; font-size: 15px; }
.tag {
  display: inline-block; padding: 2px 8px; border-radius: 999px;
  font-size: 12px; background: var(--accent-soft); color: var(--accent);
}
.tag.st-succeeded { background: var(--ok-bg); color: var(--ok); }
.tag.st-failed { background: var(--fail-bg); color: var(--fail); }
.tag.st-running { background: var(--info-bg); color: var(--info); }
.tag.st-pending { background: var(--surface-3); color: var(--text-3); }
.tpl-form { display: flex; flex-direction: column; gap: 12px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field > label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.field textarea { font-family: var(--font-mono, monospace); font-size: 12.5px; }
.btnbar { margin-top: 8px; }
</style>
