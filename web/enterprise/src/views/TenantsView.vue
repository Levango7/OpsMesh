<template>
  <div>
    <h2>{{ $t('tenants.title') }}</h2>
    <p class="muted">{{ $t('tenants.subtitle') }}</p>

    <div class="btnbar">
      <button class="primary" @click="openCreate">
        <Icon name="add" :size="14" />
        {{ $t('tenants.add') }}
      </button>
      <button class="outline" @click="fetchTenants">
        <Icon name="refresh" :size="14" />
        {{ $t('common.refresh') }}
      </button>
    </div>

    <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
    <div v-else>
      <DataTable :columns="columns" :rows="tenants" row-key="id" :empty-text="$t('tenants.empty')">
        <template #cell-name="{ row }">
          <code>{{ row.name }}</code>
          <span v-if="row.displayName" class="dn">{{ row.displayName }}</span>
        </template>
        <template #cell-status="{ value }">
          <span class="tag" :class="'st-' + value">{{ $t('tenants.status_' + value) }}</span>
        </template>
        <template #cell-usage="{ row }">
          {{ usageText(row) }}
        </template>
        <template #cell-createdAt="{ value }">{{ fmtTime(value) }}</template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <button
              v-if="row.status !== 'suspended'"
              class="xs outline"
              :title="$t('tenants.suspend')"
              @click="onSuspend(row)"
            ><Icon name="warning" :size="13" /></button>
            <button
              v-else
              class="xs outline"
              :title="$t('tenants.activate')"
              @click="onActivate(row)"
            ><Icon name="success" :size="13" /></button>
            <button class="xs outline" @click="openEdit(row)"><Icon name="edit" :size="13" /></button>
            <button class="xs danger" @click="onDelete(row)"><Icon name="delete" :size="13" /></button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- 新增/编辑租户抽屉 -->
    <DetailDrawer :open="!!form" :title="form && form.id ? $t('tenants.edit') : $t('tenants.add')" @close="form = null">
      <form v-if="form" class="tenant-form" @submit.prevent="onSave">
        <div class="field">
          <label>{{ $t('tenants.new_name') }}</label>
          <input v-model.trim="form.name" type="text" :disabled="!!form.id" required />
        </div>
        <div class="field">
          <label>{{ $t('tenants.new_display_name') }}</label>
          <input v-model.trim="form.displayName" type="text" />
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('tenants.save') }}</button>
          <button type="button" class="outline" @click="form = null">{{ $t('tenants.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 暂停确认（替代 confirm） -->
    <ConfirmModal
      v-model="suspendConfirm.show"
      data-testid="tenants-suspend-confirm-modal"
      :title="$t('tenants.suspend')"
      :message="$t('tenants.confirm_suspend')"
      @confirm="onSuspendConfirm"
    />
    <!-- 删除确认（替代 confirm） -->
    <ConfirmModal
      v-model="deleteConfirm.show"
      data-testid="tenants-delete-confirm-modal"
      :title="$t('tenants.delete')"
      :message="$t('tenants.confirm_delete')"
      @confirm="onDeleteConfirm"
    />
    <!-- 错误提示（替代 alert） -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="tenants-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// 租户管理页 — CRUD + 暂停/激活生命周期操作
import { ref, reactive, onMounted } from 'vue'
import * as tenantApi from '@/api/tenant'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import { fmtTime } from '@/composables/useFormatTime'

const tenants = ref([])
const loading = ref(false)
const form = ref(null)
const formError = ref('')

// 暂停确认弹窗
const suspendConfirm = reactive({ show: false, row: null })
// 删除确认弹窗
const deleteConfirm = reactive({ show: false, row: null })
// 错误提示弹窗
const errorConfirm = reactive({ show: false, message: '' })

const columns = [
  { key: 'name', title: t('tenants.name'), slot: 'cell-name' },
  { key: 'status', title: t('tenants.status'), slot: 'cell-status', width: '90px' },
  { key: 'usage', title: t('tenants.usage'), slot: 'cell-usage' },
  { key: 'createdAt', title: t('tenants.created_at'), slot: 'cell-createdAt' },
  { key: 'actions', title: t('tenants.actions'), slot: 'cell-actions', width: '130px' }
]

// 用量摘要（Tenant.usage：devices/tasks/alerts 等实时统计）
function usageText(row) {
  const u = row.usage || {}
  const parts = []
  if (u.devices != null) parts.push(t('quotas.devices') + ': ' + u.devices)
  if (u.tasks != null) parts.push(t('quotas.tasks') + ': ' + u.tasks)
  if (u.alerts != null) parts.push(t('quotas.alerts') + ': ' + u.alerts)
  return parts.length ? parts.join(' · ') : '—'
}

async function fetchTenants() {
  loading.value = true
  try {
    const r = await tenantApi.listTenants()
    tenants.value = (r && r.tenants) || []
  } catch {
    tenants.value = []
  } finally {
    loading.value = false
  }
}

function openCreate() {
  formError.value = ''
  form.value = { id: null, name: '', displayName: '' }
}

function openEdit(row) {
  formError.value = ''
  form.value = { id: row.id, name: row.name || '', displayName: row.displayName || '' }
}

async function onSave() {
  formError.value = ''
  if (!form.value.name) {
    formError.value = t('tenants.name_required')
    return
  }
  try {
    if (form.value.id) {
      await tenantApi.updateTenant(form.value.id, {
        name: form.value.name,
        displayName: form.value.displayName
      })
    } else {
      await tenantApi.createTenant({
        name: form.value.name,
        displayName: form.value.displayName
      })
    }
    toast.success(t('tenants.saved'))
    form.value = null
    await fetchTenants()
  } catch (e) {
    formError.value = e.j?.error || t('tenants.save_failed')
  }
}

function onSuspend(row) {
  suspendConfirm.row = row
  suspendConfirm.show = true
}

async function onSuspendConfirm() {
  const row = suspendConfirm.row
  if (!row) return
  try {
    await tenantApi.suspendTenant(row.id)
    toast.success(t('tenants.suspend_done'))
    await fetchTenants()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('tenants.suspend_failed')
    errorConfirm.show = true
  }
}

async function onActivate(row) {
  try {
    await tenantApi.activateTenant(row.id)
    toast.success(t('tenants.activate_done'))
    await fetchTenants()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('tenants.activate_failed')
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
    await tenantApi.deleteTenant(row.id)
    await fetchTenants()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('tenants.delete_failed')
    errorConfirm.show = true
  }
}

onMounted(fetchTenants)
</script>

<style scoped>
.row-actions { display: flex; gap: 4px; }
.dn { margin-left: 8px; color: var(--text-3); font-size: 12px; }
.tag {
  display: inline-block; padding: 2px 8px; border-radius: 999px;
  font-size: 12px; background: var(--accent-soft); color: var(--accent);
}
.tag.st-active { background: var(--ok-bg); color: var(--ok); }
.tag.st-suspended { background: var(--warn-bg); color: var(--warn); }
.tag.st-disabled { background: var(--fail-bg); color: var(--fail); }
.tenant-form { display: flex; flex-direction: column; gap: 12px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field > label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.btnbar { margin-top: 8px; }
</style>
