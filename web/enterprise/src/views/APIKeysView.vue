<template>
  <div>
    <h2>{{ $t('apikeys.title') }}</h2>
    <p class="muted">{{ $t('apikeys.subtitle') }}</p>

    <div class="btnbar">
      <button class="primary" data-testid="apikeys-add-btn" @click="openCreate">
        <Icon name="add" :size="14" />
        {{ $t('apikeys.add') }}
      </button>
      <button class="outline" @click="fetchKeys">
        <Icon name="refresh" :size="14" />
        {{ $t('common.refresh') }}
      </button>
    </div>

    <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
    <DataTable
      v-else
      :columns="columns"
      :rows="keys"
      row-key="id"
      :empty-text="$t('apikeys.empty')"
    >
      <template #cell-name="{ value }"><code>{{ value }}</code></template>
      <template #cell-scopes="{ value }">
        <div class="scope-tags">
          <span v-for="s in value || []" :key="s" class="scope-tag">{{ s }}</span>
          <span v-if="!(value || []).length" class="muted">{{ $t('common.none') }}</span>
        </div>
      </template>
      <template #cell-rateLimitPerSec="{ value }">{{ value > 0 ? value : $t('apikeys.unlimited') }}</template>
      <template #cell-enabled="{ value }">
        <StatusBadge :status="value ? 'ok' : 'fail'" :text="value ? $t('apikeys.enabled') : $t('apikeys.disabled')" />
      </template>
      <template #cell-expiresAt="{ value }">{{ fmtTime(value, $t('apikeys.never_expire')) }}</template>
      <template #cell-lastUsedAt="{ value }">{{ fmtTime(value, '—') }}</template>
      <template #cell-createdAt="{ value }">{{ fmtTime(value) }}</template>
      <template #cell-actions="{ row }">
        <div class="row-actions">
          <button class="xs outline" @click="openEdit(row)"><Icon name="edit" :size="13" /></button>
          <!-- 启停切换 -->
          <button v-if="!row.enabled" class="xs outline" @click="onToggle(row, true)">{{ $t('apikeys.enable') }}</button>
          <button v-else class="xs outline" @click="onToggle(row, false)">{{ $t('apikeys.disable') }}</button>
          <button class="xs danger" @click="onDelete(row)"><Icon name="delete" :size="13" /></button>
        </div>
      </template>
    </DataTable>

    <!-- 新增/编辑抽屉 -->
    <DetailDrawer :open="!!form" :title="form && form.id ? $t('apikeys.edit') : $t('apikeys.add')" @close="form = null">
      <form v-if="form" class="key-form" @submit.prevent="onSave">
        <div class="field">
          <label>{{ $t('apikeys.new_name') }}</label>
          <input v-model.trim="form.name" type="text" required />
        </div>
        <div class="field">
          <label>{{ $t('apikeys.new_scopes') }}</label>
          <div class="checkbox-group">
            <label v-for="s in scopeOptions" :key="s" class="checkbox-item">
              <input type="checkbox" :value="s" v-model="form.scopes" />
              <span>{{ s }}</span>
            </label>
          </div>
          <span v-if="!form.scopes.length" class="msg err">{{ $t('apikeys.scopes_required') }}</span>
        </div>
        <div class="field">
          <label>{{ $t('apikeys.new_rate_limit') }}</label>
          <input v-model.number="form.rateLimitPerSec" type="number" min="0" step="1" />
          <span class="hint">{{ $t('apikeys.rate_limit_hint') }}</span>
        </div>
        <div class="field">
          <label>{{ $t('apikeys.new_expires_at') }}</label>
          <input v-model="form.expiresAt" type="date" />
          <span class="hint">{{ $t('apikeys.expires_hint') }}</span>
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('apikeys.save') }}</button>
          <button type="button" class="outline" @click="form = null">{{ $t('apikeys.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 删除确认（替代 confirm） -->
    <ConfirmModal
      v-model="deleteConfirm.show"
      data-testid="apikeys-delete-confirm-modal"
      :title="$t('apikeys.delete')"
      :message="$t('apikeys.confirm_delete')"
      @confirm="onDeleteConfirm"
    />
    <!-- 创建成功：明文 key 仅显示一次（info 弹窗替代 alert） -->
    <ConfirmModal
      v-model="plainKeyConfirm.show"
      data-testid="apikeys-plainkey-modal"
      :title="$t('apikeys.plainkey_title')"
      :message="plainKeyConfirm.message"
      info
    />
    <!-- 错误提示（替代 alert） -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="apikeys-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// API Key 管理页 — 列表 + 新增/编辑/删除 + 启停
// 创建成功后后端返回明文 key（plainKey），仅此一次，用 info 弹窗展示给用户复制。
import { ref, reactive, onMounted } from 'vue'
import { listAPIKeys, createAPIKey, updateAPIKey, deleteAPIKey, enableAPIKey, disableAPIKey } from '@/api/apikeys'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { fmtTime } from '@/composables/useFormatTime'

const keys = ref([])
const loading = ref(false)
const form = ref(null)
const formError = ref('')

// 删除确认弹窗（替代 confirm）
const deleteConfirm = reactive({ show: false, row: null })
// 明文 key 一次性展示弹窗
const plainKeyConfirm = reactive({ show: false, message: '' })
// 错误提示弹窗（替代 alert）
const errorConfirm = reactive({ show: false, message: '' })

// 权限范围候选（与平台权限点命名一致，用户可多选）
const scopeOptions = [
  'device:read', 'device:write',
  'task:read', 'task:write',
  'alert:read', 'alert:write',
  'log:read',
  'cmdb:read', 'cmdb:write',
  'workflow:read', 'workflow:write',
  'deploy:read', 'deploy:write',
  'billing:read', 'billing:write',
  'gateway:read', 'gateway:write',
  'audit:read'
]

const columns = [
  { key: 'name', title: t('apikeys.name'), slot: 'cell-name' },
  { key: 'scopes', title: t('apikeys.scopes'), slot: 'cell-scopes' },
  { key: 'rateLimitPerSec', title: t('apikeys.rate_limit'), slot: 'cell-rateLimitPerSec', width: '100px' },
  { key: 'enabled', title: t('apikeys.status'), slot: 'cell-enabled', width: '80px' },
  { key: 'expiresAt', title: t('apikeys.expires_at'), slot: 'cell-expiresAt', width: '150px' },
  { key: 'lastUsedAt', title: t('apikeys.last_used_at'), slot: 'cell-lastUsedAt', width: '150px' },
  { key: 'createdAt', title: t('apikeys.created_at'), slot: 'cell-createdAt', width: '150px' },
  { key: 'actions', title: t('apikeys.actions'), slot: 'cell-actions', width: '140px' }
]

async function fetchKeys() {
  loading.value = true
  try {
    const r = await listAPIKeys()
    keys.value = (r && r.apiKeys) || r || []
  } catch {
    keys.value = []
  } finally {
    loading.value = false
  }
}

function openCreate() {
  formError.value = ''
  form.value = { id: null, name: '', scopes: [], rateLimitPerSec: 0, expiresAt: '' }
}

function openEdit(row) {
  formError.value = ''
  // expiresAt 是 RFC3339 字符串，date input 只需要 yyyy-mm-dd 部分
  let exp = ''
  if (row.expiresAt) {
    const d = new Date(row.expiresAt)
    if (!isNaN(d.getTime())) exp = d.toISOString().slice(0, 10)
  }
  form.value = { id: row.id, name: row.name || '', scopes: [...(row.scopes || [])], rateLimitPerSec: row.rateLimitPerSec || 0, expiresAt: exp }
}

async function onSave() {
  formError.value = ''
  if (!form.value.scopes.length) {
    formError.value = t('apikeys.scopes_required')
    return
  }
  try {
    if (form.value.id) {
      // 后端白名单：PUT 仅接受 name + scopes（scopes 不允许清空）
      await updateAPIKey(form.value.id, { name: form.value.name, scopes: form.value.scopes })
      toast.success(t('apikeys.update_ok'))
      form.value = null
      await fetchKeys()
    } else {
      const { j } = await createAPIKey({
        name: form.value.name,
        scopes: form.value.scopes,
        rateLimitPerSec: Number(form.value.rateLimitPerSec) || 0,
        // 空串表示永不过期；否则补全为当天 23:59:59 的 RFC3339
        expiresAt: form.value.expiresAt ? new Date(`${form.value.expiresAt}T23:59:59`).toISOString() : ''
      })
      form.value = null
      await fetchKeys()
      // 明文 key 仅此一次：info 弹窗展示（列表接口只会返回 hash，无法再次查看）
      const plain = j && j.plainKey
      if (plain) {
        plainKeyConfirm.message = t('apikeys.plainkey_message', { key: plain })
        plainKeyConfirm.show = true
      } else {
        toast.success(t('apikeys.create_ok'))
      }
    }
  } catch (e) {
    formError.value = e.j?.error || t('apikeys.save_failed')
  }
}

// 启停切换
async function onToggle(row, enable) {
  try {
    if (enable) await enableAPIKey(row.id)
    else await disableAPIKey(row.id)
    toast.success(enable ? t('apikeys.enable_ok') : t('apikeys.disable_ok'))
    await fetchKeys()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('apikeys.toggle_failed')
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
    await deleteAPIKey(row.id)
    toast.success(t('apikeys.delete_ok'))
    await fetchKeys()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('apikeys.delete_failed')
    errorConfirm.show = true
  }
}

onMounted(() => {
  fetchKeys()
})
</script>

<style scoped>
.row-actions { display: flex; gap: 4px; }
.scope-tags { display: flex; flex-wrap: wrap; gap: 4px; }
.scope-tag {
  display: inline-flex; align-items: center; height: 19px;
  padding: 0 8px; border-radius: 999px;
  background: var(--accent-soft); color: var(--accent);
  font-size: 11px; font-weight: 600; font-family: var(--mono, monospace);
}
.key-form { display: flex; flex-direction: column; gap: 12px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field > label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.checkbox-group { display: flex; flex-wrap: wrap; gap: 8px; }
.checkbox-item {
  display: inline-flex; align-items: center; gap: 5px;
  font-size: 13px; color: var(--text); margin: 0;
  padding: 4px 10px; border: 1px solid var(--border); border-radius: var(--radius-sm);
  background: var(--surface-2); cursor: pointer;
}
.hint { font-size: 12px; color: var(--text-3); }
.btnbar { margin-top: 8px; }
</style>
