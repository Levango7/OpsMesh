<template>
  <div>
    <h2>{{ $t('automation.title') }}</h2>
    <p class="muted">{{ $t('automation.subtitle') }}</p>

    <div class="btnbar">
      <button class="primary" @click="openCreate">
        <Icon name="add" :size="14" />
        {{ $t('automation.add') }}
      </button>
      <button class="outline" @click="refreshAll">
        <Icon name="refresh" :size="14" />
        {{ $t('common.refresh') }}
      </button>
    </div>

    <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
    <div v-else>
      <DataTable :columns="columns" :rows="rules" row-key="id" :empty-text="$t('automation.empty')">
        <template #cell-triggerType="{ value }"><code>{{ value }}</code></template>
        <template #cell-actions-list="{ row }">
          <span class="muted">{{ (row.actions || []).map((a) => a.type).join(', ') || '—' }}</span>
        </template>
        <template #cell-enabled="{ row }">
          <span class="status-pill" :class="row.enabled ? 'ok' : 'off'">{{ row.enabled ? $t('automation.enabled') : $t('automation.disabled') }}</span>
        </template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <button class="xs outline" :title="$t('automation.test')" @click="onTest(row)"><Icon name="check" :size="13" /></button>
            <button
              class="xs outline"
              :title="row.enabled ? $t('automation.disable') : $t('automation.enable')"
              @click="onToggle(row)"
            ><Icon :name="row.enabled ? 'mute' : 'success'" :size="13" /></button>
            <button class="xs outline" @click="openEdit(row)"><Icon name="edit" :size="13" /></button>
            <button class="xs danger" @click="onDelete(row)"><Icon name="delete" :size="13" /></button>
          </div>
        </template>
      </DataTable>

      <!-- 执行历史 -->
      <h4 class="section-title">{{ $t('automation.executions') }}</h4>
      <DataTable :columns="execColumns" :rows="executions" row-key="id" :empty-text="$t('automation.no_executions')">
        <template #cell-status="{ value }">
          <span class="status-pill" :class="value === 'succeeded' ? 'ok' : (value === 'failed' ? 'bad' : 'warn')">{{ value }}</span>
        </template>
        <template #cell-startedAt="{ value }">{{ fmtTime(value) }}</template>
        <template #cell-endedAt="{ value }">{{ fmtTime(value) }}</template>
      </DataTable>
    </div>

    <!-- 新增/编辑抽屉 -->
    <DetailDrawer :open="!!form" :title="form && form.id ? $t('automation.edit') : $t('automation.add')" @close="form = null">
      <form v-if="form" class="rule-form" @submit.prevent="onSave">
        <div class="field">
          <label>{{ $t('automation.new_name') }}</label>
          <input v-model.trim="form.name" type="text" required />
        </div>
        <div class="field">
          <label>{{ $t('automation.new_description') }}</label>
          <input v-model.trim="form.description" type="text" />
        </div>
        <div class="field">
          <label>{{ $t('automation.new_trigger_type') }}</label>
          <select v-model="form.triggerType">
            <option value="metric">{{ $t('automation.trigger_metric') }}</option>
            <option value="alert">{{ $t('automation.trigger_alert') }}</option>
            <option value="schedule">{{ $t('automation.trigger_schedule') }}</option>
            <option value="event">{{ $t('automation.trigger_event') }}</option>
          </select>
        </div>
        <div class="field">
          <label>{{ $t('automation.new_trigger_params') }}</label>
          <textarea v-model="form.triggerParamsText" rows="3" :placeholder="$t('automation.trigger_params_placeholder')"></textarea>
        </div>
        <div class="field">
          <label>{{ $t('automation.new_action_type') }}</label>
          <select v-model="form.actionType">
            <option value="execute_task">{{ $t('automation.action_execute_task') }}</option>
            <option value="send_notify">{{ $t('automation.action_send_notify') }}</option>
            <option value="scale">{{ $t('automation.action_scale') }}</option>
            <option value="restart">{{ $t('automation.action_restart') }}</option>
            <option value="isolate">{{ $t('automation.action_isolate') }}</option>
          </select>
        </div>
        <div class="field">
          <label>{{ $t('automation.new_action_params') }}</label>
          <textarea v-model="form.actionParamsText" rows="3" :placeholder="$t('automation.action_params_placeholder')"></textarea>
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('automation.save') }}</button>
          <button type="button" class="outline" @click="form = null">{{ $t('automation.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 删除确认（替代 confirm） -->
    <ConfirmModal
      v-model="deleteConfirm.show"
      data-testid="automation-delete-confirm-modal"
      :title="$t('automation.delete')"
      :message="$t('automation.confirm_delete')"
      @confirm="onDeleteConfirm"
    />
    <!-- 错误提示（替代 alert） -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="automation-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// 自动化规则管理页 — 规则 CRUD + 启停/测试 + 执行历史
import { ref, computed, reactive, onMounted } from 'vue'
import {
  getAutomationRules, createAutomationRule, updateAutomationRule, deleteAutomationRule,
  enableAutomationRule, disableAutomationRule, testAutomationRule, getAutomationExecutions
} from '@/api/automation'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import { fmtTime } from '@/composables/useFormatTime'

const rules = ref([])
const executions = ref([])
const loading = ref(false)
const form = ref(null)
const formError = ref('')

// 删除确认弹窗（替代 confirm）
const deleteConfirm = reactive({ show: false, row: null })
// 错误提示弹窗（替代 alert）
const errorConfirm = reactive({ show: false, message: '' })

const columns = computed(() => [
  { key: 'name', title: t('automation.name') },
  { key: 'triggerType', title: t('automation.trigger_type'), slot: 'cell-triggerType' },
  { key: 'actions-list', title: t('automation.action_types'), slot: 'cell-actions-list' },
  { key: 'enabled', title: t('automation.status'), slot: 'cell-enabled' },
  { key: 'actions', title: t('automation.actions'), slot: 'cell-actions', width: '120px' }
])

const execColumns = computed(() => [
  { key: 'ruleName', title: t('automation.rule_name') },
  { key: 'status', title: t('automation.exec_status'), slot: 'cell-status' },
  { key: 'detail', title: t('automation.exec_detail') },
  { key: 'startedAt', title: t('automation.started_at'), slot: 'cell-startedAt' },
  { key: 'endedAt', title: t('automation.ended_at'), slot: 'cell-endedAt' }
])

// triggerParams / action.params 编辑态用 JSON 文本，保存时解析
function parseParamsText(text, errKey) {
  if (!text || !text.trim()) return {}
  try {
    return JSON.parse(text)
  } catch {
    throw new Error(t(errKey))
  }
}

async function fetchRules() {
  loading.value = true
  try {
    const r = await getAutomationRules()
    rules.value = (r && r.rules) || []
  } catch {
    rules.value = []
  } finally {
    loading.value = false
  }
}

async function fetchExecutions() {
  try {
    const r = await getAutomationExecutions()
    executions.value = (r && r.executions) || []
  } catch {
    executions.value = []
  }
}

function refreshAll() {
  fetchRules()
  fetchExecutions()
}

function openCreate() {
  formError.value = ''
  form.value = {
    id: null, name: '', description: '',
    triggerType: 'metric', triggerParamsText: '',
    actionType: 'execute_task', actionParamsText: ''
  }
}

function openEdit(row) {
  formError.value = ''
  form.value = {
    id: row.id,
    name: row.name || '',
    description: row.description || '',
    triggerType: row.triggerType || 'metric',
    triggerParamsText: JSON.stringify(row.triggerParams || {}, null, 2),
    actionType: (row.actions && row.actions[0] && row.actions[0].type) || 'execute_task',
    actionParamsText: JSON.stringify((row.actions && row.actions[0] && row.actions[0].params) || {}, null, 2)
  }
}

async function onSave() {
  formError.value = ''
  try {
    const triggerParams = parseParamsText(form.value.triggerParamsText, 'automation.params_invalid')
    const actionParams = parseParamsText(form.value.actionParamsText, 'automation.params_invalid')
    const body = {
      name: form.value.name,
      description: form.value.description,
      triggerType: form.value.triggerType,
      triggerParams,
      actions: [{ type: form.value.actionType, params: actionParams }]
    }
    if (form.value.id) await updateAutomationRule(form.value.id, body)
    else await createAutomationRule(body)
    form.value = null
    toast.success(t('automation.saved'))
    await fetchRules()
  } catch (e) {
    formError.value = e.j?.error || (e instanceof Error ? e.message : '') || t('automation.save_failed')
  }
}

async function onToggle(row) {
  try {
    if (row.enabled) await disableAutomationRule(row.id)
    else await enableAutomationRule(row.id)
    toast.success(row.enabled ? t('automation.disabled') : t('automation.enabled'))
    await fetchRules()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('automation.toggle_failed')
    errorConfirm.show = true
  }
}

async function onTest(row) {
  try {
    const { j } = await testAutomationRule(row.id)
    toast.success(t('automation.test_done') + (j?.status ? ` (${j.status})` : ''))
    await fetchExecutions()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('automation.test_failed')
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
    await deleteAutomationRule(row.id)
    toast.success(t('automation.deleted'))
    await fetchRules()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('automation.delete_failed')
    errorConfirm.show = true
  }
}

onMounted(refreshAll)
</script>

<style scoped>
.row-actions { display: flex; gap: 4px; }
.section-title { margin: 20px 0 4px; font-size: 14px; }
.status-pill {
  display: inline-flex; align-items: center;
  padding: 1px 9px; border-radius: 999px; font-size: 12px; font-weight: 600;
}
.status-pill.ok { background: var(--accent-soft); color: var(--accent); }
.status-pill.off { background: var(--surface-3); color: var(--text-3); }
.status-pill.bad { background: var(--fail-bg); color: var(--fail); }
.status-pill.warn { background: var(--warn-bg); color: var(--warn); }
.rule-form { display: flex; flex-direction: column; gap: 12px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field > label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.field textarea, .field select { resize: vertical; }
.btnbar { margin-top: 8px; }
</style>
