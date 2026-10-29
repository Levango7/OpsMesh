<template>
  <PageShell
    :title="t('cloud_accounts.title')"
    :desc="t('cloud_accounts.subtitle')"
    testid="cloud-accounts"
  >
    <!-- 工具栏 -->
    <div class="toolbar">
      <Button variant="primary" icon="add" @click="openCreate">{{ t('cloud_accounts.add') }}</Button>
      <Button variant="ghost" icon="refresh" @click="fetchAccounts">{{ t('common.refresh') }}</Button>
    </div>

    <!-- 加载中 -->
    <div v-if="loading" class="empty-state">{{ t('common.loading') }}</div>

    <!-- 云账号列表表格 -->
    <div v-else class="table-wrap card">
      <div class="flowbar">
        <h3 class="card__title">{{ t('cloud_accounts.title') }}</h3>
        <p class="muted" style="margin: 0 0 0 auto; text-align: right">{{ t('cloud_accounts.subtitle') }}</p>
      </div>
      <Table
        :data="accounts"
        :columns="columns"
        row-key="id"
        :loading="loading"
        :empty-text="t('cloud_accounts.empty')"
      >
        <template #cell-provider="{ value }">{{ providerLabel(value) }}</template>
        <template #cell-auth_type="{ value }">{{ authTypeLabel(value) }}</template>
        <template #cell-status="{ value }">
          <StatusBadge :status="statusMap[value] || 'info'" :text="statusLabel(value)" />
        </template>
        <template #cell-created_at="{ value }">{{ fmtTime(value) }}</template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <Button
              size="sm"
              variant="ghost"
              icon="refresh"
              :loading="syncingId === row.id"
              :disabled="syncingId === row.id"
              @click="onSync(row)"
              :title="t('cloud_accounts.sync')"
            >
              {{ t('cloud_accounts.sync') }}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              icon="clock"
              :loading="testingId === row.id"
              :disabled="testingId === row.id"
              @click="onTest(row)"
              :title="t('cloud_accounts.test')"
            >
              {{ t('cloud_accounts.test') }}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              icon="edit"
              @click="openEdit(row)"
              :title="t('cloud_accounts.edit')"
            >
              {{ t('cloud_accounts.edit') }}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              icon="task"
              @click="openResources(row)"
              :title="t('cloud_accounts.resources')"
            >
              {{ t('cloud_accounts.resources') }}
            </Button>
            <Button
              size="small"
              variant="danger"
              icon="delete"
              @click="onDelete(row)"
              :title="t('cloud_accounts.delete')"
            >
              {{ t('cloud_accounts.delete') }}
            </Button>
          </div>
        </template>
      </Table>
    </div>

    <!-- 发现资源抽屉 -->
    <Drawer
      v-model="resourcesDrawer"
      :title="t('cloud_accounts.resources') + ' · ' + (resourcesAccount?.name || '')"
      direction="right"
      size="720"
    >
      <Table
        :data="resources"
        :columns="resourceColumns"
        :loading="resourcesLoading"
        :empty-text="t('cloud_accounts.no_resources')"
      />
    </Drawer>

    <!-- 新增/编辑模态框 -->
    <Modal
      v-model="formModal"
      :title="formId ? t('cloud_accounts.edit') : t('cloud_accounts.add')"
      :confirm-loading="saving"
      :confirm-text="t('common.save')"
      :cancel-text="t('common.cancel')"
      @confirm="onSave"
      @cancel="formModal = false"
    >
      <form @submit.prevent="onSave" class="account-form">
        <Input
          :label="t('cloud_accounts.new_name')"
          v-model="form.name"
          :error="!!formError.name"
          required
          placeholder="prod-aws-main"
        />
        <Select
          :label="t('cloud_accounts.new_provider')"
          v-model="form.provider"
          :options="providerOptions"
          :error="!!formError.provider"
          required
        />
        <Select
          :label="t('cloud_accounts.new_region')"
          v-model="form.region"
          :options="regionOptions"
          :error="!!formError.region"
          required
          :placeholder="t('common.all')"
        />
        <Select
          :label="t('cloud_accounts.new_auth_type')"
          v-model="form.auth_type"
          :options="authTypeOptions"
          :error="!!formError.auth_type"
          required
        />
        <div v-if="formError.server" class="msg err">{{ formError.server }}</div>
      </form>
    </Modal>

    <!-- 删除确认模态框 -->
    <Modal
      v-model="deleteModal"
      :title="t('cloud_accounts.delete')"
      :confirm-loading="deleting"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      @confirm="onDeleteConfirm"
      @cancel="deleteModal = false"
    >
      <p>{{ t('cloud_accounts.confirm_delete') }}</p>
      <p v-if="deleteTarget" class="muted">{{ deleteTarget.name }} ({{ providerLabel(deleteTarget.provider) }})</p>
    </Modal>

    <!-- 同步确认模态框 -->
    <Modal
      v-model="syncModal"
      :title="t('cloud_accounts.sync')"
      :confirm-loading="syncing"
      :confirm-text="t('cloud_accounts.sync')"
      :cancel-text="t('common.cancel')"
      @confirm="onSyncConfirm"
      @cancel="syncModal = false"
    >
      <p>{{ t('cloud_accounts.confirm_sync') }}</p>
      <p v-if="syncTarget" class="muted">{{ syncTarget.name }} ({{ providerLabel(syncTarget.provider) }})</p>
    </Modal>

    <!-- 测试连接确认模态框 -->
    <Modal
      v-model="testModal"
      :title="t('cloud_accounts.test')"
      :confirm-loading="testing"
      :confirm-text="t('cloud_accounts.test')"
      :cancel-text="t('common.cancel')"
      @confirm="onTestConfirm"
      @cancel="testModal = false"
    >
      <p>{{ t('cloud_accounts.confirm_test') }}</p>
      <p v-if="testTarget" class="muted">{{ testTarget.name }} ({{ providerLabel(testTarget.provider) }})</p>
    </Modal>
  </PageShell>
</template>

<script setup>
import PageShell from '@/components/PageShell.vue'

// 云账号管理页 — 账号列表 / 创建编辑 / 同步 / 测试连接 / 删除 / 查看资源
import { ref, reactive, onMounted } from 'vue'
import * as cloudAccountApi from '@/api/cloud-account'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import { fmtTime } from '@/composables/useFormatTime'
import { Button, Table, Modal, Drawer, Input, Select } from '@/components/ui'
import StatusBadge from '@/components/StatusBadge.vue'

const PROVIDERS = [
  { value: 'aws', label: t('cloud_accounts.provider_aws') },
  { value: 'azure', label: t('cloud_accounts.provider_azure') },
  { value: 'gcp', label: t('cloud_accounts.provider_gcp') },
  { value: 'aliyun', label: t('cloud_accounts.provider_aliyun') },
  { value: 'huaweicloud', label: t('cloud_accounts.provider_huaweicloud') }
]
const AUTH_TYPES = [
  { value: 'aksk', label: t('cloud_accounts.auth_aksk') },
  { value: 'sts', label: t('cloud_accounts.auth_sts') },
  { value: 'oidc', label: t('cloud_accounts.auth_oidc') }
]
const statusMap = {
  active: 'ok',
  pending: 'warn',
  failed: 'fail',
  disabled: 'info'
}

const accounts = ref([])
const loading = ref(false)

// 表单模态框
const formModal = ref(false)
const formId = ref(null)
const form = reactive({ name: '', provider: '', region: '', auth_type: '' })
const formError = reactive({ name: '', provider: '', region: '', auth_type: '', server: '' })
const saving = ref(false)

// 删除确认
const deleteModal = ref(false)
const deleteTarget = ref(null)
const deleting = ref(false)

// 同步确认
const syncModal = ref(false)
const syncTarget = ref(null)
const syncing = ref(false)
const syncingId = ref(null)

// 测试连接确认
const testModal = ref(false)
const testTarget = ref(null)
const testing = ref(false)
const testingId = ref(null)

// 资源抽屉
const resourcesDrawer = ref(false)
const resourcesAccount = ref(null)
const resources = ref([])
const resourcesLoading = ref(false)

const columns = [
  { prop: 'name', label: t('cloud_accounts.col_name') },
  { prop: 'provider', label: t('cloud_accounts.col_provider') },
  { prop: 'region', label: t('cloud_accounts.col_region') },
  { prop: 'auth_type', label: t('cloud_accounts.col_auth_type') },
  { prop: 'status', label: t('cloud_accounts.col_status'), width: '110px' },
  { prop: 'created_at', label: t('cloud_accounts.col_created_at'), width: '150px' },
  { prop: 'actions', label: t('cloud_accounts.col_actions'), width: '260px' }
]

const resourceColumns = [
  { prop: 'name', label: t('cloud_accounts.col_name') },
  { prop: 'provider', label: t('cloud_accounts.col_provider') },
  { prop: 'region', label: t('cloud_accounts.col_region') },
  { prop: 'status', label: t('cloud_accounts.col_status'), width: '110px' }
]

async function fetchAccounts() {
  loading.value = true
  try {
    const { j } = await cloudAccountApi.listCloudAccounts()
    accounts.value = (j && j.accounts) || []
  } catch {
    accounts.value = []
    toast.error(t('cloud_accounts.save_failed'))
  } finally {
    loading.value = false
  }
}

function providerLabel(value) {
  const opt = PROVIDERS.find((p) => p.value === value)
  return opt ? opt.label : String(value)
}

function authTypeLabel(value) {
  const opt = AUTH_TYPES.find((a) => a.value === value)
  return opt ? opt.label : String(value)
}

function statusLabel(value) {
  const map = {
    active: t('cloud_accounts.status_active'),
    pending: t('cloud_accounts.status_pending'),
    failed: t('cloud_accounts.status_failed'),
    disabled: t('cloud_accounts.status_disabled')
  }
  return map[value] || String(value)
}

function openCreate() {
  formId.value = null
  formError.name = ''
  formError.provider = ''
  formError.region = ''
  formError.auth_type = ''
  formError.server = ''
  form.name = ''
  form.provider = ''
  form.region = ''
  form.auth_type = ''
  formModal.value = true
}

function openEdit(row) {
  formId.value = row.id
  formError.name = ''
  formError.provider = ''
  formError.region = ''
  formError.auth_type = ''
  formError.server = ''
  form.name = row.name || ''
  form.provider = row.provider || ''
  form.region = row.region || ''
  form.auth_type = row.auth_type || ''
  formModal.value = true
}

async function onSave() {
  formError.name = ''
  formError.provider = ''
  formError.region = ''
  formError.auth_type = ''
  formError.server = ''
  if (!form.name) {
    formError.name = t('cloud_accounts.new_name') + ' ' + t('common.error')
    return
  }
  if (!form.provider) {
    formError.provider = t('cloud_accounts.new_provider') + ' ' + t('common.error')
    return
  }
  saving.value = true
  try {
    if (formId.value) {
      await cloudAccountApi.updateCloudAccount(formId.value, {
        name: form.name,
        provider: form.provider,
        region: form.region,
        auth_type: form.auth_type
      })
    } else {
      await cloudAccountApi.createCloudAccount({
        name: form.name,
        provider: form.provider,
        region: form.region,
        auth_type: form.auth_type
      })
    }
    toast.success(t('cloud_accounts.saved'))
    formModal.value = false
    await fetchAccounts()
  } catch (e) {
    formError.server = e.j?.error || t('cloud_accounts.save_failed')
  } finally {
    saving.value = false
  }
}

function onDelete(row) {
  deleteTarget.value = row
  deleteModal.value = true
}

async function onDeleteConfirm() {
  const row = deleteTarget.value
  if (!row) return
  deleting.value = true
  try {
    await cloudAccountApi.deleteCloudAccount(row.id)
    await fetchAccounts()
  } catch (e) {
    toast.error(e.j?.error || t('cloud_accounts.delete_failed'))
  } finally {
    deleting.value = false
    deleteModal.value = false
    deleteTarget.value = null
  }
}

function onSync(row) {
  syncTarget.value = row
  syncModal.value = true
}

async function onSyncConfirm() {
  const row = syncTarget.value
  if (!row) return
  syncingId.value = row.id
  syncing.value = true
  try {
    await cloudAccountApi.syncCloudAccount(row.id)
    toast.success(t('cloud_accounts.sync_started'))
    await fetchAccounts()
  } catch (e) {
    toast.error(e.j?.error || t('cloud_accounts.sync_failed'))
  } finally {
    syncing.value = false
    syncingId.value = null
    syncModal.value = false
    syncTarget.value = null
  }
}

function onTest(row) {
  testTarget.value = row
  testModal.value = true
}

async function onTestConfirm() {
  const row = testTarget.value
  if (!row) return
  testingId.value = row.id
  testing.value = true
  try {
    await cloudAccountApi.testCloudAccount(row.id)
    toast.success(t('cloud_accounts.test_ok'))
  } catch (e) {
    toast.error(e.j?.error || t('cloud_accounts.test_failed'))
  } finally {
    testing.value = false
    testingId.value = null
    testModal.value = false
    testTarget.value = null
  }
}

function openResources(row) {
  resourcesAccount.value = row
  resources.value = []
  resourcesLoading.value = true
  resourcesDrawer.value = true
  cloudAccountApi.listCloudAccountRegions(row.id).then(({ j }) => {
    resources.value = (j && j.regions) || []
  }).catch(() => {
    resources.value = []
    toast.error(t('cloud_accounts.save_failed'))
  }).finally(() => {
    resourcesLoading.value = false
  })
}

onMounted(fetchAccounts)
</script>

<style scoped>
.toolbar { display: flex; gap: var(--space-2); align-items: center; }
.table-wrap { display: flex; flex-direction: column; min-height: 320px; }
.account-form { display: flex; flex-direction: column; gap: var(--space-3); }
.empty-state { padding: var(--space-6); text-align: center; color: var(--text-tertiary); }
</style>
