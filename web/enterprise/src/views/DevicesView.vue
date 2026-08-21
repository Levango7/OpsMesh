<template>
  <div>
    <h2>{{ $t('devices.title') }}</h2>
    <p class="muted">{{ $t('devices.subtitle') }}</p>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <div v-if="store.loading && !segments.length" class="muted">{{ $t('common.loading') }}</div>
    <div v-else-if="!segments.length" class="muted">
      {{ $t('devices.empty') }}
    </div>

    <div v-for="seg in segments" :key="seg.name" class="seg-block">
      <h3>{{ $t('devices.segment', { name: seg.name, n: seg.devices.length }) }}</h3>
      <DataTable
        :columns="columns"
        :rows="seg.devices"
        row-key="deviceID"
        :clickable="true"
        :row-class="rowClass"
        :empty-text="$t('devices.segment_empty')"
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
        <p>{{ $t('devices.ip_label') }}: {{ dev.ip }} ｜ {{ $t('devices.agent_label') }}: {{ dev.agentID }} ｜ {{ $t('devices.tenant_label') }}: {{ dev.tenantID }}</p>
        <p>{{ $t('devices.state_label') }}: {{ dev.state }} ｜ {{ $t('devices.task_state_label') }}: {{ dev.taskState }}</p>
        <p v-if="dev.lastResult" :class="['msg', dev.lastResult === 'failed' ? 'err' : 'ok']">
          {{ $t('devices.col_last_result') }}: {{ dev.lastResult }} @ {{ fmtTime(dev.lastResultAt) }}
        </p>
        <div class="btnbar">
          <button v-if="dev.state === 'discovered'" class="primary" @click="provision">
            {{ $t('devices.provision') }}
          </button>
          <button class="outline" @click="goDetail(dev.deviceID)">
            {{ $t('devices.view_metrics') }}
          </button>
          <button class="outline" @click="dispatchTask(dev.deviceID)">
            {{ $t('devices.dispatch_task') }}
          </button>
        </div>

        <h4>{{ $t('devices.tasks_title') }}</h4>
        <DataTable :columns="taskCols" :rows="store.current.tasks || []" row-key="taskID" :empty-text="$t('devices.no_tasks')">
          <template #cell-taskID="{ value }"><code>{{ value }}</code></template>
        </DataTable>

        <h4>{{ $t('devices.recent_results') }}</h4>
        <DataTable :columns="resultCols" :rows="(store.current.results || []).slice(0, 5)" :empty-text="$t('devices.no_results')">
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
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import { fmtTime } from '@/composables/useFormatTime'

const router = useRouter()
const store = useDeviceStore()

// 表格列定义：含 hostname / segment / state / os 字段 + 操作列
const columns = [
  { key: 'hostname', title: t('devices.col_hostname'), sortable: true },
  { key: 'deviceID', title: t('devices.col_device_id'), slot: 'cell-deviceID' },
  { key: 'segment', title: t('devices.col_segment') },
  { key: 'ip', title: t('devices.col_ip') },
  { key: 'state', title: t('devices.col_state'), slot: 'cell-state', sortable: true },
  { key: 'os', title: t('devices.col_os'), sortable: true },
  { key: 'agentID', title: t('devices.col_agent') },
  { key: 'taskState', title: t('devices.col_task_state') },
  { key: 'lastResult', title: t('devices.col_last_result'), slot: 'cell-lastResult' },
  { key: 'actions', title: t('devices.col_actions'), slot: 'cell-actions', width: '160px' }
]
const taskCols = [
  { key: 'taskID', title: t('devices.col_id'), slot: 'cell-taskID' },
  { key: 'type', title: t('devices.col_type') },
  { key: 'status', title: t('devices.col_status') }
]
const resultCols = [
  { key: 'taskID', title: t('devices.col_task'), slot: 'cell-taskID' },
  { key: 'exitCode', title: t('devices.col_exit_code') },
  { key: 'stdout', title: t('devices.col_output'), slot: 'cell-stdout' }
]

// 网段分组：每个设备注入 segment 字段，便于表格直接展示
const segments = computed(() =>
  Object.entries(store.segments).map(([name, devices]) => ({
    name,
    devices: (devices || []).map((d) => ({ ...d, segment: name }))
  }))
)
const dev = computed(() => (store.current && store.current.device) || {})
const drawerTitle = computed(() => t('devices.drawer_title_prefix') + (dev.value.deviceID || ''))

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
