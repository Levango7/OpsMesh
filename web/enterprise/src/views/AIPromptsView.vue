<template>
  <PageShell
    :title="$t('prompts.title')"
    :desc="$t('prompts.desc')"
    testid="ai-prompts"
    :error="store.error"
  >
    <div class="btnbar btnbar--bottom">
      <Button variant="primary" size="md" @click="openCreate" data-testid="ai-prompts-create-btn">
        <Icon name="add" :size="14" /> {{ $t('prompts.add') }}
      </Button>
      <Button variant="ghost" size="md" @click="reload"><Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}</Button>
    </div>

    <div v-if="store.loading && !store.templates.length" class="muted">{{ $t('common.loading') }}</div>
    <DataTable
      v-else
      :columns="columns"
      :rows="store.templates"
      row-key="id"
      :loading="store.loading"
      :empty-text="$t('prompts.empty')"
    >
      <template #cell-content="{ value }">
        <span class="muted">{{ preview(value) }}</span>
      </template>
      <template #cell-actions="{ row }">
        <div class="row-actions">
          <Button variant="ghost" size="sm" @click="openEdit(row)" data-testid="ai-prompts-edit-btn"><Icon name="edit" :size="13" /></Button>
          <Button variant="danger" size="sm" @click="onDelete(row)" data-testid="ai-prompts-delete-btn"><Icon name="delete" :size="13" /></Button>
        </div>
      </template>
    </DataTable>

    <!-- 新增/编辑 -->
    <DetailDrawer :open="!!form" :title="form && form.id ? $t('prompts.edit') : $t('prompts.add')" @close="form = null">
      <form v-if="form" class="entity-form" @submit.prevent="onSave">
        <div class="field">
          <label>{{ $t('prompts.field_name') }}</label>
          <input v-model.trim="form.name" type="text" required :placeholder="$t('prompts.field_name_placeholder')" />
        </div>
        <div class="field">
          <label>{{ $t('prompts.field_category') }}</label>
          <input v-model.trim="form.category" type="text" :placeholder="$t('prompts.field_category_placeholder')" />
        </div>
        <div class="field">
          <label>{{ $t('prompts.field_content') }}</label>
          <textarea v-model="form.content" rows="10" required></textarea>
          <small class="hint">{{ $t('prompts.content_hint') }}</small>
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar btnbar--bottom">
          <Button variant="primary" size="md" native-type="submit" :disabled="saving" data-testid="ai-prompts-save-btn">
            <Icon name="success" :size="14" /> {{ $t('common.save') }}
          </Button>
          <Button variant="ghost" size="md" native-type="button" @click="form = null">{{ $t('common.cancel') }}</Button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 删除确认 -->
    <ConfirmModal
      v-model="deleteConfirm.show"
      data-testid="ai-prompts-delete-confirm-modal"
      :title="$t('common.delete')"
      :message="$t('prompts.confirm_delete')"
      @confirm="onDeleteConfirm"
    />
  </PageShell>
</template>

<script setup>
// AI 提示词模板管理（2026-11-02）。
// 背景：后端 5 条 CRUD（GET/POST /api/v1/ai/prompts、GET/PUT/DELETE /{id}，
// internal/ai/gateway/handler.go:368-378）此前 api 层已接但无任何界面消费方——
// 属死 API 面清查交产品裁后补的缺口。写权限（create/update/delete）走 ai:write，
// 本页路由门挂 ai:read（读即可进页，写操作在后端另行 403 并弹错）。
import { ref, computed, onMounted } from 'vue'
import { t } from '@/i18n'
import PageShell from '@/components/PageShell.vue'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import Button from '@/components/ui/Button.vue'
import Icon from '@/components/Icon.vue'
import { useAIPromptsStore } from '@/stores/ai-prompts'

const store = useAIPromptsStore()

const columns = computed(() => [
  { key: 'name', label: t('prompts.col_name') },
  { key: 'category', label: t('prompts.col_category') },
  { key: 'content', label: t('prompts.col_content') },
  { key: 'updatedAt', label: t('prompts.col_updated') },
  { key: 'actions', label: '', width: '96px' }
])

const form = ref(null)
const formError = ref('')
const saving = ref(false)
const deleteConfirm = ref({ show: false, row: null })

function reload() { store.fetchTemplates() }
function openCreate() { formError.value = ''; form.value = { id: '', name: '', category: '', content: '' } }
function openEdit(row) { formError.value = ''; form.value = { ...row } }
function preview(v) { return v && v.length > 60 ? v.slice(0, 60) + '…' : (v || '-') }

async function onSave() {
  saving.value = true
  formError.value = ''
  try {
    await store.saveTemplate(form.value)
    form.value = null
  } catch (e) {
    formError.value = e.j?.error || t('prompts.saveFailed')
  } finally {
    saving.value = false
  }
}

function onDelete(row) { deleteConfirm.value = { show: true, row } }
async function onDeleteConfirm() {
  const row = deleteConfirm.value.row
  if (!row) return
  try {
    await store.removeTemplate(row.id)
  } catch (e) {
    store.error = e.j?.error || t('prompts.deleteFailed')
  }
}

onMounted(reload)
</script>

<style scoped>
/* 共享原语全部在 styles/pages.css；本页无页面专属样式 */
</style>
