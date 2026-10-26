<template>
  <div>
    <h2 data-testid="tasks-title">{{ $t('tasks.title') }}</h2>
    <p class="muted">{{ $t('tasks.subtitle') }}</p>

    <div class="card" v-if="authStore.hasPerm('task:write')">
      <h3 class="card__title" data-testid="task-form-title">{{ $t('tasks.dispatch_form_title') }}</h3>
      <form @submit.prevent="onSubmit">
        <div class="row">
          <div class="field">
            <label>{{ $t('tasks.agent') }}</label>
            <select v-model="form.agentID" required data-testid="task-agent-select">
              <option value="">{{ $t('tasks.please_select') }}</option>
              <option v-for="a in agents" :key="a.agentID" :value="a.agentID">
                {{ a.agentID }} ({{ a.hostname }})
              </option>
            </select>
          </div>
          <div class="field">
            <label>{{ $t('tasks.type') }}</label>
            <select v-model="form.type" data-testid="task-type-select">
              <option value="shell">shell</option>
              <option value="file">file</option>
              <option value="service">service</option>
            </select>
          </div>
        </div>
        <div class="field">
          <label>{{ $t('tasks.command') }}</label>
          <input v-model="form.command" :placeholder="$t('tasks.command_placeholder')" style="width: 60%" data-testid="task-command-input" />
        </div>
        <div class="row">
          <div class="field">
            <label>{{ $t('tasks.path') }}</label>
            <input v-model="form.path" :placeholder="$t('tasks.optional')" />
          </div>
          <div class="field">
            <label>{{ $t('tasks.content') }}</label>
            <input v-model="form.content" :placeholder="$t('tasks.content_placeholder')" />
          </div>
        </div>
        <div class="btnbar">
          <Button native-type="submit" variant="primary" data-testid="task-submit-btn" :disabled="submitting">
            {{ submitting ? $t('common.loading') : $t('tasks.submit') }}
          </Button>
        </div>
        <p v-if="msg" :class="['msg', msgOk ? 'ok' : 'err']">{{ msg }}</p>
      </form>
    </div>
    <div v-else class="card">
      <p class="muted">{{ $t('tasks.no_permission') }}</p>
    </div>

    <div class="card">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('tasks.task_list_title') }}</h3>
        <div class="field">
          <label>{{ $t('tasks.status_filter') }}</label>
          <select v-model="store.statusFilter" @change="store.fetchTasks()" data-testid="task-status-filter">
            <option value="">{{ $t('common.all') }}</option>
            <option value="pending">pending</option>
            <option value="running">running</option>
            <option value="done">done</option>
            <option value="failed">failed</option>
            <option value="cancelled">cancelled</option>
          </select>
        </div>
        <Button variant="ghost" size="sm" @click="store.fetchTasks()">
          <Icon name="refresh" :size="13" /> {{ $t('common.refresh') }}
        </Button>
      </div>

      <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

      <DataTable :columns="columns" :rows="store.list" row-key="taskID" :loading="store.loading" :empty-text="$t('tasks.empty')">
        <template #cell-taskID="{ value }"><code>{{ value }}</code></template>
        <template #cell-command="{ value }"><code>{{ truncateCmd(value) }}</code></template>
        <template #cell-status="{ value }">
          <StatusBadge :status="value" :text="value" />
        </template>
        <template #cell-actions="{ row }">
          <div v-if="(row.status === 'pending' || row.status === 'running') && authStore.hasPerm('task:cancel')" class="row-actions">
            <Button variant="ghost" size="sm" @click.stop="onCancel(row.taskID)" data-testid="task-cancel-btn">
              {{ $t('tasks.cancel') }}
            </Button>
          </div>
          <span v-else class="muted">—</span>
        </template>
      </DataTable>
    </div>

    <!-- 待审批任务段 -->
    <div class="card" v-if="authStore.hasPerm('task:write')">
      <h3 class="card__title" data-testid="task-approval-title">{{ $t('tasks.pending_approval_title') }}</h3>
      <DataTable :columns="approvalColumns" :rows="store.pendingApprovals" row-key="taskID" :empty-text="$t('tasks.empty')">
        <template #cell-taskID="{ value }"><code>{{ value }}</code></template>
        <template #cell-command="{ value }"><code>{{ value }}</code></template>
        <template #cell-status="{ value }">
          <StatusBadge :status="value" :text="value" />
        </template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <Button variant="ghost" size="sm" data-testid="task-approve-btn" @click.stop="onApprove(row.taskID)">
              {{ $t('tasks.approve') }}
            </Button>
            <Button variant="danger" size="sm" data-testid="task-reject-btn" @click.stop="onReject(row.taskID)">
              {{ $t('tasks.reject') }}
            </Button>
          </div>
        </template>
      </DataTable>
    </div>
  </div>

  <!-- 取消确认（替代 confirm） -->
  <ConfirmModal
    v-model="cancelConfirm.show"
    data-testid="task-cancel-confirm-modal"
    :title="$t('tasks.cancel')"
    :message="$t('tasks.confirm_cancel')"
    @confirm="onCancelConfirm"
  />
</template>

<script setup>
import { onMounted, onUnmounted, reactive, ref } from 'vue'
import { useTaskStore } from '@/stores/task'
import { useAuthStore } from '@/stores/auth'
import { getAgents } from '@/api/device'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import Icon from '@/components/Icon.vue'
import { Button } from '@/components/ui'
import ConfirmModal from '@/components/ConfirmModal.vue'

const store = useTaskStore()
const authStore = useAuthStore()
const agents = ref([])
const form = ref({ agentID: '', type: 'shell', command: '', path: '', content: '' })
const msg = ref('')
const msgOk = ref(false)
const submitting = ref(false)

// 取消确认弹窗（替代 confirm）
const cancelConfirm = reactive({ show: false, id: null })

// SSE 事件刷新节流：2s 内多个 task-status 事件只 refetch 一次，
// 最后一次事件必须执行（trailing），避免批量回执时全量 refetch 轰炸。
const SSE_THROTTLE_MS = 2000
let sseLastFetchAt = 0
let sseTimer = null
function scheduleSSEFetch() {
  const now = Date.now()
  const elapsed = now - sseLastFetchAt
  if (elapsed >= SSE_THROTTLE_MS) {
    // 距上次刷新已超过节流窗口：立即执行
    sseLastFetchAt = now
    clearTimeout(sseTimer)
    sseTimer = null
    store.fetchTasks()
  } else if (!sseTimer) {
    // 窗口内首个事件：安排 trailing 执行，保证最后一次事件必然触发刷新
    sseTimer = setTimeout(() => {
      sseTimer = null
      sseLastFetchAt = Date.now()
      store.fetchTasks()
    }, SSE_THROTTLE_MS - elapsed)
  }
}

function truncateCmd(v) {
  if (!v) return ''
  const s = String(v)
  return s.length > 80 ? s.slice(0, 80) + '…' : s
}

const columns = [
  { key: 'taskID', title: t('tasks.col_task_id'), slot: 'cell-taskID' },
  { key: 'agentID', title: t('tasks.col_agent') },
  { key: 'type', title: t('tasks.col_type') },
  { key: 'command', title: t('tasks.col_command'), slot: 'cell-command' },
  { key: 'status', title: t('tasks.col_status'), slot: 'cell-status' },
  { key: 'actions', title: t('tasks.col_actions'), slot: 'cell-actions', width: '90px' }
]

const approvalColumns = [
  { key: 'taskID', title: t('tasks.col_task_id'), slot: 'cell-taskID' },
  { key: 'agentID', title: t('tasks.col_agent') },
  { key: 'type', title: t('tasks.col_type') },
  { key: 'command', title: t('tasks.col_command'), slot: 'cell-command' },
  { key: 'status', title: t('tasks.col_status'), slot: 'cell-status' },
  { key: 'actions', title: t('tasks.col_actions'), slot: 'cell-actions', width: '120px' }
]

async function onSubmit() {
  if (submitting.value) return
  submitting.value = true
  try {
    const r = await store.create({ ...form.value })
    msg.value = `[${r.s}] ${JSON.stringify(r.j)}`
    msgOk.value = r.s < 400
    if (r.s < 400) form.value.command = ''
  } catch (e) {
    msg.value = t('tasks.dispatch_failed') + (e.j?.error || e.message)
    msgOk.value = false
  } finally {
    submitting.value = false
  }
}
function onCancel(id) {
  cancelConfirm.id = id
  cancelConfirm.show = true
}
async function onCancelConfirm() {
  const id = cancelConfirm.id
  if (!id) return
  try { await store.cancel(id) } catch (e) { console.error('cancel task failed:', e) }
}
async function onApprove(id) {
  try { await store.approve(id) } catch (e) { console.error('approve task failed:', e) }
}
async function onReject(id) {
  try { await store.reject(id) } catch (e) { console.error('reject task failed:', e) }
}

onMounted(async () => {
  try { agents.value = await getAgents() || [] } catch (e) { console.error('fetch agents failed:', e) }
  store.fetchTasks()
  store.fetchPendingApprovals()
  // SSE 实时推送：任务状态变更（创建/领取/取消/回执）到达即刷新列表（App.vue 事件总线）。
  window.addEventListener('opsmesh:task-status', onTaskStatus)
})
onUnmounted(() => {
  window.removeEventListener('opsmesh:task-status', onTaskStatus)
  clearTimeout(sseTimer)
  sseTimer = null
})

function onTaskStatus() {
  scheduleSSEFetch()
  store.fetchPendingApprovals()
}
</script>

<style scoped>
.card {
  background: var(--color-surface-1, var(--surface-l1, #ffffff));
  border: 1px solid var(--color-hairline, var(--border-primary, #d3dbe8));
  border-radius: var(--radius-md, 8px);
  padding: 16px 18px;
  margin-bottom: 16px;
}
.card__title { margin: 0 0 12px 0; font-size: 13px; font-weight: 600; color: var(--color-ink-muted, #6b7280); }
.row { display: flex; gap: 20px; flex-wrap: wrap; align-items: flex-end; }
.field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 8px; min-width: 220px; }
.field label { display: block; margin: 0; font-size: 11px; font-weight: 500; color: var(--color-ink-muted, #6b7280); }
.flowbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 12px; }
.row-actions { display: inline-flex; gap: 6px; align-items: center; }
.btnbar { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; margin-top: 12px; }
.muted { color: var(--color-ink-muted, #6b7280); font-size: 12.5px; }
.poll-err {
  padding: 8px 12px; margin: 6px 0 12px; border-radius: var(--radius-sm);
  background: var(--color-error-soft, #fef2f2); border: 1px solid var(--color-error-light, #fecaca);
  color: var(--color-error, #ef4444); font-size: 12.5px; font-weight: 500;
}
.msg { margin-top: 8px; font-size: 12.5px; }
.msg.ok { color: var(--color-success, #059669); }
.msg.err { color: var(--color-error, #ef4444); }

/* Dark mode */
[data-theme="dark"] .card {
  background: var(--color-surface-2, #1e2430);
  border-color: var(--color-hairline, #23252a);
}
</style>
