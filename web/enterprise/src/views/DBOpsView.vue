<template>
  <PageShell
    :title="$t('db_ops.title')"
    :desc="$t('db_ops.subtitle')"
    testid="db-ops"
  >
    <div class="toolbar">
      <div class="field">
        <label>{{ L('目标设备', 'Target Device') }}</label>
        <select v-model="selectedDevice" data-testid="db-ops-device">
          <option value="">{{ L('选择设备', 'Select device') }}</option>
          <option v-for="d in devices" :key="d.deviceID" :value="d.deviceID">
            {{ d.hostname || d.deviceID }} ({{ d.ip || d.deviceID }})
          </option>
        </select>
      </div>
      <div class="field">
        <label>{{ $t('db_ops.db') }}</label>
        <input v-model="db" data-testid="db-ops-db" />
      </div>
      <Button variant="ghost" size="md" @click="refreshDevices" :disabled="loadingDevices">
        <Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}
      </Button>
    </div>

    <div v-if="errorMsg" class="poll-err" data-testid="db-ops-error">{{ errorMsg }}</div>

    <div class="tabbar" data-testid="db-ops-tabs">
      <Button
        v-for="t in tabs"
        :key="t.key"
        variant="ghost"
        size="md"
        :class="['tab', { active: activeTab === t.key }]"
        @click="activeTab = t.key"
        :data-testid="'db-ops-tab-' + t.key"
      >
        {{ $t('db_ops.tab_' + t.key) }}
      </Button>
    </div>

    <div class="actions" data-testid="db-ops-actions">
      <Button
        v-for="a in currentActions"
        :key="a"
        variant="primary"
        size="sm"
        :disabled="submitting || !selectedDevice"
        @click="run(a)"
        :data-testid="'db-ops-action-' + a"
      >
        {{ a }}
      </Button>
    </div>

    <div v-if="result" class="result" data-testid="db-ops-result">
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
const db = ref('postgres')
const loadingDevices = ref(false)
const submitting = ref(false)
const errorMsg = ref('')
const activeTab = ref('extensions')
const result = ref(null)

const tabs = [
  { key: 'extensions', endpoint: 'extensions', actions: ['list-installed', 'list-available', 'install', 'upgrade', 'drop', 'info'] },
  { key: 'mvcc', endpoint: 'mvcc', actions: ['analyze', 'autovacuum-settings', 'bloat-estimate', 'dead-tuples', 'freeze-age', 'reindex', 'vacuum', 'vacuum-full'] },
  { key: 'partition', endpoint: 'partition', actions: ['list', 'list-partitions', 'create-range', 'create-list', 'create-hash', 'attach', 'detach', 'drop-partition'] },
  { key: 'replication', endpoint: 'repl', actions: ['slots-list', 'slot-create', 'slot-drop', 'pubs-list', 'pub-create', 'pub-drop', 'subs-list', 'sub-create', 'sub-drop', 'physical', 'logical', 'base-backup'] },
  { key: 'fdw', endpoint: 'fdw', actions: ['servers-list', 'server-create', 'server-drop', 'tables-list', 'table-create', 'table-drop', 'import-schema', 'mappings-list'] },
  { key: 'types', endpoint: 'types', actions: ['types-list', 'enums-list', 'domains-list', 'type-info', 'type-create-enum', 'type-create-composite', 'type-create-domain', 'type-drop'] },
  { key: 'gis', endpoint: 'gis', actions: ['postgis-check', 'extension-install', 'spatial-columns', 'srid-list', 'spatial-stats'] },
  { key: 'monitor', endpoint: 'monitor', actions: ['stats', 'locks', 'long-running', 'connections', 'cache-hit', 'replication-lag', 'wal', 'checkpoints'] }
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
    const data = await postJSON(`/api/v1/db/postgresql/${currentTab.value.endpoint}`, {
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
