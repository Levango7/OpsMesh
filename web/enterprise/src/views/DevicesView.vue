<template>
  <PageShell :title="$t('devices.title')" :desc="$t('devices.subtitle')" testid="devices" :error="store.error">
    <div v-if="store.loading && !segments.length" class="muted">{{ $t('common.loading') }}</div>
    <div v-else-if="!segments.length" class="muted" data-testid="devices-empty">
      {{ $t('devices.empty') }}
    </div>

    <div v-for="seg in segments" :key="seg.name" class="seg-block">
      <h3 class="seg-title">{{ $t('devices.segment', { name: seg.name, n: seg.devices.length }) }}</h3>
      <DataTable
        :columns="columns"
        :rows="seg.devices"
        row-key="deviceID"
        :clickable="true"
        :row-class="rowClass"
        :loading="store.loading"
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
            <Button
              variant="primary"
              size="sm"
              @click="goDetail(row.deviceID)"
              :title="$t('devices.detail')"
              data-testid="device-detail-btn"
            >
              {{ $t('devices.detail') }}
            </Button>
            <Button
              variant="ghost"
              size="sm"
              @click="dispatchTask(row.deviceID)"
              :title="$t('devices.dispatch_task')"
              data-testid="device-dispatch-btn"
            >
              {{ $t('devices.dispatch_task') }}
            </Button>
          </div>
        </template>
      </DataTable>
    </div>

    <Drawer
      v-model="drawerOpen"
      :title="drawerTitle"
      @close="store.closeDrawer()"
    >
      <div v-if="store.current">
        <p class="drawer__info">
          {{ $t('devices.ip_label') }}: {{ dev.ip }} &nbsp;|&nbsp;
          {{ $t('devices.agent_label') }}: {{ dev.agentID }} &nbsp;|&nbsp;
          {{ $t('devices.tenant_label') }}: {{ dev.tenantID }}
        </p>
        <p class="drawer__info">
          {{ $t('devices.state_label') }}: {{ dev.state }} &nbsp;|&nbsp;
          {{ $t('devices.task_state_label') }}: {{ dev.taskState }}
        </p>
        <p
          v-if="dev.lastResult"
          class="drawer__msg"
          :class="dev.lastResult === 'failed' ? 'drawer__msg--err' : 'drawer__msg--ok'"
        >
          {{ $t('devices.col_last_result') }}: {{ dev.lastResult }} @ {{ fmtTime(dev.lastResultAt) }}
        </p>
        <div class="btnbar">
          <Button
            v-if="dev.state === 'discovered'"
            variant="primary"
            size="sm"
            @click="provision"
            data-testid="device-provision-btn"
          >
            {{ $t('devices.provision') }}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            @click="goDetail(dev.deviceID)"
          >
            {{ $t('devices.view_metrics') }}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            @click="dispatchTask(dev.deviceID)"
          >
            {{ $t('devices.dispatch_task') }}
          </Button>
        </div>

        <h4 class="drawer__section-title">{{ $t('devices.tasks_title') }}</h4>
        <DataTable
          :columns="taskCols"
          :rows="store.current.tasks || []"
          row-key="taskID"
          :empty-text="$t('devices.no_tasks')"
        >
          <template #cell-taskID="{ value }"><code>{{ value }}</code></template>
        </DataTable>

        <h4 class="drawer__section-title">{{ $t('devices.recent_results') }}</h4>
        <DataTable
          :columns="resultCols"
          :rows="(store.current.results || []).slice(0, 5)"
          :empty-text="$t('devices.no_results')"
        >
          <template #cell-taskID="{ value }"><code>{{ value }}</code></template>
          <template #cell-stdout="{ value }"><code>{{ value }}</code></template>
        </DataTable>
      </div>
    </Drawer>
  </PageShell>
</template>

<script setup>
import PageShell from '@/components/PageShell.vue'

// 设备纳管列表 — 按网段分组，行内可跳转详情 / 下发任务
import { computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useDeviceStore } from '@/stores/device'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { Button, Drawer } from '@/components/ui'
import { fmtTime } from '@/composables/useFormatTime'

const router = useRouter()
const store = useDeviceStore()

const drawerOpen = computed({
  get: () => !!store.current,
  set: (val) => { if (!val) store.closeDrawer() }
})

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
  try {
    await store.provision(dev.value.deviceID)
  } catch (e) {
    store.error = e.j?.error || e.message || t('devices.provision_failed')
  }
}

onMounted(() => { if (!store.total) store.fetchDevices() })
</script>

<style scoped>
.seg-block {
  margin-bottom: var(--space-4, 16px);
}

.seg-title {
  margin: 0 0 var(--space-2, 8px) 0;
  font-size: var(--font-size-sm, 13px);
  font-weight: 600;
  color: var(--color-ink, #f7f8f8);
}

:deep(.fail-row) {
  background: var(--color-error-soft, #fef2f2) !important;
}
:deep(.fail-row:hover) {
  background: var(--color-error-light, #fecaca) !important;
}

.drawer__info {
  margin-bottom: var(--space-3, 12px);
  font-size: var(--font-size-sm, 13px);
  color: var(--color-ink, #f7f8f8);
}

.drawer__msg {
  margin-bottom: var(--space-3, 12px);
  font-size: var(--font-size-sm, 13px);
  color: var(--color-ink-muted, #d0d6e0);
}

.drawer__msg--err {
  color: var(--color-error, #ef4444);
}

.drawer__msg--ok {
  color: var(--color-success, #27a644);
}

.drawer__section-title {
  margin: var(--space-3, 12px) 0 var(--space-1, 6px) 0;
  font-size: var(--font-size-sm, 13px);
  font-weight: 600;
  color: var(--color-ink-muted, #d0d6e0);
}
</style>