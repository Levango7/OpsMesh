<template>
  <PageShell
    :title="$t('diststorage_ops.title')"
    :desc="$t('diststorage_ops.subtitle')"
    testid="diststorage-ops"
  >
    <div class="toolbar">
      <div class="field">
        <label>{{ L('目标设备', 'Target Device') }}</label>
        <select v-model="selectedDevice" data-testid="diststorage-ops-device">
          <option value="">{{ L('选择设备', 'Select device') }}</option>
          <option v-for="d in devices" :key="d.deviceID" :value="d.deviceID">
            {{ d.hostname || d.deviceID }} ({{ d.ip || d.deviceID }})
          </option>
        </select>
      </div>
      <Button variant="ghost" size="md" @click="refreshDevices" :disabled="loadingDevices">
        <Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}
      </Button>
    </div>

    <div v-if="errorMsg" class="poll-err" data-testid="diststorage-ops-error">{{ errorMsg }}</div>

    <div class="tabbar" data-testid="diststorage-ops-tabs">
      <Button
        v-for="t in tabs"
        :key="t.key"
        variant="ghost"
        size="md"
        :class="['tab', { active: activeTab === t.key }]"
        @click="activeTab = t.key"
        :data-testid="'diststorage-ops-tab-' + t.key"
      >
        {{ $t('diststorage_ops.tab_' + t.key) }}
      </Button>
    </div>

    <div class="actions" data-testid="diststorage-ops-actions">
      <Button
        v-for="a in currentActions"
        :key="a"
        variant="primary"
        size="sm"
        :disabled="submitting || !selectedDevice"
        @click="run(a)"
        :data-testid="'diststorage-ops-action-' + a"
      >
        {{ a }}
      </Button>
    </div>

    <div v-if="result" class="result" data-testid="diststorage-ops-result">
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
const loadingDevices = ref(false)
const submitting = ref(false)
const errorMsg = ref('')
const activeTab = ref('moosefs')
const result = ref(null)

const tabs = [
  { key: 'moosefs', actions: ['status', 'master-start', 'master-stop', 'master-info', 'chunkserver-list', 'chunkserver-add', 'chunkserver-remove', 'metalogger-status', 'metalogger-start', 'metalogger-stop', 'mount', 'umount', 'quota-set', 'quota-get', 'snapshot-create', 'snapshot-delete', 'snapshot-list', 'trash-purge', 'repair', 'rebalance', 'monitoring'] },
  { key: 'juicefs', actions: ['format', 'fsck', 'gc', 'profile', 'dump', 'load', 'config', 'sync', 'warmup', 'stats', 'mount', 'umount', 'quota-set', 'quota-get', 'status'] },
  { key: 'lustre', actions: ['mgs-start', 'mgs-stop', 'mds-start', 'mds-stop', 'oss-start', 'oss-stop', 'client-mount', 'client-umount', 'lfs', 'lfs-setstripe', 'lfs-getstripe', 'lfs-find', 'lfs-migrate', 'pool-list', 'pool-create', 'pool-delete', 'mount', 'umount', 'quota-set', 'quota-get', 'status'] },
  { key: 'beegfs', actions: ['mgmtd-start', 'mgmtd-stop', 'meta-start', 'meta-stop', 'storage-start', 'storage-stop', 'ctl-listnodes', 'ctl-listtargets', 'ctl-listpools', 'ctl-liststoragepools', 'mount', 'umount', 'quota-set', 'quota-get', 'status'] },
  { key: 'cephfs', actions: ['mds-list', 'mds-add', 'mds-remove', 'mds-failover', 'scrub', 'mount', 'umount', 'quota-set', 'quota-get', 'status'] },
  { key: 'glusterfs', actions: ['status', 'deploy', 'create-volume', 'delete-volume', 'peer-probe', 'mount', 'umount'] },
  { key: 'seaweedfs', actions: ['status', 'mount', 'umount'] },
  { key: 'nfs', actions: ['status', 'mount', 'umount', 'export'] },
  { key: 'samba', actions: ['status', 'share', 'mount', 'umount'] }
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
    const data = await postJSON('/api/v1/db/diststorage/ext', {
      deviceID: selectedDevice.value,
      type: currentTab.value.key,
      action
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
