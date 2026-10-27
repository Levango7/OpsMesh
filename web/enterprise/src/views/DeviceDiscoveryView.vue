<template>
  <PageShell
    :title="$t('deviceDiscovery.title')"
    :desc="$t('deviceDiscovery.desc')"
    testid="device-discovery"
    :error="store.error || loadErr"
  >
    <!-- 启动发现任务 -->
    <div class="card">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('deviceDiscovery.start_title') }}</h3>
      </div>
      <div class="field">
        <label>{{ $t('deviceDiscovery.cidr_label') }}</label>
        <input
          v-model="cidr"
          :placeholder="$t('deviceDiscovery.cidr_placeholder')"
          class="cidr-input"
          @keyup.enter="startDiscovery"
        />
      </div>
      <div class="btnbar">
        <Button variant="primary" size="sm" @click="startDiscovery" :disabled="discovering">{{ $t('deviceDiscovery.start_btn') }}</Button>
        <Button variant="ghost" size="sm" @click="refreshJobs">{{ $t('common.refresh') }}</Button>
      </div>
      <p v-if="discoveryMsg" :class="['msg', discoveryMsgOk ? 'ok' : 'err']">{{ discoveryMsg }}</p>
    </div>

    <!-- 发现任务列表 -->
    <div class="card">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('deviceDiscovery.jobs_title') }}</h3>
      </div>
      <DataTable
        v-if="jobRows.length"
        :columns="jobColumns"
        :rows="jobRows"
        row-key="jobID"
        :empty-text="$t('deviceDiscovery.noJobs')"
      >
        <template #cell-status="{ value }">
          <StatusBadge :status="jobStatus(value)" :text="value || '-'" />
        </template>
        <template #cell-actions="{ row }">
          <div class="row-actions" @click.stop>
            <Button variant="ghost" size="sm" @click="refreshJob(row.jobID)">{{ $t('common.refresh') }}</Button>
            <Button variant="ghost" size="sm" @click="viewDevices(row.jobID)">{{ $t('deviceDiscovery.view_devices') }}</Button>
          </div>
        </template>
      </DataTable>
      <div v-else class="muted">{{ $t('deviceDiscovery.noJobs') }}</div>
    </div>

    <!-- 已发现设备 -->
    <div class="card">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('deviceDiscovery.discovered_title') }}</h3>
      </div>
      <DataTable
        v-if="store.discoveredDevices.length"
        :columns="deviceColumns"
        :rows="store.discoveredDevices"
        row-key="id"
        :empty-text="$t('deviceDiscovery.noDevices')"
      >
        <template #cell-status="{ value }">
          <StatusBadge :status="deviceStatus(value)" :text="value || '-'" />
        </template>
        <template #cell-actions="{ row }">
          <div class="row-actions" @click.stop>
            <Button variant="primary" size="sm" @click="provisionDiscovered(row)">{{ $t('deviceDiscovery.provision_btn') }}</Button>
          </div>
        </template>
      </DataTable>
      <div v-else class="muted">{{ $t('deviceDiscovery.noDevices') }}</div>
    </div>
  </PageShell>
</template>

<script setup>
import PageShell from '@/components/PageShell.vue'

import { ref, computed } from 'vue'
import { useDeviceStore } from '@/stores/device'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { Button } from '@/components/ui'

const store = useDeviceStore()

const cidr = ref('')
const discovering = ref(false)
const discoveryMsg = ref('')
const discoveryMsgOk = ref(false)
// 刷新类操作此前静默吞错——统一骨架批次补上可见错误（与 store.error 并列展示）
const loadErr = ref('')

const jobRows = computed(() => {
  return Object.values(store.discoveryJobs).sort((a, b) => (b.createdAt || '') - (a.createdAt || ''))
})

const jobColumns = [
  { key: 'jobID', title: 'Job ID', slot: 'cell-jobID' },
  { key: 'cidr', title: t('deviceDiscovery.cidr') },
  { key: 'status', title: t('deviceDiscovery.status'), slot: 'cell-status' },
  { key: 'devicesFound', title: t('deviceDiscovery.devicesFound') },
  { key: 'createdAt', title: t('deviceDiscovery.createdAt') },
  { key: 'actions', title: t('deviceDiscovery.actions'), slot: 'cell-actions', width: '140px' }
]

const deviceColumns = [
  { key: 'id', title: 'ID', slot: 'cell-id' },
  { key: 'ip', title: t('deviceDiscovery.ip') },
  { key: 'hostname', title: t('deviceDiscovery.hostname') },
  { key: 'status', title: t('deviceDiscovery.status'), slot: 'cell-status' },
  { key: 'actions', title: t('deviceDiscovery.actions'), slot: 'cell-actions', width: '120px' }
]

function jobStatus(s) {
  if (s === 'completed' || s === 'done') return 'success'
  if (s === 'failed' || s === 'error') return 'failed'
  if (s === 'running' || s === 'pending') return 'info'
  return 'warn'
}

function deviceStatus(s) {
  if (s === 'managed' || s === 'online') return 'success'
  if (s === 'discovered') return 'info'
  if (s === 'failed' || s === 'error') return 'failed'
  return 'warn'
}

async function startDiscovery() {
  const c = cidr.value.trim()
  if (!c) {
    discoveryMsg.value = t('deviceDiscovery.cidr_required')
    discoveryMsgOk.value = false
    return
  }
  discovering.value = true
  discoveryMsg.value = ''
  try {
    const job = await store.startDiscovery('default', c)
    discoveryMsg.value = t('deviceDiscovery.started', { id: job.jobID || job.ID })
    discoveryMsgOk.value = true
    cidr.value = ''
    // 轮询任务状态
    setTimeout(() => store.getDiscoveryStatus(job.jobID || job.ID), 2000)
  } catch (e) {
    discoveryMsg.value = e.j?.error || t('deviceDiscovery.start_failed')
    discoveryMsgOk.value = false
  } finally {
    discovering.value = false
  }
}

async function refreshJobs() {
  for (const job of jobRows.value) {
    if (job.status === 'running' || job.status === 'pending') {
      try { await store.getDiscoveryStatus(job.jobID || job.ID) } catch (e) { loadErr.value = e.j?.error || t('common.load_failed') }
    }
  }
}

async function refreshJob(jobID) {
  try { await store.getDiscoveryStatus(jobID) } catch (e) { loadErr.value = e.j?.error || t('common.load_failed') }
}

async function viewDevices(_jobID) {
  try {
    await store.getDiscoveredDevices('default')
  } catch (e) { loadErr.value = e.j?.error || t('common.load_failed') }
}

async function provisionDiscovered(row) {
  try {
    await store.provision(row.id)
    await store.getDiscoveredDevices('default')
    discoveryMsg.value = t('deviceDiscovery.provisioned', { id: row.id })
    discoveryMsgOk.value = true
  } catch (e) {
    discoveryMsg.value = e.j?.error || t('deviceDiscovery.provision_failed')
    discoveryMsgOk.value = false
  }
}
</script>

<style scoped>
.cidr-input { width: 260px; }
.flowbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 12px; }
.btnbar { display: flex; gap: 8px; margin-bottom: 12px; }
.row-actions { display: inline-flex; gap: 4px; flex-wrap: wrap; }
.muted { color: var(--color-ink-muted, #6b7280); font-size: 12.5px; }
.msg { margin-top: 8px; font-size: 12.5px; }
.msg.ok { color: var(--color-success, #059669); }
.msg.err { color: var(--color-error, #ef4444); }
.card { margin-bottom: 16px; }
</style>