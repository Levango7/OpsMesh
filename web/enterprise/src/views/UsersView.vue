<template>
  <PageShell :title="$t('users.title')" :desc="$t('users.subtitle')" testid="users" :error="loadErr">
    <div v-if="!canWrite" class="muted" data-testid="write-denied-hint">{{ $t('common.write_denied') }}</div>
    <div class="btnbar btnbar--bottom">
      <Button variant="primary" size="md" data-testid="users-add-btn" @click="openCreate" :disabled="!canWrite" :title="canWrite ? '' : $t('common.write_denied')">
        <Icon name="add" :size="14" />
        {{ $t('users.add') }}
      </Button>
      <Button variant="ghost" size="md" data-testid="users-refresh-btn" @click="fetchUsers">
        <Icon name="refresh" :size="14" />
        {{ $t('common.refresh') }}
      </Button>
    </div>

    <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
    <div v-else data-testid="users-table-wrap">
      <DataTable :loading="loading" :columns="columns" :rows="users" row-key="id" :empty-text="$t('users.empty')" data-testid="users-table" scrollable>
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
            <Button variant="primary" size="sm" v-if="row.status === 'pending'" data-testid="btn-row-approve" @click="onApprove(row)" :disabled="!canWrite" :title="canWrite ? '' : $t('common.write_denied')"><Icon name="success" :size="13" /> {{ $t('users.approve') }}</Button>
            <Button variant="danger" size="sm" v-if="row.status === 'pending'" data-testid="btn-row-reject" @click="askReject(row)" :disabled="!canWrite" :title="canWrite ? '' : $t('common.write_denied')"><Icon name="delete" :size="13" /> {{ $t('users.reject') }}</Button>
            <Button variant="ghost" size="sm" data-testid="btn-row-edit" @click="openEdit(row)" :disabled="!canWrite" :title="canWrite ? '' : $t('common.write_denied')"><Icon name="edit" :size="13" /></Button>
            <Button variant="danger" size="sm" data-testid="btn-row-delete" @click="onDelete(row)" :disabled="!canWrite" :title="canWrite ? '' : $t('common.write_denied')"><Icon name="delete" :size="13" /></Button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- ADR-2026-11-15 §ADR-3：服务端分页 -->
    <Pagination
      v-if="total > pageSize"
      :page="page"
      :page-size="users.length"
      :limit="pageSize"
      :total="total"
      @prev="fetchUsers(page - 1)"
      @next="fetchUsers(page + 1)"
    />

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
        <div class="btnbar btnbar--bottom">
          <Button variant="primary" size="md" native-type="submit" :disabled="!canWrite || saving" :title="canWrite ? '' : $t('common.write_denied')" data-testid="users-save-btn"><Icon name="success" :size="14" /> {{ $t('users.save') }}</Button>
          <Button variant="ghost" size="md" native-type="button" data-testid="users-cancel-btn" @click="form = null">{{ $t('users.cancel') }}</Button>
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
    <!-- 拒绝确认（替代直达 API） -->
    <ConfirmModal
      v-model="rejectConfirm.show"
      data-testid="users-reject-confirm-modal"
      :title="$t('users.reject')"
      :message="$t('users.confirm_reject')"
      @confirm="onRejectConfirm"
    />
    <!-- 错误提示（替代 alert） -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="users-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </PageShell>
</template>

<script setup>
// 用户管理页 — 列表 + 新增/编辑/删除，复用 DataTable / DetailDrawer / StatusBadge
import PageShell from '@/components/PageShell.vue'
import { reactive, ref, computed, onMounted } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { authApi } from '@/api/auth'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import { fmtTime } from '@/composables/useFormatTime'

// 写操作门：与后端 requirePermission('user:write') 同源；只读者显式禁用写按钮。
const auth = useAuthStore()
const canWrite = computed(() => auth.hasPerm('user:write'))
import Button from "@/components/ui/Button.vue"
import Pagination from '@/components/Pagination.vue'

const users = ref([])
const roles = ref([])
// ADR-2026-11-15 §ADR-3：服务端分页状态
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const loading = ref(false)
const saving = ref(false)
const form = ref(null)
const formError = ref('')
const loadErr = ref('')

// 删除确认弹窗（替代 confirm）
const deleteConfirm = reactive({ show: false, row: null })
// 错误提示弹窗（替代 alert）
const errorConfirm = reactive({ show: false, message: '' })

// 拒绝确认弹窗（2026-11-06 五维排查批 3）：reject 此前直达 API，与同页删除的交互规范不一致。
const rejectConfirm = reactive({ show: false, row: null })
function askReject(row) {
  rejectConfirm.row = row
  rejectConfirm.show = true
}
async function onRejectConfirm() {
  const row = rejectConfirm.row
  rejectConfirm.show = false
  if (!row) return
  try {
    await authApi.rejectUser(row.id)
    await fetchUsers()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('users.reject_failed')
    errorConfirm.show = true
  }
}

const columns = [
  { key: 'username', title: t('users.username'), slot: 'cell-username', sortable: true },
  { key: 'email', title: t('users.email') },
  { key: 'status', title: t('users.status'), slot: 'cell-status', sortable: true },
  { key: 'roleIDs', title: t('users.roles'), slot: 'cell-roleIDs', sortable: true },
  { key: 'createdAt', title: t('users.created_at'), slot: 'cell-createdAt', sortable: true },
  { key: 'actions', title: t('users.actions'), slot: 'cell-actions', width: '200px' }
]

// 请求序号：只允许最后发出的请求写回状态（rows/total/page/loadErr/loading），
// 防止慢的旧响应覆盖新页面数据，或旧失败把新成功翻成错误横幅。
let usersSeq = 0

// 容错解包：支持 null / 旧版数组 / {users|null} / 缺失或字符串 total/page。
// 数字统一收敛：total >= 0，page >= 1；异常值不抛错。
function normalizeUsersResponse(r, fallbackPage) {
  const envelope = Array.isArray(r) ? { users: r } : (r && typeof r === 'object' ? r : {})
  const rows = Array.isArray(envelope.users) ? envelope.users : []
  const rawTotal = Number(envelope.total)
  const normalizedTotal = Number.isFinite(rawTotal) ? Math.max(0, Math.floor(rawTotal)) : rows.length
  const rawPage = Number(envelope.page)
  const normalizedPage = Number.isFinite(rawPage) ? Math.max(1, Math.floor(rawPage)) : Math.max(1, fallbackPage)
  return { rows, total: normalizedTotal, page: normalizedPage }
}

async function fetchUsers(target) {
  // 兼容 @click="fetchUsers"（事件对象 / 刷新按钮）与 fetchUsers(n)（分页器）两种调用。
  // 分页请求在加载中被忽略，避免连点叠发；刷新按钮不受限制，可强制发起新请求
  // （新请求序号更大，在途的旧响应随后会被丢弃）。
  const isPageChange = typeof target === 'number'
  if (isPageChange && loading.value) return
  const p = Math.max(1, Math.floor(isPageChange ? target : page.value))
  const seq = ++usersSeq
  loading.value = true
  loadErr.value = ''
  let ok = false
  try {
    const r = await authApi.listUsers({ page: p, pageSize: pageSize.value })
    if (seq !== usersSeq) return
    const norm = normalizeUsersResponse(r, p)
    users.value = norm.rows
    total.value = norm.total
    page.value = norm.page
    ok = true
  } catch (e) {
    if (seq !== usersSeq) return
    // 此前静默吞错（只见空表）——统一骨架批次补上可见的加载错误
    users.value = []
    loadErr.value = e?.j?.error || t('common.load_failed')
  } finally {
    // 旧请求结束不得关闭新请求的 loading
    if (seq === usersSeq) loading.value = false
  }
  // 删除/审批导致当前页为空时回退一页，避免停在空页（仅最新且成功的请求触发）
  if (ok && seq === usersSeq && !users.value.length && page.value > 1) {
    await fetchUsers(page.value - 1)
  }
}

async function fetchRoles() {
  try {
    const r = await authApi.listRoles()
    roles.value = (r && r.roles) || r || []
  } catch (e) {
    // 角色列表仅供分配/名称回显，失败同样在骨架横幅可见化
    roles.value = []
    loadErr.value = e.j?.error || t('common.load_failed')
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
  if (saving.value) return
  saving.value = true
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
  } finally {
    saving.value = false
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
.user-form { display: flex; flex-direction: column; gap: 12px; }
/* .row-actions / .field / .field > label / .btnbar 已改用 styles/pages.css 全局原语 */
.checkbox-group { display: flex; flex-wrap: wrap; gap: 8px; }
</style>