<template>
  <div>
    <h2 data-testid="dead-letter-title">{{ $t('dead_letter.title') }}</h2>
    <p class="muted">{{ $t('dead_letter.subtitle') }}</p>

    <div class="card">
      <div class="flowbar">
        <Button variant="ghost" size="sm" data-testid="dead-letter-refresh-btn" @click="store.fetchDeadLetterTasks()">
          <Icon name="refresh" :size="13" /> {{ $t('common.refresh') }}
        </Button>
      </div>

      <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

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
            <Button variant="ghost" size="sm" data-testid="dead-letter-retry-btn" @click.stop="onRetry(row.taskID)">
              {{ $t('dead_letter.retry') }}
            </Button>
            <Button variant="danger" size="sm" data-testid="dead-letter-purge-btn" @click.stop="onPurge(row.taskID)">
              {{ $t('dead_letter.purge') }}
            </Button>
          </div>
        </template>
      </DataTable>
    </div>
  </div>
</template>

<script setup>
import { onMounted } from 'vue'
import { useDeadLetterStore } from '@/stores/dead-letter'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import Icon from '@/components/Icon.vue'
import { Button } from '@/components/ui'
import { t } from '@/i18n'

const store = useDeadLetterStore()

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
  try { await store.retry(id) } catch (e) { console.error('retry dead letter failed:', e) }
}
async function onPurge(id) {
  try { await store.purge(id) } catch (e) { console.error('purge dead letter failed:', e) }
}

onMounted(() => {
  store.fetchDeadLetterTasks()
})
</script>

<style scoped>
.card {
  background: var(--color-surface-1, var(--surface-l1, #ffffff));
  border: 1px solid var(--color-hairline, var(--border-primary, #d3dbe8));
  border-radius: var(--radius-md, 8px);
  padding: 16px 18px;
  margin-bottom: 16px;
}
.flowbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 10px; }
.poll-err { padding: 8px 12px; margin: 6px 0 12px; border-radius: var(--radius-sm); background: var(--fail-soft); border: 1px solid var(--fail-bg); color: var(--fail); font-size: 12.5px; font-weight: 500; }
.row-actions { display: flex; gap: 4px; }
.muted { color: var(--text-3, #6b7280); font-size: 12.5px; }
</style>
EOF