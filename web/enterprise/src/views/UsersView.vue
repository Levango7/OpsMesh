<template>
  <div>
    <h2>{{ $t('users.title') }}</h2>
    <p class="muted">{{ $t('users.subtitle') }}</p>

    <div class="btnbar">
      <button class="primary" @click="openCreate">
        <Icon name="add" :size="14" />
        {{ $t('users.add') }}
      </button>
      <button class="outline" @click="fetchUsers">
        <Icon name="refresh" :size="14" />
        {{ $t('common.refresh') }}
      </button>
    </div>

    <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
    <div v-else>
      <DataTable :columns="columns" :rows="users" row-key="id" :empty-text="$t('users.empty')">
        <template #cell-username="{ value }"><code>{{ value }}</code></template>
        <template #cell-status="{ value }">
          <StatusBadge :status="value === 'active' ? 'ok' : 'info'" :text="value === 'active' ? $t('users.active') : $t('users.disabled')" />
        </template>
        <template #cell-role_ids="{ row }">
          <span>{{ roleNames(row.role_ids) }}</span>
        </template>
        <template #cell-created_at="{ value }">{{ fmtTime(value) }}</template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <button class="xs outline" @click="openEdit(row)"><Icon name="edit" :size="13" /></button>
            <button class="xs danger" @click="onDelete(row)"><Icon name="delete" :size="13" /></button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- 新增/编辑抽屉 -->
    <DetailDrawer :open="!!form" :title="form && form.id ? $t('users.edit') : $t('users.add')" @close="form = null">
      <form v-if="form" class="user-form" @submit.prevent="onSave">
        <div class="field">
          <label>{{ $t('users.new_username') }}</label>
          <input v-model.trim="form.username" type="text" :disabled="!!form.id" required />
        </div>
        <div v-if="!form.id" class="field">
          <label>{{ $t('users.new_password') }}</label>
          <input v-model.trim="form.password" type="password" required />
        </div>
        <div class="field">
          <label>{{ $t('users.new_email') }}</label>
          <input v-model.trim="form.email" type="email" />
        </div>
        <div class="field">
          <label>{{ $t('users.new_roles') }}</label>
          <div class="checkbox-group">
            <label v-for="r in roles" :key="r.id" class="checkbox-item">
              <input type="checkbox" :value="r.id" v-model="form.role_ids" />
              <span>{{ r.name }}</span>
            </label>
            <span v-if="!roles.length" class="muted">{{ $t('common.none') }}</span>
          </div>
        </div>
        <div v-if="form.id" class="field">
          <label>{{ $t('users.status') }}</label>
          <select v-model="form.status">
            <option value="active">{{ $t('users.active') }}</option>
            <option value="disabled">{{ $t('users.disabled') }}</option>
          </select>
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('users.save') }}</button>
          <button type="button" class="outline" @click="form = null">{{ $t('users.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>
  </div>
</template>

<script setup>
// 用户管理页 — 列表 + 新增/编辑/删除，复用 DataTable / DetailDrawer / StatusBadge
import { ref, onMounted } from 'vue'
import { authApi } from '@/api/auth'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import Icon from '@/components/Icon.vue'

const users = ref([])
const roles = ref([])
const loading = ref(false)
const form = ref(null)
const formError = ref('')

const columns = [
  { key: 'username', title: t('users.username'), slot: 'cell-username' },
  { key: 'email', title: t('users.email') },
  { key: 'status', title: t('users.status'), slot: 'cell-status' },
  { key: 'role_ids', title: t('users.roles'), slot: 'cell-role_ids' },
  { key: 'created_at', title: t('users.created_at'), slot: 'cell-created_at' },
  { key: 'actions', title: t('users.actions'), slot: 'cell-actions', width: '90px' }
]

async function fetchUsers() {
  loading.value = true
  try {
    const r = await authApi.listUsers()
    users.value = (r && r.users) || r || []
  } catch (e) {
    users.value = []
  } finally {
    loading.value = false
  }
}

async function fetchRoles() {
  try {
    const r = await authApi.listRoles()
    roles.value = (r && r.roles) || r || []
  } catch (e) {
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

function openCreate() {
  formError.value = ''
  form.value = { id: null, username: '', password: '', email: '', role_ids: [], status: 'active' }
}

function openEdit(row) {
  formError.value = ''
  form.value = { id: row.id, username: row.username, email: row.email || '', role_ids: [...(row.role_ids || [])], status: row.status || 'active' }
}

async function onSave() {
  formError.value = ''
  try {
    if (form.value.id) {
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

async function onDelete(row) {
  if (!window.confirm(t('users.confirm_delete'))) return
  try {
    await authApi.deleteUser(row.id)
    await fetchUsers()
  } catch (e) {
    window.alert(e.j?.error || t('users.delete_failed'))
  }
}

function fmtTime(s) {
  if (!s) return ''
  const d = new Date(s)
  return isNaN(d.getTime()) ? s : d.toLocaleString('zh-CN', { hour12: false })
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