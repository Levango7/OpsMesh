<template>
  <div>
    <h2>{{ $t('tasks.title') }}</h2>
    <p class="muted">{{ $t('tasks.subtitle') }}</p>

    <div class="card" v-if="authStore.hasPerm('task:write')">
      <h3>{{ $t('tasks.dispatch_form_title') }}</h3>
      <form @submit.prevent="onSubmit">
        <div class="row">
          <div class="field">
            <label>{{ $t('tasks.agent') }}</label>
            <select v-model="form.agentID" required>
              <option value="">{{ $t('tasks.please_select') }}</option>
              <option v-for="a in agents" :key="a.agentID" :value="a.agentID">
                {{ a.agentID }} ({{ a.hostname }})
              </option>
            </select>
          </div>
          <div class="field">
            <label>{{ $t('tasks.type') }}</label>
            <select v-model="form.type">
              <option value="shell">shell</option>
              <option value="file">file</option>
              <option value="service">service</option>
            </select>
          </div>
        </div>
        <div class="field">
          <label>{{ $t('tasks.command') }}</label>
          <input v-model="form.command" :placeholder="$t('tasks.command_placeholder')" style="width: 60%" />
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
          <button type="submit" class="primary">{{ $t('tasks.submit') }}</button>
        </div>
        <p v-if="msg" :class="['msg', msgOk ? 'ok' : 'err']">{{ msg }}</p>
      </form>
    </div>
    <div v-else class="card">
      <p class="muted">{{ $t('tasks.no_permission') }}</p>
    </div>

    <div class="card">
      <div class="flowbar">
        <div class="field">
          <label>{{ $t('tasks.status_filter') }}</label>
          <select v-model="store.statusFilter" @change="store.fetchTasks()">
            <option value="">{{ $t('common.all') }}</option>
            <option value="pending">pending</option>
            <option value="running">running</option>
            <option value="done">done</option>
            <option value="failed">failed</option>
            <option value="canceled">canceled</option>
          </select>
        </div>
        <button @click="store.fetchTasks()">↻ {{ $t('common.refresh') }}</button>
      </div>

      <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

      <DataTable :columns="columns" :rows="store.list" row-key="taskID" :empty-text="$t('tasks.empty')">
        <template #cell-taskID="{ value }"><code>{{ value }}</code></template>
        <template #cell-command="{ value }"><code>{{ value }}</code></template>
        <template #cell-status="{ value }">
          <StatusBadge :status="value" :text="value" />
        </template>
        <template #cell-actions="{ row }">
          <button
            v-if="(row.status === 'pending' || row.status === 'running') && authStore.hasPerm('task:cancel')"
            class="xs outline"
            @click.stop="onCancel(row.taskID)"
          >{{ $t('tasks.cancel') }}</button>
          <span v-else class="muted">—</span>
        </template>
      </DataTable>
    </div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { useTaskStore } from '@/stores/task'
import { useAuthStore } from '@/stores/auth'
import { getAgents } from '@/api/device'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import Icon from '@/components/Icon.vue'

const store = useTaskStore()
const authStore = useAuthStore()
const agents = ref([])
const form = ref({ agentID: '', type: 'shell', command: '', path: '', content: '' })
const msg = ref('')
const msgOk = ref(false)

const columns = [
  { key: 'taskID', title: 'TaskID', slot: 'cell-taskID' },
  { key: 'agentID', title: t('tasks.col_agent') },
  { key: 'type', title: t('tasks.col_type') },
  { key: 'command', title: t('tasks.col_command'), slot: 'cell-command' },
  { key: 'status', title: t('tasks.col_status'), slot: 'cell-status' },
  { key: 'actions', title: t('tasks.col_actions'), slot: 'cell-actions', width: '90px' }
]

async function onSubmit() {
  try {
    const r = await store.create({ ...form.value })
    msg.value = `[${r.s}] ${JSON.stringify(r.j)}`
    msgOk.value = r.s < 400
    if (r.s < 400) form.value.command = ''
  } catch (e) {
    msg.value = t('tasks.dispatch_failed') + (e.j?.error || e.message)
    msgOk.value = false
  }
}
async function onCancel(id) {
  try { await store.cancel(id) } catch (e) { console.error('cancel task failed:', e) }
}

onMounted(async () => {
  try { agents.value = await getAgents() || [] } catch (e) { console.error('fetch agents failed:', e) }
  store.fetchTasks()
})
</script>

<style scoped>
.field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 8px; min-width: 220px; }
.field label { margin: 0; }
.flowbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 10px; }
.poll-err {
  padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}
</style>
