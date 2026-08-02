<template>
  <div>
    <h2>{{ $t('roles.title') }}</h2>
    <p class="muted">{{ $t('roles.subtitle') }}</p>

    <div class="btnbar">
      <button class="primary" @click="openCreate">
        <Icon name="add" :size="14" />
        {{ $t('roles.add') }}
      </button>
      <button class="outline" @click="fetchRoles">
        <Icon name="refresh" :size="14" />
        {{ $t('common.refresh') }}
      </button>
    </div>

    <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
    <div v-else>
      <DataTable :columns="columns" :rows="roles" row-key="id" :empty-text="$t('roles.empty')">
        <template #cell-name="{ value }"><code>{{ value }}</code></template>
        <template #cell-permissions="{ row }">
          <span class="perm-count">{{ (row.permissions || []).length }}</span>
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
    <DetailDrawer :open="!!form" :title="form && form.id ? $t('roles.edit') : $t('roles.add')" @close="form = null">
      <form v-if="form" class="role-form" @submit.prevent="onSave">
        <div class="field">
          <label>{{ $t('roles.new_name') }}</label>
          <input v-model.trim="form.name" type="text" :disabled="!!form.id" required />
        </div>
        <div class="field">
          <label>{{ $t('roles.new_description') }}</label>
          <input v-model.trim="form.description" type="text" />
        </div>
        <div class="field">
          <label>{{ $t('roles.new_permissions') }}</label>
          <div class="perm-group" v-for="g in permissionGroups" :key="g.group">
            <div class="perm-group-title">{{ g.group || '—' }}</div>
            <div class="checkbox-group">
              <label v-for="p in g.items" :key="p.id" class="checkbox-item">
                <input type="checkbox" :value="p.id" v-model="form.permissions" />
                <span>{{ p.name }}</span>
              </label>
            </div>
          </div>
          <span v-if="!permissions.length" class="muted">{{ $t('common.none') }}</span>
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('roles.save') }}</button>
          <button type="button" class="outline" @click="form = null">{{ $t('roles.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>
  </div>
</template>

<script setup>
// 角色管理页 — 列表 + 新增/编辑/删除 + 权限分配
import { ref, computed, onMounted } from 'vue'
import { authApi } from '@/api/auth'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'

const roles = ref([])
const permissions = ref([])
const loading = ref(false)
const form = ref(null)
const formError = ref('')

const columns = [
  { key: 'name', title: t('roles.name'), slot: 'cell-name' },
  { key: 'description', title: t('roles.description') },
  { key: 'permissions', title: t('roles.permissions'), slot: 'cell-permissions', width: '90px' },
  { key: 'created_at', title: t('roles.created_at'), slot: 'cell-created_at' },
  { key: 'actions', title: t('roles.actions'), slot: 'cell-actions', width: '90px' }
]

// 权限按 group 分组
const permissionGroups = computed(() => {
  const map = {}
  for (const p of permissions.value) {
    const g = p.group || '—'
    if (!map[g]) map[g] = []
    map[g].push(p)
  }
  return Object.entries(map).map(([group, items]) => ({ group, items }))
})

async function fetchRoles() {
  loading.value = true
  try {
    const r = await authApi.listRoles()
    roles.value = (r && r.roles) || r || []
  } catch (e) {
    roles.value = []
  } finally {
    loading.value = false
  }
}

async function fetchPermissions() {
  try {
    const r = await authApi.listPermissions()
    permissions.value = (r && r.permissions) || r || []
  } catch (e) {
    permissions.value = []
  }
}

function openCreate() {
  formError.value = ''
  form.value = { id: null, name: '', description: '', permissions: [] }
}

function openEdit(row) {
  formError.value = ''
  form.value = { id: row.id, name: row.name, description: row.description || '', permissions: [...(row.permissions || [])] }
}

async function onSave() {
  formError.value = ''
  try {
    if (form.value.id) {
      await authApi.updateRole(form.value.id, {
        description: form.value.description,
        permissions: form.value.permissions
      })
    } else {
      await authApi.createRole({
        name: form.value.name,
        description: form.value.description,
        permissions: form.value.permissions
      })
    }
    form.value = null
    await fetchRoles()
  } catch (e) {
    formError.value = e.j?.error || '保存失败'
  }
}

async function onDelete(row) {
  if (!window.confirm(t('roles.confirm_delete'))) return
  try {
    await authApi.deleteRole(row.id)
    await fetchRoles()
  } catch (e) {
    window.alert(e.j?.error || '删除失败')
  }
}

function fmtTime(s) {
  if (!s) return ''
  const d = new Date(s)
  return isNaN(d.getTime()) ? s : d.toLocaleString('zh-CN', { hour12: false })
}

onMounted(() => {
  fetchRoles()
  fetchPermissions()
})
</script>

<style scoped>
.row-actions { display: flex; gap: 4px; }
.perm-count {
  display: inline-flex; align-items: center; justify-content: center;
  min-width: 26px; height: 22px; padding: 0 7px;
  background: var(--accent-soft); color: var(--accent);
  border-radius: 999px; font-size: 12px; font-weight: 600;
}
.role-form { display: flex; flex-direction: column; gap: 12px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field > label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.perm-group { margin-bottom: 10px; }
.perm-group-title {
  font-size: 12px; font-weight: 600; color: var(--text-3);
  margin-bottom: 6px; text-transform: uppercase; letter-spacing: .04em;
}
.checkbox-group { display: flex; flex-wrap: wrap; gap: 8px; }
.checkbox-item {
  display: inline-flex; align-items: center; gap: 5px;
  font-size: 13px; color: var(--text); margin: 0;
  padding: 4px 10px; border: 1px solid var(--border); border-radius: var(--radius-sm);
  background: var(--surface-2); cursor: pointer;
}
.btnbar { margin-top: 8px; }
</style>