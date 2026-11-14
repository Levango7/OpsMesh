<template>
  <PageShell
    :title="$t('schedules.title')"
    :desc="$t('schedules.subtitle')"
    testid="schedules"
    :error="loadErr"
  >
    <div class="btnbar btnbar--bottom">
      <Button variant="primary" size="md" @click="openCreate" data-testid="schedules-opencreate-btn" :disabled="!canWrite || saving" :title="canWrite ? '' : $t('common.write_denied')">
        <Icon name="add" :size="14" />
        {{ $t('schedules.add') }}
      </Button>
      <Button variant="ghost" size="md" @click="fetchSchedules">
        <Icon name="refresh" :size="14" />
        {{ $t('common.refresh') }}
      </Button>
    </div>

    <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
    <div v-else>
      <div v-if="!canWrite" class="muted" data-testid="write-denied-hint">{{ $t('common.write_denied') }}</div>
      <DataTable :loading="loading" :columns="columns" :rows="schedules" row-key="id" :empty-text="$t('schedules.empty')">
        <template #cell-status="{ row }">
          <span class="status-pill" :class="row.status === 'active' ? 'ok' : 'warn'">{{ statusText(row.status) }}</span>
        </template>
        <template #cell-cronExpr="{ value }"><code>{{ value }}</code></template>
        <template #cell-lastRunAt="{ value }">{{ fmtTime(value) }}</template>
        <template #cell-nextRunAt="{ value }">{{ fmtTime(value) }}</template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <Button
              variant="ghost" size="sm" v-if="row.status === 'active'"
              :title="canWrite ? ($t('schedules.pause')) : $t('common.write_denied')"
              @click="onToggle(row, 'pause')"
             data-testid="schedules-pause-btn" :disabled="!canWrite"><Icon name="mute" :size="13" /></Button>
            <Button
              variant="ghost" size="sm" v-else

              :title="canWrite ? ($t('schedules.resume')) : $t('common.write_denied')"
              @click="onToggle(row, 'resume')"
             data-testid="schedules-resume-btn" :disabled="!canWrite"><Icon name="success" :size="13" /></Button>
            <Button variant="ghost" size="sm" @click="openEdit(row)" data-testid="schedules-openedit-btn" :disabled="!canWrite" :title="canWrite ? '' : $t('common.write_denied')"><Icon name="edit" :size="13" /></Button>
            <Button variant="danger" size="sm" @click="onDelete(row)" data-testid="schedules-ondelete-btn" :disabled="!canWrite" :title="canWrite ? '' : $t('common.write_denied')"><Icon name="delete" :size="13" /></Button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- 新增/编辑抽屉 -->
    <DetailDrawer :open="!!form" :title="form && form.id ? $t('schedules.edit') : $t('schedules.add')" @close="form = null">
      <form v-if="form" class="schedule-form" @submit.prevent="onSave">
        <div class="field">
          <label>{{ $t('schedules.new_name') }}</label>
          <input v-model.trim="form.name" type="text" required />
        </div>
        <div class="field">
          <label>{{ $t('schedules.new_task_id') }}</label>
          <input v-model.trim="form.taskID" type="text" :disabled="!!form.id" required />
        </div>
        <div class="field">
          <label>{{ $t('schedules.new_cron') }}</label>
          <input v-model.trim="form.cronExpr" type="text" placeholder="*/5 * * * *" required />
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar btnbar--bottom">
          <Button variant="primary" size="md" native-type="submit" data-testid="schedules-onsave-btn" :disabled="!canWrite" :title="canWrite ? '' : $t('common.write_denied')"><Icon name="success" :size="14" /> {{ $t('schedules.save') }}</Button>
          <Button variant="ghost" size="md" native-type="button" @click="form = null">{{ $t('schedules.cancel') }}</Button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 删除确认（替代 confirm） -->
    <ConfirmModal
      v-model="deleteConfirm.show"
      data-testid="schedules-delete-confirm-modal"
      :title="$t('schedules.delete')"
      :message="$t('schedules.confirm_delete')"
      @confirm="onDeleteConfirm"
    />
    <!-- 错误提示（替代 alert） -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="schedules-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </PageShell>
</template>

<script setup>
import PageShell from '@/components/PageShell.vue'
import { useAuthStore } from '@/stores/auth'

// 定时任务管理页 — cron 调度列表 + 新增/编辑/删除 + 暂停/恢复
import { ref, computed, reactive, onMounted } from 'vue'
import {
  getSchedules, createSchedule, updateSchedule, deleteSchedule,
  pauseSchedule, resumeSchedule
} from '@/api/schedule'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import { fmtTime } from '@/composables/useFormatTime'
import Button from "@/components/ui/Button.vue"

// 写操作门（按钮级门范式，2026-11-06 批量批）：后端该域 handler 一律 requirePermission('schedule:write')
// ——只读者进得来、点写按钮必 403。这里显式禁用 + 说明，不再让用户撞墙。
const auth = useAuthStore()
const canWrite = computed(() => auth.hasPerm('schedule:write'))

const schedules = ref([])
const loading = ref(false)
const saving = ref(false)
const form = ref(null)
const formError = ref('')
const loadErr = ref('')

// 删除确认弹窗（替代 confirm）
const deleteConfirm = reactive({ show: false, row: null })
// 错误提示弹窗（替代 alert）
const errorConfirm = reactive({ show: false, message: '' })

const columns = computed(() => [
  { key: 'name', title: t('schedules.name') },
  { key: 'taskID', title: t('schedules.task_id') },
  { key: 'cronExpr', title: t('schedules.cron'), slot: 'cell-cronExpr' },
  { key: 'status', title: t('schedules.status'), slot: 'cell-status' },
  { key: 'lastRunAt', title: t('schedules.last_run'), slot: 'cell-lastRunAt' },
  { key: 'nextRunAt', title: t('schedules.next_run'), slot: 'cell-nextRunAt' },
  { key: 'actions', title: t('schedules.actions'), slot: 'cell-actions', width: '120px' }
])

function statusText(s) {
  if (s === 'active') return t('schedules.status_active')
  if (s === 'paused') return t('schedules.status_paused')
  return s || '—'
}

async function fetchSchedules() {
  loading.value = true
  loadErr.value = ''
  try {
    const r = await getSchedules()
    schedules.value = (r && r.schedules) || []
  } catch (e) {
    schedules.value = []
    // 此前静默吞错（只见空表）——统一骨架批次补上可见的加载错误
    loadErr.value = e.j?.error || t('common.load_failed')
  } finally {
    loading.value = false
  }
}

function openCreate() {
  formError.value = ''
  form.value = { id: null, name: '', taskID: '', cronExpr: '' }
}

function openEdit(row) {
  formError.value = ''
  form.value = { id: row.id, name: row.name || '', taskID: row.taskID || '', cronExpr: row.cronExpr || '' }
}

async function onSave() {
  if (saving.value) return
  saving.value = true
  formError.value = ''
  try {
    if (form.value.id) {
      await updateSchedule(form.value.id, { name: form.value.name, cronExpr: form.value.cronExpr })
    } else {
      await createSchedule({
        name: form.value.name,
        taskID: form.value.taskID,
        cronExpr: form.value.cronExpr
      })
    }
    form.value = null
    toast.success(t('schedules.saved'))
    await fetchSchedules()
  } catch (e) {
    formError.value = e.j?.error || t('schedules.save_failed')
  } finally {
    saving.value = false
  }
}

async function onToggle(row, action) {
  try {
    if (action === 'pause') await pauseSchedule(row.id)
    else await resumeSchedule(row.id)
    toast.success(action === 'pause' ? t('schedules.paused') : t('schedules.resumed'))
    await fetchSchedules()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('schedules.toggle_failed')
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
    await deleteSchedule(row.id)
    toast.success(t('schedules.deleted'))
    await fetchSchedules()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('schedules.delete_failed')
    errorConfirm.show = true
  }
}

onMounted(fetchSchedules)
</script>

<style scoped>

.schedule-form { display: flex; flex-direction: column; gap: 12px; }
</style>
