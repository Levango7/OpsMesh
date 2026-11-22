<template>
  <PageShell
    :title="$t('bigdata_ops.title')"
    :desc="$t('bigdata_ops.subtitle')"
    testid="bigdata-ops"
  >
    <div class="toolbar">
      <div class="field">
        <label>{{ L('目标设备', 'Target Device') }}</label>
        <select v-model="selectedDevice" data-testid="bigdata-ops-device">
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

    <div v-if="errorMsg" class="poll-err" data-testid="bigdata-ops-error">{{ errorMsg }}</div>

    <div class="tabbar" data-testid="bigdata-ops-tabs">
      <Button
        v-for="t in tabs"
        :key="t.key"
        variant="ghost"
        size="md"
        :class="['tab', { active: activeTab === t.key }]"
        @click="activeTab = t.key"
        :data-testid="'bigdata-ops-tab-' + t.key"
      >
        {{ $t('bigdata_ops.tab_' + t.key) }}
      </Button>
    </div>

    <div class="actions" data-testid="bigdata-ops-actions">
      <Button
        v-for="a in currentActions"
        :key="a"
        variant="primary"
        size="sm"
        :disabled="submitting || !selectedDevice"
        @click="run(a)"
        :data-testid="'bigdata-ops-action-' + a"
      >
        {{ a }}
      </Button>
    </div>

    <div v-if="result" class="result" data-testid="bigdata-ops-result">
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
const activeTab = ref('ambari')
const result = ref(null)

const tabs = [
  { key: 'ambari', endpoint: 'bigdata/cluster', type: 'ambari', actions: ['ambari-deploy', 'ambari-status', 'ambari-services', 'ambari-service-start', 'ambari-service-stop', 'ambari-service-restart', 'ambari-config-get', 'ambari-config-set', 'ambari-hosts', 'ambari-host-add', 'ambari-host-remove', 'ambari-cluster-create', 'ambari-blueprint', 'ambari-rolling-restart', 'ambari-kerberos-enable', 'ambari-kerberos-disable', 'ambari-alerts', 'ambari-metrics'] },
  { key: 'cloudera', endpoint: 'bigdata/cluster', type: 'cloudera', actions: ['cm-deploy', 'cm-status', 'cm-clusters', 'cm-services', 'cm-service-start', 'cm-service-stop', 'cm-service-restart', 'cm-config', 'cm-hosts', 'cm-kerberos', 'cm-rolling-restart', 'cm-upgrade', 'cm-parcel-list', 'cm-parcel-install', 'cm-parcel-activate', 'cm-alerts'] },
  { key: 'hadoop-ha', endpoint: 'bigdata/cluster', type: 'hadoop', actions: ['ha-namenode-status', 'ha-namenode-failover', 'ha-namenode-checkpoint', 'ha-zkfc-status', 'ha-journalnode-status', 'ha-yarn-rm-status', 'ha-yarn-rm-failover', 'ha-hdfs-fsck', 'ha-hdfs-balancer', 'ha-hdfs-quota-set', 'ha-hdfs-quota-get', 'ha-yarn-queue-list', 'ha-yarn-queue-create', 'ha-yarn-queue-delete', 'ha-yarn-queue-update', 'ha-yarn-app-kill', 'ha-yarn-app-list'] },
  { key: 'kerberos', endpoint: 'bigdata/cluster', type: 'hadoop', actions: ['kerberos-status', 'kerberos-principal-list', 'kerberos-principal-create', 'kerberos-principal-delete', 'kerberos-keytab-generate', 'kerberos-kinit-test'] },
  { key: 'spark', endpoint: 'sparkflink/ext', type: 'spark', actions: ['status', 'submit', 'kill', 'logs', 'app-list', 'app-status', 'history-server', 'spark-shell', 'sql', 'thrift-server', 'streaming', 'structured-streaming', 'checkpoint-status', 'executor-list', 'executor-kill', 'config', 'monitoring'] },
  { key: 'flink', endpoint: 'sparkflink/ext', type: 'flink', actions: ['job-list', 'job-status', 'savepoint', 'restore', 'checkpoint-list', 'taskmanager-list', 'taskmanager-kill', 'web-ui', 'sql-client'] },
  { key: 'ranger', endpoint: 'hadoop/ecosystem', type: 'ranger', actions: ['status', 'policy-list', 'policy-create', 'policy-delete', 'audit', 'user-sync'] },
  { key: 'atlas', endpoint: 'hadoop/ecosystem', type: 'atlas', actions: ['entity-search', 'lineage', 'type-def', 'classification'] },
  { key: 'knox', endpoint: 'hadoop/ecosystem', type: 'knox', actions: ['topology-list', 'topology-create', 'service-list', 'ldap-config'] },
  { key: 'sqoop', endpoint: 'hadoop/ecosystem', type: 'sqoop', actions: ['import', 'export', 'job-list', 'job-create', 'job-delete', 'job-exec'] },
  { key: 'flume', endpoint: 'hadoop/ecosystem', type: 'flume', actions: ['agent-status', 'flow-list', 'flow-deploy', 'flow-stop'] },
  { key: 'hue', endpoint: 'hadoop/ecosystem', type: 'hue', actions: ['user-list', 'query-history'] },
  { key: 'oozie', endpoint: 'hadoop/ecosystem', type: 'oozie', actions: ['workflow-list', 'workflow-submit', 'workflow-kill', 'coordinator-list', 'bundle-list'] },
  { key: 'zeppelin', endpoint: 'hadoop/ecosystem', type: 'zeppelin', actions: ['notebook-list', 'notebook-run', 'interpreter-list', 'interpreter-restart'] }
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
    const data = await postJSON(`/api/v1/db/${currentTab.value.endpoint}`, {
      deviceID: selectedDevice.value,
      type: currentTab.value.type,
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
