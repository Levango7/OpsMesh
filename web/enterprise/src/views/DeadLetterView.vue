<template>
  <PageShell
    :title="$t('dead_letter.title')"
    :desc="$t('dead_letter.subtitle')"
    testid="dead-letter"
    :error="store.error"
  >
    <div class="card">
      <div class="flowbar">
        <Button variant="ghost" size="sm" data-testid="dead-letter-refresh-btn" @click="store.fetchDeadLetterTasks()">
          <Icon name="refresh" :size="13" /> {{ $t('common.refresh') }}
        </Button>
      </div>
      <div v-if="!canOperate" class="muted" data-testid="write-denied-hint">{{ $t('common.write_denied') }}</div>

      <DataTable
        :columns="columns"
        :rows="store.list"
        row-key="taskID"
        :loading="store.loading"
        :empty-text="$t('dead_letter.empty')"
      >
        <template #cell-taskID="{ value }"><code>{{ value }}</code></template>
        <template #cell-agentID="{ value }">{{ value }}</template>
        <template #cell-command="{ value, row }"><code>{{ truncate(value ?? row.command) }}</code></template>
        <template #cell-retry="{ row }">{{ row.retryCount ?? 0 }}/{{ row.maxRetries ?? 0 }}</template>
        <template #cell-status="{ value }">
          <StatusBadge :status="value" :text="value" />
        </template>
        <template #cell-failureReason="{ row }">{{ failureReason(row) }}</template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <Button variant="ghost" size="sm" data-testid="dead-letter-retry-btn" :disabled="!canOperate"
                    :title="canOperate ? '' : $t('common.write_denied')" @click.stop="onRetry(row.taskID)">
              {{ $t('dead_letter.retry') }}
            </Button>
            <Button variant="danger" size="sm" data-testid="dead-letter-purge-btn" :disabled="!canOperate"
                    :title="canOperate ? '' : $t('common.write_denied')" @click.stop="askPurge(row.taskID)">
              {{ $t('dead_letter.purge') }}
            </Button>
          </div>
        </template>
      </DataTable>
    </div>

    <ConfirmModal
      v-model="purgeConfirm.show"
      data-testid="dead-letter-purge-confirm-modal"
      :title="$t('dead_letter.purge')"
      :message="$t('dead_letter.purge_confirm')"
      @confirm="onPurgeConfirm"
    />
  </PageShell>
</template>

<script setup>
import PageShell from '@/components/PageShell.vue'

import { computed, onMounted, reactive } from 'vue'
import { useDeadLetterStore } from '@/stores/dead-letter'
import { useAuthStore } from '@/stores/auth'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import { Button } from '@/components/ui'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'

const store = useDeadLetterStore()
const auth = useAuthStore()

// 死信处置门（2026-11-06 五维排查批 2）：后端 retry=task:write、purge=task:cancel。
// 只读者点 purge 是**永久删除**（不可逆），此前按钮既不禁用也不确认。
const canOperate = computed(() => auth.hasPerm('task:write') || auth.hasPerm('task:cancel'))

// 清除确认（此前一键直删，无任何拦截）
const purgeConfirm = reactive({ show: false, id: null })

const columns = [
  { key: 'taskID', title: t('dead_letter.col_task_id'), slot: 'cell-taskID' },
  { key: 'agentID', title: t('dead_letter.col_agent'), slot: 'cell-agentID' },
  { key: 'command', title: t('dead_letter.col_command'), slot: 'cell-command' },
  { key: 'retry', title: t('dead_letter.col_retry'), slot: 'cell-retry', width: '90px' },
  { key: 'status', title: t('dead_letter.col_status'), slot: 'cell-status' },
  { key: 'failureReason', title: t('dead_letter.col_failure_reason'), slot: 'cell-failureReason' },
  { key: 'actions', title: t('dead_letter.col_actions'), slot: 'cell-actions', width: '120px' }
]

function truncate(v) {
  const s = String(v ?? '')
  return s.length > 80 ? s.slice(0, 80) + '…' : s
}

function failureReason(row) {
  return row?.failureReason || row?.reason || row?.error || row?.stderr || row?.lastError || '—'
}

async function onRetry(id) {
  try { await store.retry(id); toast.success(t('dead_letter.retry_ok')) }
  catch (e) { store.error = (e && e.j && e.j.error) || t('dead_letter.retry_failed') }
}
function askPurge(id) {
  purgeConfirm.id = id
  purgeConfirm.show = true
}
async function onPurgeConfirm() {
  const id = purgeConfirm.id
  purgeConfirm.show = false
  try { await store.purge(id); toast.success(t('dead_letter.purge_ok')) }
  catch (e) { store.error = (e && e.j && e.j.error) || t('dead_letter.purge_failed') }
}

onMounted(() => {
  store.fetchDeadLetterTasks()
})
</script>
