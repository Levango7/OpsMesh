<template>
  <div>
    <h2>{{ $t('devices.title') }}</h2>
    <p class="muted">{{ $t('devices.subtitle') }}</p>

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
        <template #cell-state="{ value }">
          <StatusBadge :status="value || 'info'" :text="value || '—'" />
        </template>
        <template #cell-lastResult="{ value }">
          <StatusBadge v-if="value === 'failed'" status="failed" text="failed" />
          <StatusBadge v-else-if="value === 'success'" status="success" text="ok" />
          <span v-else class="muted">—</span>
        </template>
        <template #cell-actions="{ row }">
          <div class="row-actions" @click.stop>
            <button class="xs primary" @click="goDetail(row.deviceID)" :title="$t('devices.detail')">
              {{ $t('devices.detail') }}
            </button>
            <button class="xs outline" @click="dispatchTask(row.deviceID)" :title="$t('devices.dispatch_task')">
              {{ $t('devices.dispatch_task') }}
            </button>
          </div>
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
        <div class="btnbar">
          <button v-if="dev.state === 'discovered'" class="primary" @click="provision">
            推送 Agent 纳管
          </button>
          <button class="outline" @click="goDetail(dev.deviceID)">
            {{ $t('devices.view_metrics') }}
          </button>
          <button class="outline" @click="dispatchTask(dev.deviceID)">
            {{ $t('devices.dispatch_task') }}
          </button>
        </div>

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
// 设备纳管列表 — 按网段分组，行内可跳转详情 / 下发任务
import { computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useDeviceStore } from '@/stores/device'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'

const router = useRouter()
const store = useDeviceStore()

// 表格列定义：含 hostname / segment / state / os 字段 + 操作列
const columns = [
  { key: 'hostname', title: '主机名' },
  { key: 'deviceID', title: 'DeviceID', slot: 'cell-deviceID' },
  { key: 'segment', title: '网段' },
  { key: 'ip', title: 'IP' },
  { key: 'state', title: '状态', slot: 'cell-state' },
  { key: 'os', title: 'OS' },
  { key: 'agentID', title: '采集端' },
  { key: 'taskState', title: '任务态' },
  { key: 'lastResult', title: 'LastResult', slot: 'cell-lastResult' },
  { key: 'actions', title: '操作', slot: 'cell-actions', width: '160px' }
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

// 网段分组：每个设备注入 segment 字段，便于表格直接展示
const segments = computed(() =>
  Object.entries(store.segments).map(([name, devices]) => ({
    name,
    devices: (devices || []).map((d) => ({ ...d, segment: name }))
  }))
)
const dev = computed(() => (store.current && store.current.device) || {})
const drawerTitle = computed(() => '设备 ' + (dev.value.deviceID || ''))

function rowClass(row) {
  return row.lastResult === 'failed' ? 'fail-row' : ''
}
function openDevice(row) { store.openDevice(row.deviceID) }

// 跳转到设备详情页（监控指标仪表盘）
function goDetail(id) {
  if (!id) return
  store.closeDrawer()
  router.push({ name: 'device-detail', params: { id } })
}

// 下发任务：跳转到任务页，并通过 query 指定目标设备
function dispatchTask(id) {
  if (!id) return
  store.closeDrawer()
  router.push({ name: 'tasks', query: { device: id } })
}

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
.row-actions { display: inline-flex; gap: 6px; }
:deep(.fail-row) { background: var(--fail-soft) !important; }
:deep(.fail-row:hover) { background: var(--fail-bg) !important; }
</style>
