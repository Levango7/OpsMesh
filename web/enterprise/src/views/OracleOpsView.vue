<template>
  <PageShell
    :title="$t('oracle_ops.title')"
    :desc="$t('oracle_ops.subtitle')"
    testid="oracle-ops"
  >
    <div class="toolbar">
      <div class="field">
        <label>{{ L('目标设备', 'Target Device') }}</label>
        <select v-model="selectedDevice" data-testid="oracle-ops-device">
          <option value="">{{ L('选择设备', 'Select device') }}</option>
          <option v-for="d in devices" :key="d.deviceID" :value="d.deviceID">
            {{ d.hostname || d.deviceID }} ({{ d.ip || d.deviceID }})
          </option>
        </select>
      </div>
      <div class="field">
        <label>{{ $t('oracle_ops.db') }}</label>
        <input v-model="db" data-testid="oracle-ops-db" />
      </div>
      <Button variant="ghost" size="md" @click="refreshDevices" :disabled="loadingDevices">
        <Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}
      </Button>
    </div>

    <div v-if="errorMsg" class="poll-err" data-testid="oracle-ops-error">{{ errorMsg }}</div>

    <div class="tabbar" data-testid="oracle-ops-tabs">
      <Button
        v-for="t in tabs"
        :key="t.key"
        variant="ghost"
        size="md"
        :class="['tab', { active: activeTab === t.key }]"
        @click="activeTab = t.key"
        :data-testid="'oracle-ops-tab-' + t.key"
      >
        {{ $t('oracle_ops.tab_' + t.key) }}
      </Button>
    </div>

    <div class="actions" data-testid="oracle-ops-actions">
      <Button
        v-for="a in currentActions"
        :key="a"
        variant="primary"
        size="sm"
        :disabled="submitting || !selectedDevice"
        @click="run(a)"
        :data-testid="'oracle-ops-action-' + a"
      >
        {{ a }}
      </Button>
    </div>

    <div v-if="result" class="result" data-testid="oracle-ops-result">
      <pre>{{ resultText }}</pre>
    </div>
  </PageShell>
</template>

<script setup>
import PageShell from '@/components/PageShell.vue'
import Button from '@/components/ui/Button.vue'
import Icon from '@/components/Icon.vue'
import { computed, onMounted, ref } from 'vue'
import { useDeviceStore } from '@/stores/device'
import { currentLang } from '@/i18n'
import { postJSON } from '@/api/request'
import { toast } from '@/utils/toast'

const deviceStore = useDeviceStore()
const L = (zh, en) => (currentLang.value === 'en' ? en : zh)
const devices = computed(() => deviceStore.flat || [])
const selectedDevice = ref('')
const db = ref('ORCL')
const loadingDevices = ref(false)
const submitting = ref(false)
const errorMsg = ref('')
const activeTab = ref('ext')
const result = ref(null)

const extActions = [
  'asm-diskgroups', 'asm-rebalance', 'asm-add-disk', 'asm-drop-disk', 'asm-status',
  'tbs-list', 'tbs-create', 'tbs-drop', 'tbs-resize', 'tbs-offline', 'tbs-online',
  'pdb-list', 'pdb-create', 'pdb-open', 'pdb-close', 'pdb-drop', 'pdb-save-state',
  'cdb-info', 'pdb-plugin', 'pdb-unplug',
  'flashback-query', 'flashback-table', 'flashback-database', 'flashback-enable', 'flashback-disable',
  'partition-list', 'partition-create-range', 'partition-create-list', 'partition-create-hash', 'partition-create-interval', 'partition-drop', 'partition-split', 'partition-merge',
  'net-listener-status', 'net-services', 'net-registration',
  'priv-list', 'priv-grant', 'priv-revoke', 'role-list', 'role-create', 'role-drop',
  'iso-level', 'lock-list', 'long-running', 'blocking-chain',
  'awr-report', 'ash-report', 'addm-report', 'sqlplan',
  'tde-status', 'tde-wallet-open', 'tde-wallet-close', 'tde-key-rotate',
  'archive-log-status', 'archive-log-switch', 'archive-log-list',
  'stats-gather', 'stats-delete', 'stats-lock', 'stats-unlock'
]

const haActions = [
  'dg-status', 'dg-lag', 'dg-switchover', 'dg-failover', 'dg-reinstate', 'dg-convert-physical', 'dg-convert-logical',
  'dg-broker-status', 'dg-broker-enable', 'dg-broker-disable', 'dg-broker-failover', 'dg-broker-switchover',
  'adg-open-readonly', 'adg-status',
  'rac-status', 'rac-nodes', 'rac-add-node', 'rac-remove-node', 'rac-voting', 'rac-asm', 'rac-scan', 'rac-health', 'rac-crs-check',
  'rac-gi-status', 'rac-gi-start', 'rac-gi-stop',
  'flashback-database-restore', 'point-in-time-recovery'
]

const tabs = [
  { key: 'ext', endpoint: 'ext', actions: extActions },
  { key: 'ha', endpoint: 'ha', actions: haActions },
  { key: 'backup', endpoint: 'backup', actions: ['expdp', 'impdp', 'rman', 'backup-list', 'restore'] },
  { key: 'replication', endpoint: 'replication', actions: ['status', 'setup', 'switchover', 'failover', 'lag'] },
  { key: 'perf', endpoint: 'perf', actions: ['awr', 'ash', 'addm', 'sql-tuning', 'top-sql'] },
  { key: 'security', endpoint: 'security', actions: ['tde', 'audit', 'user', 'password', 'role'] },
  { key: 'upgrade', endpoint: 'upgrade', actions: ['precheck', 'patch', 'ru', 'rur', 'rollback', 'version'] },
  { key: 'migration', endpoint: 'migration', actions: ['datapump', 'rman', 'goldengate', 'status'] },
  { key: 'rac', endpoint: 'rac', actions: ['status', 'grid', 'asm', 'voting', 'scanvip', 'node-add', 'node-remove', 'health'] }
]

const currentTab = computed(() => tabs.find((t) => t.key === activeTab.value) || tabs[0])
const currentActions = computed(() => currentTab.value.actions)
const resultText = computed(() => (result.value ? JSON.stringify(result.value, null, 2) : ''))

async function refreshDevices() {
  loadingDevices.value = true
  try {
    await deviceStore.fetchDevices()
  } catch (e) {
    errorMsg.value = e?.message || String(e)
  } finally {
    loadingDevices.value = false
  }
}

async function run(action) {
  if (!selectedDevice.value) {
    errorMsg.value = 'device required'
    return
  }
  submitting.value = true
  errorMsg.value = ''
  try {
    const data = await postJSON(`/api/v1/db/oracle/${currentTab.value.endpoint}`, {
      deviceID: selectedDevice.value,
      action,
      db: db.value
    })
    result.value = data
  } catch (e) {
    errorMsg.value = e?.message || String(e)
    toast.error(errorMsg.value)
  } finally {
    submitting.value = false
  }
}

onMounted(() => {
  refreshDevices()
})
</script>


F:\Agent\kilo\workspace\OpsMesh\web\enterprise\src\views\OracleOpsView.vue