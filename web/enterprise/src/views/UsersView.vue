<template>
  <div data-testid="users-view">
    <h2 data-testid="users-title">{{ $t('users.title') }}</h2>
    <p class="muted">{{ $t('users.subtitle') }}</p>

    <div class="btnbar">
      <button class="primary" data-testid="users-add-btn" @click="openCreate">
        <Icon name="add" :size="14" />
        {{ $t('users.add') }}
      </button>
      <button class="outline" data-testid="users-refresh-btn" @click="fetchUsers">
        <Icon name="refresh" :size="14" />
        {{ $t('common.refresh') }}
      </button>
    </div>

    <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
    <div v-else data-testid="users-table-wrap">
      <DataTable :columns="columns" :rows="users" row-key="id" :empty-text="$t('users.empty')" data-testid="users-table">
        <template #cell-username="{ value }"><code>{{ value }}</code></template>
        <template #cell-status="{ row }">
          <StatusBadge
            :data-testid="'users-status-' + (row.status || '')"
            :status="statusBadgeKind(row.status)"
            :text="statusText(row.status)"
          />
        </template>
        <template #cell-roleIDs="{ row }">
          <span>{{ roleNames(row.roleIDs) }}</span>
        </template>
        <template #cell-createdAt="{ value }">{{ fmtTime(value) }}</template>
        <template #cell-actions="{ row }">
          <div class="row-actions" :data-testid="'users-row-' + (row.id || row.username)">
            <button v-if="row.status === 'pending'" class="xs primary" data-testid="btn-row-approve" @click="onApprove(row)"><Icon name="success" :size="13" /> {{ $t('users.approve') }}</button>
            <button v-if="row.status === 'pending'" class="xs danger" data-testid="btn-row-reject" @click="onReject(row)"><Icon name="delete" :size="13" /> {{ $t('users.reject') }}</button>
            <button class="xs outline" data-testid="btn-row-edit" @click="openEdit(row)"><Icon name="edit" :size="13" /></button>
            <button class="xs danger" data-testid="btn-row-delete" @click="onDelete(row)"><Icon name="delete" :size="13" /></button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- 新增/编辑抽屉 -->
    <DetailDrawer :open="!!form" :title="form && form.id ? $t('users.edit') : $t('users.add')" data-testid="users-form-drawer" @close="form = null">
      <form v-if="form" class="user-form" data-testid="users-form" @submit.prevent="onSave">
        <div class="field">
          <label>{{ $t('users.new_username') }}</label>
          <input v-model.trim="form.username" type="text" :disabled="!!form.id" required data-testid="input-username" />
        </div>
        <div v-if="!form.id" class="field">
          <label>{{ $t('users.new_password') }}</label>
          <input v-model.trim="form.password" type="password" required data-testid="input-password" />
        </div>
        <div class="field">
          <label>{{ $t('users.new_email') }}</label>
          <input v-model.trim="form.email" type="email" data-testid="input-email" />
        </div>
        <div class="field">
          <label>{{ $t('users.new_roles') }}</label>
          <div class="checkbox-group" data-testid="users-role-checkboxes">
            <label v-for="r in roles" :key="r.id" class="checkbox-item" :data-testid="'users-role-' + r.id">
              <input type="checkbox" :value="r.id" v-model="form.role_ids" />
              <span>{{ r.name }}</span>
            </label>
            <span v-if="!roles.length" class="muted">{{ $t('common.none') }}</span>
          </div>
        </div>
        <div v-if="form.id" class="field">
          <label>{{ $t('users.status') }}</label>
          <select v-model="form.status" data-testid="input-status">
            <option value="active">{{ $t('users.active') }}</option>
            <option value="pending">{{ $t('users.pending') }}</option>
            <option value="disabled">{{ $t('users.disabled') }}</option>
          </select>
        </div>
        <div v-if="formError" class="msg err" data-testid="users-form-error">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary" data-testid="users-save-btn"><Icon name="success" :size="14" /> {{ $t('users.save') }}</button>
          <button type="button" class="outline" data-testid="users-cancel-btn" @click="form = null">{{ $t('users.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 删除确认（替代 confirm） -->
    <ConfirmModal
      v-model="deleteConfirm.show"
      data-testid="users-delete-confirm-modal"
      :title="$t('users.delete')"
      :message="$t('users.confirm_delete')"
      @confirm="onDeleteConfirm"
    />
    <!-- 错误提示（替代 alert） -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="users-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// 用户管理页 — 列表 + 新增/编辑/删除，复用 DataTable / DetailDrawer / StatusBadge
import { reactive, ref, onMounted } from 'vue'
import { authApi } from '@/api/auth'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import { fmtTime } from '@/composables/useFormatTime'

const users = ref([])
const roles = ref([])
const loading = ref(false)
const form = ref(null)
const formError = ref('')

// 删除确认弹窗（替代 confirm）
const deleteConfirm = reactive({ show: false, row: null })
// 错误提示弹窗（替代 alert）
const errorConfirm = reactive({ show: false, message: '' })

const columns = [
  { key: 'username', title: t('users.username'), slot: 'cell-username', sortable: true },
  { key: 'email', title: t('users.email') },
  { key: 'status', title: t('users.status'), slot: 'cell-status', sortable: true },
  { key: 'roleIDs', title: t('users.roles'), slot: 'cell-roleIDs', sortable: true },
  { key: 'createdAt', title: t('users.created_at'), slot: 'cell-createdAt', sortable: true },
  { key: 'actions', title: t('users.actions'), slot: 'cell-actions', width: '200px' }
]

async function fetchUsers() {
  loading.value = true
  try {
    const r = await authApi.listUsers()
    users.value = (r && r.users) || r || []
  } catch {
    users.value = []
  } finally {
    loading.value = false
  }
}

async function fetchRoles() {
  try {
    const r = await authApi.listRoles()
    roles.value = (r && r.roles) || r || []
  } catch {
    roles.value = []
  }
}

function roleNames(ids) {
  if (!ids || !ids.length) return t('common.none')
  return ids.map((id) => {
    const r = roles.value.find((x) => x.id === id)
    return r ? r.name : id
  }).join(', ')
}

// 状态三态展示：active（正常）/ pending（待审批，琥珀色）/ disabled|rejected（禁用）。
// 不再用 active?ok:disabled 二值逻辑，避免 pending 用户被误标为"禁用"。
function statusBadgeKind(value) {
  if (value === 'active') return 'ok'
  if (value === 'pending') return 'warn'
  return 'info'
}

function statusText(value) {
  if (value === 'active') return t('users.active')
  if (value === 'pending') return t('users.pending')
  return t('users.disabled')
}

function openCreate() {
  formError.value = ''
  form.value = { id: null, username: '', password: '', email: '', role_ids: [], status: 'active' }
}

function openEdit(row) {
  formError.value = ''
  // 后端 User JSON 输出为驼峰（roleIDs），复制时须读驼峰字段。
  form.value = { id: row.id, username: row.username, email: row.email || '', role_ids: [...(row.roleIDs || [])], status: row.status || 'active' }
}

async function onSave() {
  formError.value = ''
  try {
    if (form.value.id) {
      // PUT /users/{id} 后端接收 json tag 为 role_ids（auth_users.go handleUpdateUser），
      // 请求体字段名与后端真实 tag 保持一致，避免静默清空角色。
      await authApi.updateUser(form.value.id, {
        email: form.value.email || undefined,
        role_ids: form.value.role_ids,
        status: form.value.status
      })
    } else {
      await authApi.createUser({
        username: form.value.username,
        password: form.value.password,
        email: form.value.email || undefined,
        role_ids: form.value.role_ids
      })
    }
    form.value = null
    await fetchUsers()
  } catch (e) {
    formError.value = e.j?.error || t('users.save_failed')
  }
}

// —— 注册审批（仅 status=pending 行显示操作按钮）——
async function onApprove(row) {
  try {
    await authApi.approveUser(row.id)
    await fetchUsers()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('users.approve_failed')
    errorConfirm.show = true
  }
}

async function onReject(row) {
  try {
    await authApi.rejectUser(row.id)
    await fetchUsers()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('users.reject_failed')
    errorConfirm.show = true
  }
}

async function onDelete(row) {
  deleteConfirm.row = row
  deleteConfirm.show = true
}

async function onDeleteConfirm() {
  const row = deleteConfirm.row
  if (!row) return
  try {
    await authApi.deleteUser(row.id)
    await fetchUsers()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('users.delete_failed')
    errorConfirm.show = true
  }
}


onMounted(() => {
  fetchUsers()
  fetchRoles()
})
</script>

<style scoped>
.row-actions { display: flex; gap: 4px; }
.user-form { display: flex; flex-direction: column; gap: 12px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field > label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.checkbox-group { display: flex; flex-wrap: wrap; gap: 8px; }
.checkbox-item {
  display: inline-flex; align-items: center; gap: 5px;
  font-size: 13px; color: var(--text); margin: 0;
  padding: 4px 10px; border: 1px solid var(--border); border-radius: var(--radius-sm);
  background: var(--surface-2); cursor: pointer;
}
.btnbar { margin-top: 8px; }
</style>