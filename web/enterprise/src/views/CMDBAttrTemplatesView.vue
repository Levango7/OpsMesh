<template>
  <div>
    <h2 data-testid="cmdb-attr-templates-title">{{ $t('cmdbAttrTemplates.title') }}</h2>
    <p class="muted">{{ $t('cmdbAttrTemplates.subtitle') }}</p>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <div class="btnbar">
      <button class="primary" @click="openCreate" data-testid="cmdb-attr-template-create-btn">
        <Icon name="add" :size="14" /> {{ $t('cmdbAttrTemplates.add') }}
      </button>
      <button class="outline" @click="store.fetchTemplates()"><Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}</button>
    </div>

    <!-- 属性模板列表 -->
    <div v-if="store.loading && !store.templates.length" class="muted">{{ $t('common.loading') }}</div>
    <DataTable
      v-else
      :columns="columns"
      :rows="store.templates"
      row-key="id"
      :loading="store.loading"
      :empty-text="$t('cmdbAttrTemplates.empty')"
    >
      <template #cell-id="{ value }"><code>{{ value }}</code></template>
      <template #cell-required="{ value }">
        <span class="status-pill" :class="value ? 'ok' : 'off'">{{ value ? $t('common.yes') : $t('common.no') }}</span>
      </template>
      <template #cell-options="{ value }">
        <code class="options-cell">{{ formatOptions(value) }}</code>
      </template>
      <template #cell-actions="{ row }">
        <div class="row-actions">
          <button class="xs outline" @click="openEdit(row)" data-testid="cmdb-attr-template-edit-btn"><Icon name="edit" :size="13" /></button>
          <button class="xs danger" @click="onDelete(row)" data-testid="cmdb-attr-template-delete-btn"><Icon name="delete" :size="13" /></button>
        </div>
      </template>
    </DataTable>

    <!-- 创建/编辑抽屉 -->
    <DetailDrawer :open="!!form" :title="form && form.id ? $t('cmdbAttrTemplates.edit') : $t('cmdbAttrTemplates.add')" @close="form = null">
      <form v-if="form" class="entity-form" @submit.prevent="onSave">
        <div class="field">
          <label>{{ $t('cmdbAttrTemplates.field_name') }}</label>
          <input v-model.trim="form.name" type="text" required />
        </div>
        <div class="row-2">
          <div class="field">
            <label>{{ $t('cmdbAttrTemplates.field_type') }}</label>
            <select v-model="form.type">
              <option value="string">string</option>
              <option value="number">number</option>
              <option value="boolean">boolean</option>
              <option value="enum">enum</option>
              <option value="json">json</option>
            </select>
          </div>
          <div class="field">
            <label>{{ $t('cmdbAttrTemplates.field_category') }}</label>
            <input v-model.trim="form.category" type="text" />
          </div>
        </div>
        <div class="field">
          <label>
            <input type="checkbox" v-model="form.required" />
            {{ $t('cmdbAttrTemplates.field_required') }}
          </label>
        </div>
        <div class="field">
          <label>{{ $t('cmdbAttrTemplates.field_defaultValue') }}</label>
          <input v-model.trim="form.defaultValue" type="text" />
        </div>
        <div class="field">
          <label>{{ $t('cmdbAttrTemplates.field_options') }}</label>
          <textarea v-model="form.optionsRaw" rows="3" :placeholder="$t('cmdbAttrTemplates.options_placeholder')"></textarea>
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('common.save') }}</button>
          <button type="button" class="outline" @click="form = null">{{ $t('common.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <ConfirmModal
      v-model="deleteConfirm.show"
      data-testid="cmdb-attr-template-delete-confirm"
      :title="$t('common.delete')"
      :message="$t('cmdbAttrTemplates.confirm_delete')"
      @confirm="onDeleteConfirm"
    />
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="cmdb-attr-template-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// CMDB 属性模板页 — CRUD（列表 + 创建/编辑 modal + 删除）
import { ref, reactive, computed, onMounted } from 'vue'
import { useCMDBAdvancedStore } from '@/stores/cmdb-advanced'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'

const store = useCMDBAdvancedStore()
const formError = ref('')
const deleteConfirm = reactive({ show: false, id: null })
const errorConfirm = reactive({ show: false, message: '' })

const columns = computed(() => [
  { key: 'id', title: t('cmdbAttrTemplates.col_id'), slot: 'cell-id' },
  { key: 'name', title: t('cmdbAttrTemplates.field_name') },
  { key: 'type', title: t('cmdbAttrTemplates.field_type') },
  { key: 'category', title: t('cmdbAttrTemplates.field_category') },
  { key: 'required', title: t('cmdbAttrTemplates.field_required'), slot: 'cell-required' },
  { key: 'defaultValue', title: t('cmdbAttrTemplates.field_defaultValue') },
  { key: 'options', title: t('cmdbAttrTemplates.field_options'), slot: 'cell-options' },
  { key: 'createdAt', title: t('cmdbAttrTemplates.field_createdAt') },
  { key: 'actions', title: t('cmdbAttrTemplates.col_actions'), slot: 'cell-actions', width: '90px' }
])

function formatOptions(opts) {
  if (!opts) return '—'
  try { return JSON.stringify(opts) } catch { return String(opts) }
}

// ---------- 创建/编辑 ----------
const form = ref(null)

function openCreate() {
  formError.value = ''
  form.value = { id: null, name: '', type: 'string', category: '', required: false, defaultValue: '', optionsRaw: '' }
}

function openEdit(row) {
  formError.value = ''
  form.value = {
    id: row.id,
    name: row.name || '',
    type: row.type || 'string',
    category: row.category || '',
    required: !!row.required,
    defaultValue: row.defaultValue != null ? String(row.defaultValue) : '',
    optionsRaw: row.options ? (typeof row.options === 'string' ? row.options : JSON.stringify(row.options, null, 2)) : ''
  }
}

async function onSave() {
  formError.value = ''
  let options
  try {
    options = form.value.optionsRaw ? JSON.parse(form.value.optionsRaw) : null
  } catch {
    options = form.value.optionsRaw ? form.value.optionsRaw.split(/[\s,]+/).filter(Boolean) : null
  }
  const body = {
    name: form.value.name,
    type: form.value.type,
    category: form.value.category,
    required: form.value.required,
    defaultValue: form.value.defaultValue,
    options
  }
  try {
    if (form.value.id) await store.updateTemplate(form.value.id, body)
    else await store.createTemplate(body)
    form.value = null
    toast.success(t('cmdbAttrTemplates.saved'))
  } catch (e) {
    formError.value = e.j?.error || t('cmdbAttrTemplates.save_failed')
  }
}

function onDelete(row) {
  deleteConfirm.id = row.id
  deleteConfirm.show = true
}

async function onDeleteConfirm() {
  if (!deleteConfirm.id) return
  try {
    await store.removeTemplate(deleteConfirm.id)
    toast.success(t('cmdbAttrTemplates.deleted'))
  } catch (e) {
    errorConfirm.message = e.j?.error || t('cmdbAttrTemplates.delete_failed')
    errorConfirm.show = true
  }
}

onMounted(() => {
  store.fetchTemplates().catch((e) => {
    errorConfirm.message = e.j?.error || t('error.cmdbAttrTemplatesFailed')
    errorConfirm.show = true
  })
})
</script>

<style scoped>
.btnbar { display: flex; gap: 8px; margin-top: 8px; }
.field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 10px; min-width: 220px; }
.field label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.row-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.options-cell { display: inline-block; max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.poll-err {
  padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}

@media (max-width: 768px) {
  .row-2 { grid-template-columns: 1fr; }
}
</style>