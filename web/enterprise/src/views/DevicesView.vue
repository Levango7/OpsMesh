<template>
  <div>
    <h2>设备纳管</h2>
    <p class="muted">按网段分组的设备清单。点击行打开详情抽屉，可对 discovered 设备推送 Agent 纳管。</p>

    <div v-if="store.error" class="poll-err">⚠️ {{ store.error }}</div>

    <div v-if="store.loading && !segments.length" class="muted">加载中…</div>
    <div v-else-if="!segments.length" class="muted">
      暂无纳管设备。设备接入网段后会被自动发现并纳管。
    </div>

    <div v-for="seg in segments" :key="seg.name" class="seg-block">
      <h3>网段 {{ seg.name }}（{{ seg.devices.length }} 台设备）</h3>
      <DataTable
        :columns="columns"
        :rows="seg.devices"
        row-key="deviceID"
        :clickable="true"
        :row-class="rowClass"
        empty-text="该网段暂无设备"
        @row-click="openDevice"
      >
        <template #cell-deviceID="{ value }"><code>{{ value }}</code></template>
        <template #cell-lastResult="{ value }">
          <StatusBadge v-if="value === 'failed'" status="failed" text="failed" />
          <StatusBadge v-else-if="value === 'success'" status="success" text="ok" />
          <span v-else class="muted">—</span>
        </template>
      </DataTable>
    </div>

    <DetailDrawer :open="!!store.current" :title="drawerTitle" @close="store.closeDrawer()">
      <div v-if="store.current">
        <p>IP: {{ dev.ip }} ｜ 采集端: {{ dev.agentID }} ｜ 租户: {{ dev.tenantID }}</p>
        <p>状态: {{ dev.state }} ｜ 任务态: {{ dev.taskState }}</p>
        <p v-if="dev.lastResult" :class="['msg', dev.lastResult === 'failed' ? 'err' : 'ok']">
          LastResult: {{ dev.lastResult }} @ {{ fmtTime(dev.lastResultAt) }}
        </p>
        <button v-if="dev.state === 'discovered'" class="primary" @click="provision">
          推送 Agent 纳管
        </button>

        <h4>任务</h4>
        <DataTable :columns="taskCols" :rows="store.current.tasks || []" row-key="taskID" empty-text="无任务">
          <template #cell-taskID="{ value }"><code>{{ value }}</code></template>
        </DataTable>

        <h4>最近结果</h4>
        <DataTable :columns="resultCols" :rows="(store.current.results || []).slice(0, 5)" empty-text="无结果">
          <template #cell-taskID="{ value }"><code>{{ value }}</code></template>
          <template #cell-stdout="{ value }"><code>{{ value }}</code></template>
        </DataTable>
      </div>
    </DetailDrawer>
  </div>
</template>

<script setup>
import { computed, onMounted } from 'vue'
import { useDeviceStore } from '@/stores/device'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'

const store = useDeviceStore()

const columns = [
  { key: 'deviceID', title: 'DeviceID', slot: 'cell-deviceID' },
  { key: 'ip', title: 'IP' },
  { key: 'agentID', title: '采集端' },
  { key: 'state', title: '状态' },
  { key: 'taskState', title: '任务态' },
  { key: 'lastResult', title: 'LastResult', slot: 'cell-lastResult' }
]
const taskCols = [
  { key: 'taskID', title: 'ID', slot: 'cell-taskID' },
  { key: 'type', title: '类型' },
  { key: 'status', title: '状态' }
]
const resultCols = [
  { key: 'taskID', title: '任务', slot: 'cell-taskID' },
  { key: 'exitCode', title: '退出码' },
  { key: 'stdout', title: '输出', slot: 'cell-stdout' }
]

const segments = computed(() =>
  Object.entries(store.segments).map(([name, devices]) => ({ name, devices: devices || [] }))
)
const dev = computed(() => (store.current && store.current.device) || {})
const drawerTitle = computed(() => '设备 ' + (dev.value.deviceID || ''))

function rowClass(row) {
  return row.lastResult === 'failed' ? 'fail-row' : ''
}
function openDevice(row) { store.openDevice(row.deviceID) }
async function provision() {
  if (!dev.value.deviceID) return
  await store.provision(dev.value.deviceID)
}
function fmtTime(s) {
  if (!s) return ''
  const d = new Date(s)
  return isNaN(d.getTime()) ? s : d.toLocaleString('zh-CN', { hour12: false })
}

onMounted(() => { if (!store.total) store.fetchDevices() })
</script>

<style scoped>
.seg-block { margin-bottom: 18px; }
.poll-err {
  padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}
:deep(.fail-row) { background: var(--fail-soft) !important; }
:deep(.fail-row:hover) { background: var(--fail-bg) !important; }
</style>