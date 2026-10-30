<template>
  <PageShell
    :title="$t('backups.title')"
    :desc="$t('backups.subtitle')"
    testid="backups"
    :error="loadErr"
  >
    <div class="btnbar btnbar--bottom">
      <Button variant="ghost" size="md" @click="openCreate">
        <Icon name="add" :size="14" />
        {{ $t('backups.create') }}
      </Button>
      <Button variant="ghost" size="md" @click="fetchBackups">
        <Icon name="refresh" :size="14" />
        {{ $t('common.refresh') }}
      </Button>
    </div>

    <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
    <div v-else>
      <DataTable :columns="columns" :rows="backups" row-key="id" :empty-text="$t('backups.empty')">
        <template #cell-id="{ value }"><code>{{ value }}</code></template>
        <template #cell-type="{ value }">
          <span class="tag">{{ $t('backups.type_' + value) }}</span>
        </template>
        <template #cell-status="{ value }">
          <span class="tag" :class="'st-' + value">{{ $t('backups.status_' + value) }}</span>
        </template>
        <template #cell-size="{ value }">{{ fmtSize(value) }}</template>
        <template #cell-createdAt="{ value }">{{ fmtTime(value) }}</template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <Button variant="ghost" size="sm" :title="$t('backups.restore')" @click="onRestore(row)"><Icon name="success" :size="13" /></Button>
            <Button variant="danger" size="sm" @click="onDelete(row)"><Icon name="delete" :size="13" /></Button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- 创建备份抽屉（选择备份类型） -->
    <DetailDrawer :open="!!createForm" :title="$t('backups.create')" @close="createForm = null">
      <form v-if="createForm" class="backup-form" @submit.prevent="onCreateConfirm">
        <div class="field">
          <label>{{ $t('backups.new_type') }}</label>
          <select v-model="createForm.type">
            <option value="full">{{ $t('backups.type_full') }}</option>
            <option value="config">{{ $t('backups.type_config') }}</option>
            <option value="devices">{{ $t('backups.type_devices') }}</option>
            <option value="tasks">{{ $t('backups.type_tasks') }}</option>
          </select>
        </div>
        <p class="muted">{{ $t('backups.create_hint') }}</p>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar btnbar--bottom">
          <Button variant="primary" size="md" native-type="submit"><Icon name="success" :size="14" /> {{ $t('backups.submit') }}</Button>
          <Button variant="ghost" size="md" native-type="button" @click="createForm = null">{{ $t('backups.cancel') }}</Button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 恢复确认（高危操作，替代 confirm） -->
    <ConfirmModal
      v-model="restoreConfirm.show"
      data-testid="backups-restore-confirm-modal"
      :title="$t('backups.restore')"
      :message="$t('backups.confirm_restore')"
      :confirm-text="$t('common.confirm')"
      @confirm="onRestoreConfirm"
    />
    <!-- 删除确认（替代 confirm） -->
    <ConfirmModal
      v-model="deleteConfirm.show"
      data-testid="backups-delete-confirm-modal"
      :title="$t('backups.delete')"
      :message="$t('backups.confirm_delete')"
      @confirm="onDeleteConfirm"
    />
    <!-- 错误/结果提示（替代 alert） -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="backups-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </PageShell>
</template>

<script setup>
import PageShell from '@/components/PageShell.vue'

// 灾备备份页 — 创建（异步归档）+ 列表 + 恢复（高危二次确认）+ 删除
// create 触发后端异步归档（POST 返回 creating 态记录），随后刷新列表观察状态推进
import { ref, reactive, onMounted } from 'vue'
import * as backupApi from '@/api/backup'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import { fmtTime } from '@/composables/useFormatTime'
import Button from "@/components/ui/Button.vue"

const backups = ref([])
const loading = ref(false)
const createForm = ref(null)
const formError = ref('')
const loadErr = ref('')

// 恢复确认弹窗（高危操作）
const restoreConfirm = reactive({ show: false, row: null })
// 删除确认弹窗
const deleteConfirm = reactive({ show: false, row: null })
// 错误提示弹窗
const errorConfirm = reactive({ show: false, message: '' })

const columns = [
  { key: 'id', title: 'ID', slot: 'cell-id' },
  { key: 'type', title: t('backups.type'), slot: 'cell-type', width: '100px' },
  { key: 'status', title: t('backups.status'), slot: 'cell-status', width: '100px' },
  { key: 'size', title: t('backups.size'), slot: 'cell-size', width: '100px' },
  { key: 'createdAt', title: t('backups.created_at'), slot: 'cell-createdAt' },
  { key: 'actions', title: t('backups.actions'), slot: 'cell-actions', width: '110px' }
]

// 字节数转人类可读（备份 Size 为字节数）
function fmtSize(v) {
  const n = Number(v) || 0
  if (n <= 0) return '—'
  if (n < 1024) return n + ' B'
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB'
  if (n < 1024 * 1024 * 1024) return (n / 1024 / 1024).toFixed(1) + ' MB'
  return (n / 1024 / 1024 / 1024).toFixed(1) + ' GB'
}

async function fetchBackups() {
  loading.value = true
  loadErr.value = ''
  try {
    const r = await backupApi.listBackups()
    backups.value = (r && r.backups) || []
  } catch (e) {
    backups.value = []
    // 此前静默吞错（只见空表）——统一骨架批次顺带补上可见的加载错误
    loadErr.value = e.j?.error || t('common.load_failed')
  } finally {
    loading.value = false
  }
}

function openCreate() {
  formError.value = ''
  createForm.value = { type: 'full' }
}

async function onCreateConfirm() {
  formError.value = ''
  try {
    // 后端异步归档：返回 creating 态记录，由后台 goroutine 推进 completed
    await backupApi.createBackup(createForm.value.type)
    toast.success(t('backups.create_started'))
    createForm.value = null
    await fetchBackups()
  } catch (e) {
    formError.value = e.j?.error || t('backups.create_failed')
  }
}

function onRestore(row) {
  // 高危操作必须二次确认，不直接执行
  restoreConfirm.row = row
  restoreConfirm.show = true
}

async function onRestoreConfirm() {
  const row = restoreConfirm.row
  if (!row) return
  try {
    const { j } = await backupApi.restoreBackup(row.id)
    // 展示恢复计数摘要（后端返回各类恢复条数）
    const counts = j?.restored ? JSON.stringify(j.restored) : ''
    errorConfirm.message = t('backups.restore_done') + (counts ? ` (${counts})` : '')
    errorConfirm.show = true
    await fetchBackups()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('backups.restore_failed')
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
    await backupApi.deleteBackup(row.id)
    await fetchBackups()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('backups.delete_failed')
    errorConfirm.show = true
  }
}

onMounted(fetchBackups)
</script>

<style scoped>
.tag {
  display: inline-block; padding: 2px 8px; border-radius: 999px;
  font-size: 12px; background: var(--accent-soft); color: var(--accent);
}
.tag.st-completed { background: var(--ok-bg); color: var(--ok); }
.tag.st-creating { background: var(--info-bg); color: var(--info); }

.backup-form { display: flex; flex-direction: column; gap: 12px; }
</style>
