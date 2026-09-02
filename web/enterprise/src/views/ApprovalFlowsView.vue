<template>
  <div>
    <h2 data-testid="approval-flows-title">{{ $t('approval.flows_title') }}</h2>
    <p class="muted">{{ $t('approval.flows_subtitle') }}</p>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <div class="btnbar">
      <button class="primary" @click="openCreate" data-testid="approval-flow-create-btn">
        <Icon name="add" :size="14" /> {{ $t('approval.add_flow') }}
      </button>
      <button class="outline" @click="store.fetchFlows()"><Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}</button>
    </div>

    <div v-if="store.loading && !store.flows.length" class="muted">{{ $t('common.loading') }}</div>
    <DataTable
      v-else
      :columns="columns"
      :rows="store.flows"
      row-key="id"
      :loading="store.loading"
      :empty-text="$t('approval.empty_flows')"
    >
      <template #cell-steps="{ value }">
        <code>{{ formatSteps(value) }}</code>
      </template>
      <template #cell-actions="{ row }">
        <div class="row-actions">
          <button class="xs outline" @click="openEdit(row)" data-testid="approval-flow-edit-btn"><Icon name="edit" :size="13" /></button>
          <button class="xs danger" @click="onDelete(row)" data-testid="approval-flow-delete-btn"><Icon name="delete" :size="13" /></button>
        </div>
      </template>
    </DataTable>

    <!-- 新增/编辑抽屉 -->
    <DetailDrawer :open="!!form" :title="form && form.id ? $t('approval.edit_flow') : $t('approval.add_flow')" @close="form = null">
      <form v-if="form" class="entity-form" @submit.prevent="onSave">
        <div class="field">
          <label>{{ $t('approval.field_name') }}</label>
          <input v-model.trim="form.name" type="text" required />
        </div>
        <div class="field">
          <label>{{ $t('approval.field_description') }}</label>
          <input v-model.trim="form.description" type="text" />
        </div>
        <div class="field">
          <label>{{ $t('approval.field_steps') }}</label>
          <textarea v-model="form.stepsRaw" rows="6" :placeholder="$t('approval.steps_placeholder')" required></textarea>
          <small class="hint">{{ $t('approval.steps_hint') }}</small>
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('common.save') }}</button>
          <button type="button" class="outline" @click="form = null">{{ $t('common.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 删除确认 -->
    <ConfirmModal
      v-model="deleteConfirm.show"
      data-testid="approval-flow-delete-confirm-modal"
      :title="$t('common.delete')"
      :message="$t('approval.confirm_delete_flow')"
      @confirm="onDeleteConfirm"
    />
    <!-- 错误提示 -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="approval-flow-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// 审批流定义页 — 列表 + 创建/编辑（多级审批节点配置）+ 删除
import { ref, computed, reactive, onMounted } from 'vue'
import { useApprovalStore } from '@/stores/approval'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'

const store = useApprovalStore()
const formError = ref('')
const deleteConfirm = reactive({ show: false, id: null })
const errorConfirm = reactive({ show: false, message: '' })

const columns = computed(() => [
  { key: 'name', title: t('approval.field_name') },
  { key: 'description', title: t('approval.field_description') },
  { key: 'steps', title: t('approval.field_steps'), slot: 'cell-steps' },
  { key: 'createdAt', title: t('approval.field_createdAt') },
  { key: 'actions', title: t('approval.col_actions'), slot: 'cell-actions', width: '90px' }
])

const form = ref(null)

function openCreate() {
  formError.value = ''
  form.value = { id: null, name: '', description: '', stepsRaw: '' }
}

function openEdit(row) {
  formError.value = ''
  form.value = {
    id: row.id,
    name: row.name || '',
    description: row.description || '',
    stepsRaw: formatSteps(row.steps)
  }
}

function formatSteps(v) {
  if (!v) return '[]'
  if (typeof v === 'string') return v
  try { return JSON.stringify(v, null, 2) } catch { return String(v) }
}

async function onSave() {
  formError.value = ''
  let steps
  try {
    steps = JSON.parse(form.value.stepsRaw)
  } catch {
    formError.value = t('approval.steps_invalid')
    return
  }
  try {
    const body = {
      name: form.value.name,
      description: form.value.description,
      steps
    }
    if (form.value.id) await store.updateFlow(form.value.id, body)
    else await store.createFlow(body)
    form.value = null
    toast.success(t('approval.flow_saved'))
  } catch (e) {
    formError.value = e.j?.error || t('approval.flow_save_failed')
  }
}

function onDelete(row) {
  deleteConfirm.id = row.id
  deleteConfirm.show = true
}

async function onDeleteConfirm() {
  const { id } = deleteConfirm
  if (!id) return
  try {
    await store.removeFlow(id)
    toast.success(t('approval.flow_deleted'))
  } catch (e) {
    errorConfirm.message = e.j?.error || t('approval.flow_delete_failed')
    errorConfirm.show = true
  }
}

onMounted(() => {
  store.fetchFlows().catch((e) => {
    errorConfirm.message = e.j?.error || t('error.approvalFlowsFailed')
    errorConfirm.show = true
  })
})
</script>

<style scoped>
.btnbar { display: flex; gap: 8px; margin-bottom: 12px; }
.row-actions { display: flex; gap: 4px; }

.poll-err {
  padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}

.entity-form { display: flex; flex-direction: column; gap: 12px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field > label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.field .hint { font-size: 11.5px; color: var(--text-3); }
.btnbar { margin-top: 8px; }
</style>