<template>
  <div>
    <h2>任务下发</h2>
    <p class="muted">向采集端下发 shell / file / service 任务，支持按状态过滤与取消。</p>

    <div class="card">
      <h3>下发任务</h3>
      <form @submit.prevent="onSubmit">
        <div class="row">
          <div class="field">
            <label>采集端</label>
            <select v-model="form.agentID" required>
              <option value="">（请选择）</option>
              <option v-for="a in agents" :key="a.agentID" :value="a.agentID">
                {{ a.agentID }} ({{ a.hostname }})
              </option>
            </select>
          </div>
          <div class="field">
            <label>类型</label>
            <select v-model="form.type">
              <option value="shell">shell</option>
              <option value="file">file</option>
              <option value="service">service</option>
            </select>
          </div>
        </div>
        <div class="field">
          <label>命令</label>
          <input v-model="form.command" placeholder="如 uptime / systemctl status nginx" style="width: 60%" />
        </div>
        <div class="row">
          <div class="field">
            <label>路径</label>
            <input v-model="form.path" placeholder="（可选）" />
          </div>
          <div class="field">
            <label>内容</label>
            <input v-model="form.content" placeholder="（可选，file 类型用）" />
          </div>
        </div>
        <div class="btnbar">
          <button type="submit" class="primary">下发</button>
        </div>
        <p v-if="msg" :class="['msg', msgOk ? 'ok' : 'err']">{{ msg }}</p>
      </form>
    </div>

    <div class="card">
      <div class="flowbar">
        <div class="field">
          <label>状态过滤</label>
          <select v-model="store.statusFilter" @change="store.fetchTasks()">
            <option value="">全部</option>
            <option value="pending">pending</option>
            <option value="running">running</option>
            <option value="done">done</option>
            <option value="failed">failed</option>
            <option value="canceled">canceled</option>
          </select>
        </div>
        <button @click="store.fetchTasks()">↻ 刷新</button>
      </div>

      <div v-if="store.error" class="poll-err">⚠️ {{ store.error }}</div>

      <DataTable :columns="columns" :rows="store.list" row-key="taskID" empty-text="暂无任务">
        <template #cell-taskID="{ value }"><code>{{ value }}</code></template>
        <template #cell-command="{ value }"><code>{{ value }}</code></template>
        <template #cell-status="{ value }">
          <StatusBadge :status="value" :text="value" />
        </template>
        <template #cell-actions="{ row }">
          <button
            v-if="row.status === 'pending' || row.status === 'running'"
            class="xs outline"
            @click.stop="onCancel(row.taskID)"
          >取消</button>
          <span v-else class="muted">—</span>
        </template>
      </DataTable>
    </div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { useTaskStore } from '@/stores/task'
import { getAgents } from '@/api/device'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'

const store = useTaskStore()
const agents = ref([])
const form = ref({ agentID: '', type: 'shell', command: '', path: '', content: '' })
const msg = ref('')
const msgOk = ref(false)

const columns = [
  { key: 'taskID', title: 'TaskID', slot: 'cell-taskID' },
  { key: 'agentID', title: '采集端' },
  { key: 'type', title: '类型' },
  { key: 'command', title: '命令', slot: 'cell-command' },
  { key: 'status', title: '状态', slot: 'cell-status' },
  { key: 'actions', title: '操作', slot: 'cell-actions', width: '90px' }
]

async function onSubmit() {
  try {
    const r = await store.create({ ...form.value })
    msg.value = `[${r.s}] ${JSON.stringify(r.j)}`
    msgOk.value = r.s < 400
    if (r.s < 400) form.value.command = ''
  } catch (e) {
    msg.value = '下发失败: ' + (e.j?.error || e.message)
    msgOk.value = false
  }
}
async function onCancel(id) {
  try { await store.cancel(id) } catch (_) { /* 错误已由 store 记录 */ }
}

onMounted(async () => {
  try { agents.value = await getAgents() || [] } catch (_) { /* 忽略 */ }
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