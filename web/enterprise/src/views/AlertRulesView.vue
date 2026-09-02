<template>
  <div>
    <h2 data-testid="alert-rules-title">{{ $t('alertRules.title') }}</h2>
    <p class="muted">{{ $t('alertRules.subtitle') }}</p>

    <!-- Tab 切换 -->
    <div class="tabbar">
      <button
        v-for="tab in tabs"
        :key="tab.key"
        :class="['tab', { active: activeTab === tab.key }]"
        :data-testid="'alert-rules-tab-' + tab.key"
        @click="switchTab(tab.key)"
      >{{ $t(tab.label) }}</button>
    </div>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <!-- ========== Tab 1: 告警规则 ========== -->
    <div v-show="activeTab === 'rules'">
      <div class="btnbar">
        <button class="primary" @click="openRuleCreate" data-testid="alert-rule-create-btn">
          <Icon name="add" :size="14" /> {{ $t('alertRules.add_rule') }}
        </button>
        <button class="outline" @click="store.fetchRules()"><Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}</button>
      </div>
      <div v-if="store.loading && !store.rules.length" class="muted">{{ $t('common.loading') }}</div>
      <DataTable
        v-else
        :columns="ruleColumns"
        :rows="store.rules"
        row-key="id"
        :loading="store.loading"
        :empty-text="$t('alertRules.empty_rules')"
      >
        <template #cell-threshold="{ row }">
          <code>{{ row.metric }} {{ row.op }} {{ row.threshold }}</code>
        </template>
        <template #cell-enabled="{ value }">
          <span class="status-pill" :class="value ? 'ok' : 'off'">{{ value ? $t('alertRules.enabled') : $t('alertRules.disabled') }}</span>
        </template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <button class="xs outline" @click="openRuleEdit(row)" data-testid="alert-rule-edit-btn"><Icon name="edit" :size="13" /></button>
            <button class="xs danger" @click="onRuleDelete(row)" data-testid="alert-rule-delete-btn"><Icon name="delete" :size="13" /></button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- ========== Tab 2: 引擎规则 ========== -->
    <div v-show="activeTab === 'engine'">
      <div class="btnbar">
        <button class="primary" @click="openEngineCreate" data-testid="alert-engine-create-btn">
          <Icon name="add" :size="14" /> {{ $t('alertRules.add_engine') }}
        </button>
        <button class="outline" @click="store.fetchEngineRules()"><Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}</button>
      </div>
      <div v-if="store.loading && !store.engineRules.length" class="muted">{{ $t('common.loading') }}</div>
      <DataTable
        v-else
        :columns="engineColumns"
        :rows="store.engineRules"
        row-key="id"
        :loading="store.loading"
        :empty-text="$t('alertRules.empty_engine')"
      >
        <template #cell-conditions="{ value }">
          <code>{{ formatConditions(value) }}</code>
        </template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <button class="xs outline" @click="openEngineEdit(row)" data-testid="alert-engine-edit-btn"><Icon name="edit" :size="13" /></button>
            <button class="xs danger" @click="onEngineDelete(row)" data-testid="alert-engine-delete-btn"><Icon name="delete" :size="13" /></button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- ========== Tab 3: 静默规则 ========== -->
    <div v-show="activeTab === 'silences'">
      <div class="btnbar">
        <button class="primary" @click="openSilenceCreate" data-testid="alert-silence-create-btn">
          <Icon name="add" :size="14" /> {{ $t('alertRules.add_silence') }}
        </button>
        <button class="outline" @click="store.fetchSilences()"><Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}</button>
      </div>
      <div v-if="store.loading && !store.silences.length" class="muted">{{ $t('common.loading') }}</div>
      <DataTable
        v-else
        :columns="silenceColumns"
        :rows="store.silences"
        row-key="id"
        :loading="store.loading"
        :empty-text="$t('alertRules.empty_silences')"
      >
        <template #cell-matchers="{ value }"><code>{{ formatMatchers(value) }}</code></template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <button class="xs danger" @click="onSilenceDelete(row)" data-testid="alert-silence-delete-btn"><Icon name="delete" :size="13" /></button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- 告警规则新增/编辑抽屉 -->
    <DetailDrawer :open="!!ruleForm" :title="ruleForm && ruleForm.id ? $t('alertRules.edit_rule') : $t('alertRules.add_rule')" @close="ruleForm = null">
      <form v-if="ruleForm" class="entity-form" @submit.prevent="onRuleSave">
        <div class="field">
          <label>{{ $t('alertRules.field_name') }}</label>
          <input v-model.trim="ruleForm.name" type="text" required />
        </div>
        <div class="field">
          <label>{{ $t('alertRules.field_metric') }}</label>
          <input v-model.trim="ruleForm.metric" type="text" placeholder="cpu_usage" required />
        </div>
        <div class="row-2">
          <div class="field">
            <label>{{ $t('alertRules.field_op') }}</label>
            <select v-model="ruleForm.op">
              <option value=">">&gt;</option>
              <option value="<">&lt;</option>
              <option value=">=">&ge;</option>
              <option value="<=">&le;</option>
              <option value="==">==</option>
            </select>
          </div>
          <div class="field">
            <label>{{ $t('alertRules.field_threshold') }}</label>
            <input v-model.number="ruleForm.threshold" type="number" step="0.01" required />
          </div>
        </div>
        <div class="field">
          <label>{{ $t('alertRules.field_duration') }}</label>
          <input v-model.trim="ruleForm.duration" type="text" placeholder="5m" />
        </div>
        <div class="field">
          <label>{{ $t('alertRules.field_severity') }}</label>
          <select v-model="ruleForm.severity">
            <option value="warning">warning</option>
            <option value="critical">critical</option>
          </select>
        </div>
        <div class="field">
          <label>
            <input type="checkbox" v-model="ruleForm.enabled" />
            {{ $t('alertRules.field_enabled') }}
          </label>
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('common.save') }}</button>
          <button type="button" class="outline" @click="ruleForm = null">{{ $t('common.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 引擎规则新增/编辑抽屉 -->
    <DetailDrawer :open="!!engineForm" :title="engineForm && engineForm.id ? $t('alertRules.edit_engine') : $t('alertRules.add_engine')" @close="engineForm = null">
      <form v-if="engineForm" class="entity-form" @submit.prevent="onEngineSave">
        <div class="field">
          <label>{{ $t('alertRules.field_name') }}</label>
          <input v-model.trim="engineForm.name" type="text" required />
        </div>
        <div class="field">
          <label>{{ $t('alertRules.field_conditions') }}</label>
          <textarea v-model="engineForm.conditionsRaw" rows="4" :placeholder="$t('alertRules.conditions_placeholder')" required></textarea>
        </div>
        <div class="field">
          <label>{{ $t('alertRules.field_action') }}</label>
          <select v-model="engineForm.action">
            <option value="alert">alert</option>
            <option value="webhook">webhook</option>
            <option value="silence">silence</option>
          </select>
        </div>
        <div class="field">
          <label>{{ $t('alertRules.field_severity') }}</label>
          <select v-model="engineForm.severity">
            <option value="warning">warning</option>
            <option value="critical">critical</option>
          </select>
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('common.save') }}</button>
          <button type="button" class="outline" @click="engineForm = null">{{ $t('common.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 静默规则新增抽屉 -->
    <DetailDrawer :open="!!silenceForm" :title="$t('alertRules.add_silence')" @close="silenceForm = null">
      <form v-if="silenceForm" class="entity-form" @submit.prevent="onSilenceSave">
        <div class="field">
          <label>{{ $t('alertRules.field_matchers') }}</label>
          <textarea v-model="silenceForm.matchersRaw" rows="3" :placeholder="$t('alertRules.matchers_placeholder')" required></textarea>
        </div>
        <div class="row-2">
          <div class="field">
            <label>{{ $t('alertRules.field_starts_at') }}</label>
            <input v-model.trim="silenceForm.startsAt" type="datetime-local" required />
          </div>
          <div class="field">
            <label>{{ $t('alertRules.field_ends_at') }}</label>
            <input v-model.trim="silenceForm.endsAt" type="datetime-local" required />
          </div>
        </div>
        <div class="field">
          <label>{{ $t('alertRules.field_comment') }}</label>
          <input v-model.trim="silenceForm.comment" type="text" />
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('common.save') }}</button>
          <button type="button" class="outline" @click="silenceForm = null">{{ $t('common.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 删除确认 -->
    <ConfirmModal
      v-model="deleteConfirm.show"
      data-testid="alert-rules-delete-confirm-modal"
      :title="$t('common.delete')"
      :message="$t('alertRules.confirm_delete')"
      @confirm="onDeleteConfirm"
    />
    <!-- 错误提示 -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="alert-rules-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// 告警规则管理页 — 三 Tab：规则 / 引擎 / 静默
import { ref, computed, reactive, onMounted } from 'vue'
import { useAlertRuleStore } from '@/stores/alert-rule'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'

const store = useAlertRuleStore()
const activeTab = ref('rules')
const formError = ref('')

const tabs = [
  { key: 'rules', label: 'alertRules.tab_rules' },
  { key: 'engine', label: 'alertRules.tab_engine' },
  { key: 'silences', label: 'alertRules.tab_silences' }
]

const ruleColumns = computed(() => [
  { key: 'name', title: t('alertRules.field_name') },
  { key: 'metric', title: t('alertRules.field_metric') },
  { key: 'threshold', title: t('alertRules.field_threshold'), slot: 'cell-threshold' },
  { key: 'severity', title: t('alertRules.field_severity') },
  { key: 'enabled', title: t('alertRules.field_enabled'), slot: 'cell-enabled' },
  { key: 'actions', title: t('alertRules.col_actions'), slot: 'cell-actions', width: '90px' }
])

const engineColumns = computed(() => [
  { key: 'name', title: t('alertRules.field_name') },
  { key: 'conditions', title: t('alertRules.field_conditions'), slot: 'cell-conditions' },
  { key: 'action', title: t('alertRules.field_action') },
  { key: 'severity', title: t('alertRules.field_severity') },
  { key: 'actions', title: t('alertRules.col_actions'), slot: 'cell-actions', width: '90px' }
])

const silenceColumns = computed(() => [
  { key: 'matchers', title: t('alertRules.field_matchers'), slot: 'cell-matchers' },
  { key: 'startsAt', title: t('alertRules.field_starts_at') },
  { key: 'endsAt', title: t('alertRules.field_ends_at') },
  { key: 'comment', title: t('alertRules.field_comment') },
  { key: 'actions', title: t('alertRules.col_actions'), slot: 'cell-actions', width: '70px' }
])

// 删除确认 / 错误提示
const deleteConfirm = reactive({ show: false, type: '', id: null })
const errorConfirm = reactive({ show: false, message: '' })

// ---------- Tab 切换 ----------
function switchTab(key) {
  activeTab.value = key
  if (key === 'rules' && !store.rules.length) store.fetchRules()
  else if (key === 'engine' && !store.engineRules.length) store.fetchEngineRules()
  else if (key === 'silences' && !store.silences.length) store.fetchSilences()
}

// ---------- 告警规则表单 ----------
const ruleForm = ref(null)

function openRuleCreate() {
  formError.value = ''
  ruleForm.value = { id: null, name: '', metric: '', op: '>', threshold: 0, duration: '5m', severity: 'warning', enabled: true }
}

function openRuleEdit(row) {
  formError.value = ''
  ruleForm.value = {
    id: row.id,
    name: row.name || '',
    metric: row.metric || '',
    op: row.op || '>',
    threshold: row.threshold != null ? row.threshold : 0,
    duration: row.duration || '5m',
    severity: row.severity || 'warning',
    enabled: !!row.enabled
  }
}

async function onRuleSave() {
  formError.value = ''
  try {
    const body = {
      name: ruleForm.value.name,
      metric: ruleForm.value.metric,
      op: ruleForm.value.op,
      threshold: ruleForm.value.threshold,
      duration: ruleForm.value.duration,
      severity: ruleForm.value.severity,
      enabled: ruleForm.value.enabled
    }
    if (ruleForm.value.id) await store.updateRule(ruleForm.value.id, body)
    else await store.createRule(body)
    ruleForm.value = null
    toast.success(t('alertRules.saved'))
  } catch (e) {
    formError.value = e.j?.error || t('alertRules.save_failed')
  }
}

function onRuleDelete(row) {
  deleteConfirm.type = 'rule'
  deleteConfirm.id = row.id
  deleteConfirm.show = true
}

// ---------- 引擎规则表单 ----------
const engineForm = ref(null)

function openEngineCreate() {
  formError.value = ''
  engineForm.value = { id: null, name: '', conditionsRaw: '', action: 'alert', severity: 'warning' }
}

function openEngineEdit(row) {
  formError.value = ''
  engineForm.value = {
    id: row.id,
    name: row.name || '',
    conditionsRaw: typeof row.conditions === 'string' ? row.conditions : JSON.stringify(row.conditions || [], null, 2),
    action: row.action || 'alert',
    severity: row.severity || 'warning'
  }
}

async function onEngineSave() {
  formError.value = ''
  let conditions
  try {
    conditions = JSON.parse(engineForm.value.conditionsRaw)
  } catch {
    conditions = engineForm.value.conditionsRaw
  }
  try {
    const body = {
      name: engineForm.value.name,
      conditions,
      action: engineForm.value.action,
      severity: engineForm.value.severity
    }
    if (engineForm.value.id) await store.updateEngineRule(engineForm.value.id, body)
    else await store.createEngineRule(body)
    engineForm.value = null
    toast.success(t('alertRules.saved'))
  } catch (e) {
    formError.value = e.j?.error || t('alertRules.save_failed')
  }
}

function onEngineDelete(row) {
  deleteConfirm.type = 'engine'
  deleteConfirm.id = row.id
  deleteConfirm.show = true
}

// ---------- 静默规则表单 ----------
const silenceForm = ref(null)

function openSilenceCreate() {
  formError.value = ''
  silenceForm.value = { matchersRaw: '', startsAt: '', endsAt: '', comment: '' }
}

async function onSilenceSave() {
  formError.value = ''
  let matchers
  try {
    matchers = JSON.parse(silenceForm.value.matchersRaw)
  } catch {
    matchers = silenceForm.value.matchersRaw
  }
  try {
    const body = {
      matchers,
      startsAt: silenceForm.value.startsAt,
      endsAt: silenceForm.value.endsAt,
      comment: silenceForm.value.comment
    }
    await store.createSilence(body)
    silenceForm.value = null
    toast.success(t('alertRules.saved'))
  } catch (e) {
    formError.value = e.j?.error || t('alertRules.save_failed')
  }
}

function onSilenceDelete(row) {
  deleteConfirm.type = 'silence'
  deleteConfirm.id = row.id
  deleteConfirm.show = true
}

// ---------- 删除确认 ----------
async function onDeleteConfirm() {
  const { type, id } = deleteConfirm
  if (!id) return
  try {
    if (type === 'rule') await store.removeRule(id)
    else if (type === 'engine') await store.removeEngineRule(id)
    else if (type === 'silence') await store.removeSilence(id)
    toast.success(t('alertRules.deleted'))
  } catch (e) {
    errorConfirm.message = e.j?.error || t('alertRules.delete_failed')
    errorConfirm.show = true
  }
}

// ---------- 格式化辅助 ----------
function formatConditions(v) {
  if (!v) return '—'
  if (typeof v === 'string') return v
  try { return JSON.stringify(v) } catch { return String(v) }
}

function formatMatchers(v) {
  if (!v) return '—'
  if (typeof v === 'string') return v
  try { return JSON.stringify(v) } catch { return String(v) }
}

onMounted(() => { store.fetchRules() })
</script>

<style scoped>
.tabbar {
  display: flex; gap: 4px; margin-bottom: 16px;
  border-bottom: 1px solid var(--border);
}
.tab {
  background: transparent; border: none; border-bottom: 2px solid transparent;
  padding: 8px 16px; cursor: pointer; color: var(--text-2);
  font-size: 13px; font-weight: 500;
}
.tab.active { color: var(--accent); border-bottom-color: var(--accent); }
.tab:hover { color: var(--text); }

.btnbar { display: flex; gap: 8px; margin-bottom: 12px; }
.row-actions { display: flex; gap: 4px; }
.status-pill {
  display: inline-flex; align-items: center;
  padding: 1px 9px; border-radius: 999px; font-size: 12px; font-weight: 600;
}
.status-pill.ok { background: var(--accent-soft); color: var(--accent); }
.status-pill.off { background: var(--surface-3); color: var(--text-3); }

.poll-err {
  padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}

.entity-form { display: flex; flex-direction: column; gap: 12px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field > label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.row-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.btnbar { margin-top: 8px; }

@media (max-width: 600px) { .row-2 { grid-template-columns: 1fr; } }
</style>