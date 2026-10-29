<template>
  <PageShell
    :title="$t('federation.title')"
    :desc="$t('federation.subtitle')"
    testid="federation"
    :error="store.error || taskStatusError"
  >
    <!-- Tab 切换 -->
    <div class="tabbar">
      <Button
        variant="primary" size="md" v-for="tab in tabs"
        :key="tab.key"
        :class="['tab', { active: activeTab === tab.key }]"
        :data-testid="'federation-tab-' + tab.key"
        @click="switchTab(tab.key)"
      >{{ $t(tab.label) }}</Button>
    </div>

    <!-- ========== Tab 1: Peer 管理 ========== -->
    <div v-show="activeTab === 'peers'">
      <div class="btnbar btnbar--bottom">
        <Button variant="ghost" size="md" @click="store.fetchPeers()"><Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}</Button>
      </div>
      <div v-if="store.loading && !store.peers.length" class="muted">{{ $t('common.loading') }}</div>
      <DataTable
        v-else
        :columns="peerColumns"
        :rows="store.peers"
        row-key="url"
        :loading="store.loading"
        :empty-text="$t('federation.empty_peers')"
      >
        <template #cell-url="{ value }"><code>{{ value }}</code></template>
        <template #cell-online="{ value }">
          <span class="status-pill" :class="value ? 'ok' : 'off'">{{ value ? $t('federation.online') : $t('federation.offline') }}</span>
        </template>
        <template #cell-latencyMs="{ value }">{{ value != null ? value + ' ms' : '—' }}</template>
      </DataTable>
    </div>

    <!-- ========== Tab 2: 设备聚合视图（分页+搜索） ========== -->
    <div v-show="activeTab === 'devices'">
      <div class="btnbar btnbar--bottom">
        <Button variant="ghost" size="md" @click="loadDevices()"><Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}</Button>
        <input
          v-model="deviceSearch"
          :placeholder="$t('federation.device_search_placeholder')"
          class="search-input"
          @keyup.enter="loadDevices"
        />
        <Button variant="primary" size="sm" @click="loadDevices">{{ $t('federation.device_search') }}</Button>
      </div>

      <div v-if="deviceLoading && !devices.length" class="muted">{{ $t('common.loading') }}</div>

      <!-- 分页信息 -->
      <div v-if="devices.length || deviceTotal > 0" class="page-info">
        <span>{{ $t('federation.device_total') }}: <strong>{{ deviceTotal }}</strong></span>
        <span>{{ $t('federation.device_page') }}: <strong>{{ devicePage }}</strong></span>
        <span>{{ $t('federation.device_page_size') }}: <strong>{{ devicePageSize }}</strong></span>
        <Button
          variant="ghost" size="sm" :disabled="devicePage <= 1"
          @click="prevPage"
          data-testid="federation-device-prev"
        >{{ $t('federation.device_prev') }}</Button>
        <Button
          variant="ghost" size="sm" :disabled="!deviceHasMore"
          @click="nextPage"
          data-testid="federation-device-next"
        >{{ $t('federation.device_next') }}</Button>
      </div>

      <div v-if="deviceLoading && !devices.length" class="muted">{{ $t('common.loading') }}</div>
      <DataTable
        v-else
        :columns="deviceColumns"
        :rows="devices"
        row-key="agentID"
        :loading="deviceLoading"
        :empty-text="$t('federation.empty_devices')"
      >
        <template #cell-agentID="{ value }"><code>{{ value }}</code></template>
        <template #cell-hostname="{ value }">{{ value || '—' }}</template>
        <template #cell-ip="{ value }">{{ value || '—' }}</template>
        <template #cell-os="{ value }">{{ value || '—' }}</template>
        <template #cell-tenant="{ value }">{{ value || '—' }}</template>
        <template #cell-source="{ value }">
          <span class="tag" :class="value === 'local' ? 'ok' : 'off'">{{ value === 'local' ? $t('federation.source_local') : $t('federation.source_peer') }}</span>
        </template>
      </DataTable>
    </div>

    <!-- ========== Tab 3: 任务转发 ========== -->
    <div v-show="activeTab === 'forward'">
      <div class="card card--raised">
        <h3>{{ $t('federation.forward_form_title') }}</h3>
        <form @submit.prevent="onForward">
          <div class="field field--fixed">
            <label>{{ $t('federation.field_peerURL') }}</label>
            <select v-model="form.peerURL" required>
              <option value="">{{ $t('federation.please_select') }}</option>
              <option v-for="p in store.peers" :key="p.url" :value="p.url">{{ p.url }}{{ p.online ? '' : ' (offline)' }}</option>
            </select>
          </div>
          <div class="row-2">
            <div class="field field--fixed">
              <label>{{ $t('federation.field_taskType') }}</label>
              <select v-model="form.taskType">
                <option value="shell">shell</option>
                <option value="file">file</option>
                <option value="service">service</option>
              </select>
            </div>
            <div class="field field--fixed">
              <label>{{ $t('federation.field_deviceID') }}</label>
              <input v-model.trim="form.deviceID" type="text" :placeholder="$t('federation.deviceID_placeholder')" />
            </div>
          </div>
          <div class="field field--fixed">
            <label>{{ $t('federation.field_command') }}</label>
            <input v-model.trim="form.command" type="text" :placeholder="$t('federation.command_placeholder')" required />
          </div>
          <div class="field field--fixed">
            <label>{{ $t('federation.field_timeoutSec') }}</label>
            <input v-model.number="form.timeoutSec" type="number" min="1" />
          </div>
          <div class="btnbar btnbar--bottom">
            <Button variant="primary" size="md" native-type="submit" :disabled="submitting" data-testid="federation-forward-btn">
              {{ submitting ? $t('common.loading') : $t('federation.forward') }}
            </Button>
          </div>
          <p v-if="msg" :class="['msg', msgOk ? 'ok' : 'err']">{{ msg }}</p>
        </form>
      </div>

      <!-- 最近一次转发结果 -->
      <div class="card card--raised" v-if="store.lastForward">
        <h3>{{ $t('federation.forward_result_title') }}</h3>
        <div class="kv-grid">
          <div class="kv"><span class="k">{{ $t('federation.field_taskID') }}</span><span class="v"><code>{{ store.lastForward.taskID || '—' }}</code></span></div>
          <div class="kv"><span class="k">{{ $t('federation.field_peerURL') }}</span><span class="v"><code>{{ store.lastForward.peerURL || '—' }}</code></span></div>
          <div class="kv"><span class="k">{{ $t('federation.field_status') }}</span><span class="v"><span class="status-pill" :class="store.lastForward.status === 'forwarded' ? 'ok' : 'off'">{{ store.lastForward.status || '—' }}</span></span></div>
          <div class="kv" v-if="store.lastForward.error"><span class="k">{{ $t('common.error') }}</span><span class="v">{{ store.lastForward.error }}</span></div>
        </div>
      </div>
    </div>

    <!-- ========== Tab 4: 联邦任务状态查询 ========== -->
    <div v-show="activeTab === 'taskstatus'">
      <div class="card card--raised">
        <h3>{{ $t('federation.taskstatus_title') }}</h3>
        <p class="muted">{{ $t('federation.taskstatus_desc') }}</p>
        <form @submit.prevent="queryTaskStatus">
          <div class="row-2">
            <div class="field field--fixed">
              <label>{{ $t('federation.field_taskID') }}</label>
              <input v-model="taskStatusForm.taskID" type="text" :placeholder="$t('federation.taskID_placeholder')" required />
            </div>
            <div class="field field--fixed">
              <label>{{ $t('federation.field_peerURL') }}</label>
              <select v-model="taskStatusForm.peerURL" required>
                <option value="">{{ $t('federation.please_select') }}</option>
                <option v-for="p in store.peers" :key="p.url" :value="p.url">{{ p.url }}{{ p.online ? '' : ' (offline)' }}</option>
              </select>
            </div>
          </div>
          <div class="btnbar btnbar--bottom">
            <Button variant="primary" size="md" native-type="submit" :disabled="taskStatusLoading" data-testid="federation-taskstatus-query">
              {{ taskStatusLoading ? $t('common.loading') : $t('federation.taskstatus_query') }}
            </Button>
          </div>
        </form>
      </div>

      <div v-if="taskStatusResult" class="card card--raised result-box">
        <h4>{{ $t('federation.taskstatus_result') }}</h4>
        <div class="kv-grid">
          <div class="kv"><span class="k">{{ $t('federation.field_taskID') }}</span><span class="v"><code>{{ taskStatusResult.taskID || taskStatusForm.taskID }}</code></span></div>
          <div class="kv"><span class="k">{{ $t('federation.field_peerURL') }}</span><span class="v">{{ taskStatusResult.peerURL || taskStatusForm.peerURL }}</span></div>
          <div class="kv"><span class="k">{{ $t('federation.field_status') }}</span><span class="v"><span class="status-pill" :class="taskStatusResult.status === 'success' ? 'ok' : (taskStatusResult.status === 'failed' ? 'off' : '')">{{ taskStatusResult.status || '—' }}</span></span></div>
          <div class="kv"><span class="k">{{ $t('federation.field_agentID') }}</span><span class="v">{{ taskStatusResult.agentID || '—' }}</span></div>
          <div class="kv"><span class="k">{{ $t('federation.field_command') }}</span><span class="v">{{ taskStatusResult.command || '—' }}</span></div>
          <div class="kv"><span class="k">{{ $t('federation.field_result') }}</span><span class="v">{{ taskStatusResult.result || '—' }}</span></div>
          <div class="kv" v-if="taskStatusResult.error"><span class="k">{{ $t('common.error') }}</span><span class="v" style="color: var(--fail)">{{ taskStatusResult.error }}</span></div>
        </div>
      </div>

    </div>

    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="federation-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </PageShell>
</template>

<script setup>
import PageShell from '@/components/PageShell.vue'
import { ref, reactive, computed, onMounted } from 'vue'
import { useFederationStore } from '@/stores/federation'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import { getFederationDevices, getFederationTaskStatus } from '@/api/federation'
import Button from "@/components/ui/Button.vue"

const store = useFederationStore()
const activeTab = ref('peers')
const submitting = ref(false)
const msg = ref('')
const msgOk = ref(false)
const errorConfirm = reactive({ show: false, message: '' })

// 设备列表分页+搜索状态
const deviceSearch = ref('')
const devicePage = ref(1)
const devicePageSize = ref(50)
const deviceTotal = ref(0)
const deviceHasMore = ref(false)
const deviceLoading = ref(false)
const devices = reactive([])

const tabs = [
  { key: 'peers', label: 'federation.tab_peers' },
  { key: 'devices', label: 'federation.tab_devices' },
  { key: 'forward', label: 'federation.tab_forward' },
  { key: 'taskstatus', label: 'federation.tab_taskstatus' }
]

const peerColumns = computed(() => [
  { key: 'url', title: t('federation.field_url'), slot: 'cell-url' },
  { key: 'online', title: t('federation.field_online'), slot: 'cell-online' },
  { key: 'lastCheckAt', title: t('federation.field_lastCheckAt') },
  { key: 'latencyMs', title: t('federation.field_latencyMs'), slot: 'cell-latencyMs' }
])

const deviceColumns = computed(() => [
  { key: 'agentID', title: t('federation.field_agentID'), slot: 'cell-agentID' },
  { key: 'hostname', title: t('federation.field_hostname') },
  { key: 'ip', title: t('federation.field_ip') },
  { key: 'os', title: t('federation.field_os') },
  { key: 'tenant', title: t('federation.field_tenant') },
  { key: 'source', title: t('federation.field_source'), slot: 'cell-source' }
])

const form = ref({ peerURL: '', taskType: 'shell', command: '', deviceID: '', timeoutSec: 30 })
const taskStatusForm = ref({ taskID: '', peerURL: '' })
const taskStatusLoading = ref(false)
const taskStatusResult = ref(null)
const taskStatusError = ref('')

function switchTab(key) {
  activeTab.value = key
  if (key === 'peers' && !store.peers.length) store.fetchPeers()
  else if (key === 'devices' && !devices.length) loadDevices()
  else if (key === 'forward' && !store.peers.length) store.fetchPeers()
  else if (key === 'taskstatus' && !store.peers.length) store.fetchPeers()
}

async function loadDevices() {
  deviceLoading.value = true
  devices.splice(0, devices.length)
  try {
    const resp = await getFederationDevices({
      page: devicePage.value,
      pageSize: devicePageSize.value,
      search: deviceSearch.value
    })
    devices.splice(0, devices.length, ...(resp.data || []))
    deviceTotal.value = resp.total || 0
    deviceHasMore.value = resp.hasMore || false
  } catch (e) {
    store.error = e.j?.error || t('error.federationDevicesFailed')
  } finally {
    deviceLoading.value = false
  }
}

function prevPage() {
  if (devicePage.value > 1) {
    devicePage.value--
    loadDevices()
  }
}

function nextPage() {
  if (deviceHasMore.value) {
    devicePage.value++
    loadDevices()
  }
}

async function onForward() {
  if (submitting.value) return
  submitting.value = true
  msg.value = ''
  try {
    const r = await store.forward({
      peerURL: form.value.peerURL,
      taskType: form.value.taskType,
      command: form.value.command,
      deviceID: form.value.deviceID,
      timeoutSec: form.value.timeoutSec
    })
    msg.value = t('federation.forward_success', { id: r.j?.taskID || '—' })
    msgOk.value = true
  } catch (e) {
    msg.value = t('federation.forward_failed') + (e.j?.error || e.message || '')
    msgOk.value = false
  } finally {
    submitting.value = false
  }
}

async function queryTaskStatus() {
  if (!taskStatusForm.value.taskID || !taskStatusForm.value.peerURL) return
  taskStatusLoading.value = true
  taskStatusError.value = ''
  taskStatusResult.value = null
  try {
    const result = await getFederationTaskStatus(taskStatusForm.value.taskID, taskStatusForm.value.peerURL)
    taskStatusResult.value = result.j || result
  } catch (e) {
    taskStatusError.value = e.j?.error || e.message || t('federation.taskstatus_failed')
  } finally {
    taskStatusLoading.value = false
  }
}

onMounted(() => {
  store.fetchPeers().catch((e) => {
    errorConfirm.message = e.j?.error || t('error.federationPeersFailed')
    errorConfirm.show = true
  })
})
</script>

<style scoped>
.row-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }

.search-input {
  padding: 6px 8px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  font-size: 13px;
  min-width: 240px;
}

.peer-summary { display: flex; flex-wrap: wrap; gap: 8px; margin: 10px 0; }
.peer-chip {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 4px 10px; border-radius: var(--radius-sm);
  border: 1px solid var(--border); background: var(--surface-2);
  font-size: 12px;
}
.peer-chip.ok { border-color: var(--teal); }
.peer-chip.off { border-color: var(--rose); opacity: .7; }
.peer-chip small { color: var(--text-3); }

.kv-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 10px; }
.kv { display: flex; flex-direction: column; gap: 4px; padding: 8px 10px; background: var(--surface-2); border-radius: var(--radius-sm); }
.kv .k { font-size: 11.5px; color: var(--text-3); }
.kv .v { font-size: 13px; color: var(--text); word-break: break-all; }

.page-info {
  display: flex;
  align-items: center;
  gap: 16px;
  margin: 8px 0;
  font-size: 13px;
  color: var(--text-2);
}
.page-info strong { color: var(--text); }

.result-box { margin-top: 12px; padding: 12px; background: var(--muted-soft); border-radius: var(--radius-sm); }

@media (max-width: 768px) {
  .row-2 { grid-template-columns: 1fr; }
  .btnbar { flex-direction: column; align-items: stretch; }
}
</style>