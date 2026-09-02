<template>
  <div>
    <h2 data-testid="federation-title">{{ $t('federation.title') }}</h2>
    <p class="muted">{{ $t('federation.subtitle') }}</p>

    <!-- Tab 切换 -->
    <div class="tabbar">
      <button
        v-for="tab in tabs"
        :key="tab.key"
        :class="['tab', { active: activeTab === tab.key }]"
        :data-testid="'federation-tab-' + tab.key"
        @click="switchTab(tab.key)"
      >{{ $t(tab.label) }}</button>
    </div>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <!-- ========== Tab 1: Peer 管理 ========== -->
    <div v-show="activeTab === 'peers'">
      <div class="btnbar">
        <button class="outline" @click="store.fetchPeers()"><Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}</button>
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

    <!-- ========== Tab 2: 设备聚合视图 ========== -->
    <div v-show="activeTab === 'devices'">
      <div class="btnbar">
        <button class="outline" @click="store.fetchDevices()"><Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}</button>
      </div>
      <div v-if="store.devicePeers.length" class="peer-summary">
        <span v-for="p in store.devicePeers" :key="p.url" class="peer-chip" :class="p.online ? 'ok' : 'off'">
          <code>{{ p.url }}</code>
          <small>{{ p.online ? $t('federation.online') : $t('federation.offline') }} · {{ p.deviceCount ?? 0 }}</small>
        </span>
      </div>
      <div v-if="store.loading && !store.devices.length" class="muted">{{ $t('common.loading') }}</div>
      <DataTable
        v-else
        :columns="deviceColumns"
        :rows="store.devices"
        row-key="agentID"
        :loading="store.loading"
        :empty-text="$t('federation.empty_devices')"
      >
        <template #cell-agentID="{ value }"><code>{{ value }}</code></template>
      </DataTable>
    </div>

    <!-- ========== Tab 3: 任务转发 ========== -->
    <div v-show="activeTab === 'forward'">
      <div class="card">
        <h3>{{ $t('federation.forward_form_title') }}</h3>
        <form @submit.prevent="onForward">
          <div class="field">
            <label>{{ $t('federation.field_peerURL') }}</label>
            <select v-model="form.peerURL" required>
              <option value="">{{ $t('federation.please_select') }}</option>
              <option v-for="p in store.peers" :key="p.url" :value="p.url">{{ p.url }}{{ p.online ? '' : ' (offline)' }}</option>
            </select>
          </div>
          <div class="row-2">
            <div class="field">
              <label>{{ $t('federation.field_taskType') }}</label>
              <select v-model="form.taskType">
                <option value="shell">shell</option>
                <option value="file">file</option>
                <option value="service">service</option>
              </select>
            </div>
            <div class="field">
              <label>{{ $t('federation.field_deviceID') }}</label>
              <input v-model.trim="form.deviceID" type="text" :placeholder="$t('federation.deviceID_placeholder')" />
            </div>
          </div>
          <div class="field">
            <label>{{ $t('federation.field_command') }}</label>
            <input v-model.trim="form.command" type="text" :placeholder="$t('federation.command_placeholder')" required />
          </div>
          <div class="field">
            <label>{{ $t('federation.field_timeoutSec') }}</label>
            <input v-model.number="form.timeoutSec" type="number" min="1" />
          </div>
          <div class="btnbar">
            <button type="submit" class="primary" :disabled="submitting" data-testid="federation-forward-btn">
              {{ submitting ? $t('common.loading') : $t('federation.forward') }}
            </button>
          </div>
          <p v-if="msg" :class="['msg', msgOk ? 'ok' : 'err']">{{ msg }}</p>
        </form>
      </div>

      <!-- 最近一次转发结果 -->
      <div class="card" v-if="store.lastForward">
        <h3>{{ $t('federation.forward_result_title') }}</h3>
        <div class="kv-grid">
          <div class="kv"><span class="k">{{ $t('federation.field_taskID') }}</span><span class="v"><code>{{ store.lastForward.taskID || '—' }}</code></span></div>
          <div class="kv"><span class="k">{{ $t('federation.field_peerURL') }}</span><span class="v"><code>{{ store.lastForward.peerURL || '—' }}</code></span></div>
          <div class="kv"><span class="k">{{ $t('federation.field_status') }}</span><span class="v"><span class="status-pill" :class="store.lastForward.status === 'forwarded' ? 'ok' : 'off'">{{ store.lastForward.status || '—' }}</span></span></div>
          <div class="kv" v-if="store.lastForward.error"><span class="k">{{ $t('common.error') }}</span><span class="v">{{ store.lastForward.error }}</span></div>
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
  </div>
</template>

<script setup>
// 控制面联邦页 — 三 Tab：Peer 管理 / 设备聚合 / 任务转发
import { ref, reactive, computed, onMounted } from 'vue'
import { useFederationStore } from '@/stores/federation'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'

const store = useFederationStore()
const activeTab = ref('peers')
const submitting = ref(false)
const msg = ref('')
const msgOk = ref(false)
const errorConfirm = reactive({ show: false, message: '' })

const tabs = [
  { key: 'peers', label: 'federation.tab_peers' },
  { key: 'devices', label: 'federation.tab_devices' },
  { key: 'forward', label: 'federation.tab_forward' }
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
  { key: 'tenant', title: t('federation.field_tenant') }
])

const form = ref({ peerURL: '', taskType: 'shell', command: '', deviceID: '', timeoutSec: 30 })

function switchTab(key) {
  activeTab.value = key
  if (key === 'peers' && !store.peers.length) store.fetchPeers()
  else if (key === 'devices' && !store.devices.length) store.fetchDevices()
  else if (key === 'forward' && !store.peers.length) store.fetchPeers()
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

onMounted(() => {
  store.fetchPeers().catch((e) => {
    errorConfirm.message = e.j?.error || t('error.federationPeersFailed')
    errorConfirm.show = true
  })
})
</script>

<style scoped>
.card {
  background: var(--surface); border: 1px solid var(--border);
  border-radius: var(--radius); padding: 16px; margin-top: 14px;
  box-shadow: var(--shadow);
}
.card h3 { margin: 0 0 12px; font-size: 14px; }
.btnbar { display: flex; gap: 8px; margin-top: 8px; }
.field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 10px; min-width: 220px; }
.field label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.row-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }

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

.poll-err {
  padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}

@media (max-width: 768px) {
  .row-2 { grid-template-columns: 1fr; }
}
</style>