<template>
  <PageShell :title="$t('network.devices_title')" :desc="$t('network.devices_subtitle')" testid="network-devices" :error="store.error">
    <!-- Tab 切换 -->
    <div class="tabbar">
      <Button
        variant="primary" size="md" v-for="tab in tabs"
        :key="tab.key"
        :class="['tab', { active: activeTab === tab.key }]"
        :data-testid="'network-devices-tab-' + tab.key"
        @click="switchTab(tab.key)"
      >{{ $t(tab.label) }}</Button>
    </div>

    <!-- ========== Tab 1: 设备列表 ========== -->
    <div v-show="activeTab === 'devices'">
      <div class="btnbar btnbar--bottom">
        <Button variant="primary" size="md" @click="openCreate" data-testid="network-device-create-btn">
          <Icon name="add" :size="14" /> {{ $t('network.add_device') }}
        </Button>
        <Button variant="ghost" size="md" @click="store.fetchDevices()"><Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}</Button>
      </div>
      <div v-if="store.loading && !store.devices.length" class="muted">{{ $t('common.loading') }}</div>
      <DataTable
        v-else
        :columns="deviceColumns"
        :rows="store.devices"
        row-key="id"
        :loading="store.loading"
        :empty-text="$t('network.empty_devices')"
      >
        <template #cell-status="{ value }">
          <span class="status-pill" :class="value === 'online' ? 'ok' : 'off'">{{ value || '—' }}</span>
        </template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <Button variant="ghost" size="sm" @click="viewMetrics(row)" data-testid="network-device-metrics-btn"><Icon name="logs" :size="13" /></Button>
            <Button variant="ghost" size="sm" @click="openConfigPush(row)" data-testid="network-device-config-btn"><Icon name="settings" :size="13" /></Button>
            <Button variant="danger" size="sm" @click="onDelete(row)" data-testid="network-device-delete-btn"><Icon name="delete" :size="13" /></Button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- ========== Tab 2: 网络发现 ========== -->
    <div v-show="activeTab === 'discover'">
      <div class="card card--raised">
        <h3>{{ $t('network.discover_form_title') }}</h3>
        <form @submit.prevent="onDiscover">
          <div class="row-2">
            <div class="field">
              <label>{{ $t('network.field_segment') }}</label>
              <input v-model.trim="discoverForm.segment" type="text" :placeholder="$t('network.segment_placeholder')" required />
            </div>
            <div class="field">
              <label>{{ $t('network.field_agentID') }}</label>
              <input v-model.trim="discoverForm.agentID" type="text" :placeholder="$t('network.agentID_placeholder')" required />
            </div>
          </div>
          <div class="btnbar btnbar--bottom">
            <Button variant="primary" size="md" native-type="submit" :disabled="submitting" data-testid="network-discover-btn">
              {{ submitting ? $t('common.loading') : $t('network.discover') }}
            </Button>
          </div>
          <p v-if="discoverMsg" :class="['msg', discoverMsgOk ? 'ok' : 'err']">{{ discoverMsg }}</p>
        </form>
      </div>

      <div v-if="store.loading && !store.discovered.length" class="muted">{{ $t('common.loading') }}</div>
      <DataTable
        v-else-if="store.discovered.length"
        :columns="discoveredColumns"
        :rows="store.discovered"
        row-key="id"
        :empty-text="$t('network.empty_discovered')"
      >
        <template #cell-status="{ value }">
          <span class="status-pill" :class="value === 'online' ? 'ok' : 'off'">{{ value || '—' }}</span>
        </template>
      </DataTable>
    </div>

    <!-- 新增设备抽屉 -->
    <DetailDrawer :open="!!form" :title="$t('network.add_device')" @close="form = null">
      <form v-if="form" class="entity-form" @submit.prevent="onSave">
        <div class="row-2">
          <div class="field">
            <label>{{ $t('network.field_name') }}</label>
            <input v-model.trim="form.name" type="text" required />
          </div>
          <div class="field">
            <label>{{ $t('network.field_type') }}</label>
            <select v-model="form.type">
              <option value="router">router</option>
              <option value="switch">switch</option>
              <option value="firewall">firewall</option>
              <option value="load-balancer">load-balancer</option>
              <option value="server">server</option>
            </select>
          </div>
        </div>
        <div class="row-2">
          <div class="field">
            <label>{{ $t('network.field_ip') }}</label>
            <input v-model.trim="form.ip" type="text" placeholder="10.0.0.1" required />
          </div>
          <div class="field">
            <label>{{ $t('network.field_segment') }}</label>
            <input v-model.trim="form.segment" type="text" placeholder="10.0.0.0/24" />
          </div>
        </div>
        <div class="row-2">
          <div class="field">
            <label>{{ $t('network.field_vendor') }}</label>
            <input v-model.trim="form.vendor" type="text" />
          </div>
          <div class="field">
            <label>{{ $t('network.field_model') }}</label>
            <input v-model.trim="form.model" type="text" />
          </div>
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar btnbar--bottom">
          <Button variant="primary" size="md" native-type="submit"><Icon name="success" :size="14" /> {{ $t('common.save') }}</Button>
          <Button variant="ghost" size="md" native-type="button" @click="form = null">{{ $t('common.cancel') }}</Button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 配置下发抽屉 -->
    <DetailDrawer :open="!!configForm" :title="$t('network.config_push_title')" @close="configForm = null">
      <form v-if="configForm" class="entity-form" @submit.prevent="onConfigPush">
        <div class="field">
          <label>{{ $t('network.field_device') }}</label>
          <input :value="configForm.deviceName" type="text" disabled />
        </div>
        <div class="field">
          <label>{{ $t('network.field_format') }}</label>
          <select v-model="configForm.format">
            <option value="json">json</option>
            <option value="yaml">yaml</option>
            <option value="text">text</option>
          </select>
        </div>
        <div class="field">
          <label>{{ $t('network.field_config') }}</label>
          <textarea v-model="configForm.config" rows="6" :placeholder="$t('network.config_placeholder')" required></textarea>
        </div>
        <div v-if="configError" class="msg err">{{ configError }}</div>
        <div class="btnbar btnbar--bottom">
          <Button variant="primary" size="md" native-type="submit"><Icon name="success" :size="14" /> {{ $t('network.push_config') }}</Button>
          <Button variant="ghost" size="md" native-type="button" @click="configForm = null">{{ $t('common.cancel') }}</Button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 设备指标抽屉 -->
    <DetailDrawer :open="metricsOpen" :title="$t('network.metrics_title')" @close="metricsOpen = false">
      <div v-if="store.loading" class="muted">{{ $t('common.loading') }}</div>
      <DataTable
        v-else
        :columns="metricsColumns"
        :rows="store.deviceMetrics"
        row-key="timestamp"
        :empty-text="$t('network.empty_metrics')"
      />
    </DetailDrawer>

    <!-- 删除确认 -->
    <ConfirmModal
      v-model="deleteConfirm.show"
      data-testid="network-device-delete-confirm-modal"
      :title="$t('common.delete')"
      :message="$t('network.confirm_delete_device')"
      @confirm="onDeleteConfirm"
    />
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="network-devices-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </PageShell>
</template>

<script setup>
// 网络设备页 — 两 Tab：设备列表 CRUD + 指标查看 + 配置下发 / 网络发现
import PageShell from '@/components/PageShell.vue'
import { ref, reactive, computed, onMounted } from 'vue'
import { useNetworkStore } from '@/stores/network'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import Button from "@/components/ui/Button.vue"

const store = useNetworkStore()
const activeTab = ref('devices')
const submitting = ref(false)
const formError = ref('')
const configError = ref('')
const deleteConfirm = reactive({ show: false, id: null })
const errorConfirm = reactive({ show: false, message: '' })

const tabs = [
  { key: 'devices', label: 'network.tab_devices' },
  { key: 'discover', label: 'network.tab_discover' }
]

const deviceColumns = computed(() => [
  { key: 'name', title: t('network.field_name') },
  { key: 'type', title: t('network.field_type') },
  { key: 'ip', title: t('network.field_ip') },
  { key: 'segment', title: t('network.field_segment') },
  { key: 'vendor', title: t('network.field_vendor') },
  { key: 'model', title: t('network.field_model') },
  { key: 'status', title: t('network.field_status'), slot: 'cell-status' },
  { key: 'createdAt', title: t('network.field_createdAt') },
  { key: 'actions', title: t('network.col_actions'), slot: 'cell-actions', width: '110px' }
])

const discoveredColumns = computed(() => [
  { key: 'name', title: t('network.field_name') },
  { key: 'type', title: t('network.field_type') },
  { key: 'ip', title: t('network.field_ip') },
  { key: 'segment', title: t('network.field_segment') },
  { key: 'vendor', title: t('network.field_vendor') },
  { key: 'status', title: t('network.field_status'), slot: 'cell-status' }
])

const metricsColumns = computed(() => [
  { key: 'timestamp', title: t('network.field_timestamp') },
  { key: 'bandwidth', title: t('network.field_bandwidth') },
  { key: 'throughput', title: t('network.field_throughput') },
  { key: 'errors', title: t('network.field_errors') }
])

function switchTab(key) {
  activeTab.value = key
  if (key === 'devices' && !store.devices.length) store.fetchDevices()
}

// ---------- 新增设备 ----------
const form = ref(null)

function openCreate() {
  formError.value = ''
  form.value = { name: '', type: 'router', ip: '', segment: '', vendor: '', model: '' }
}

async function onSave() {
  formError.value = ''
  try {
    await store.createDevice({
      name: form.value.name,
      type: form.value.type,
      ip: form.value.ip,
      segment: form.value.segment,
      vendor: form.value.vendor,
      model: form.value.model
    })
    form.value = null
    toast.success(t('network.device_saved'))
  } catch (e) {
    formError.value = e.j?.error || t('network.device_save_failed')
  }
}

function onDelete(row) {
  deleteConfirm.id = row.id
  deleteConfirm.show = true
}

async function onDeleteConfirm() {
  const { id } = deleteConfirm
  if (!id) return
  try {
    await store.removeDevice(id)
    toast.success(t('network.device_deleted'))
  } catch (e) {
    errorConfirm.message = e.j?.error || t('network.device_delete_failed')
    errorConfirm.show = true
  }
}

// ---------- 配置下发 ----------
const configForm = ref(null)

function openConfigPush(row) {
  configError.value = ''
  configForm.value = { id: row.id, deviceName: row.name || row.id, format: 'json', config: '' }
}

async function onConfigPush() {
  configError.value = ''
  try {
    const r = await store.pushConfig(configForm.value.id, {
      config: configForm.value.config,
      format: configForm.value.format
    })
    configForm.value = null
    toast.success(t('network.config_pushed', { id: r.j?.taskID || '—' }))
  } catch (e) {
    configError.value = e.j?.error || t('network.config_push_failed')
  }
}

// ---------- 设备指标 ----------
const metricsOpen = ref(false)

async function viewMetrics(row) {
  metricsOpen.value = true
  await store.fetchDeviceMetrics(row.id)
}

// ---------- 网络发现 ----------
const discoverForm = ref({ segment: '', agentID: '' })
const discoverMsg = ref('')
const discoverMsgOk = ref(false)

async function onDiscover() {
  if (submitting.value) return
  submitting.value = true
  discoverMsg.value = ''
  try {
    await store.discover({
      segment: discoverForm.value.segment,
      agentID: discoverForm.value.agentID
    })
    discoverMsg.value = t('network.discover_done', { n: store.discovered.length })
    discoverMsgOk.value = true
  } catch (e) {
    discoverMsg.value = t('network.discover_failed') + (e.j?.error || e.message || '')
    discoverMsgOk.value = false
  } finally {
    submitting.value = false
  }
}

onMounted(() => {
  store.fetchDevices().catch((e) => {
    errorConfirm.message = e.j?.error || t('error.networkDevicesFailed')
    errorConfirm.show = true
  })
})
</script>

<style scoped>
.tabbar {
  display: flex; gap: 4px; margin-bottom: 16px;
  border-bottom: 1px solid var(--border);
}
.tab {
  background: transparent; border: none; border-bottom: 2px solid transparent;
  padding: 8px 16px; cursor: pointer; color: var(--text-2);
  font-size: 13px; font-weight: 500;
}
.tab.active { color: var(--accent); border-bottom-color: var(--accent); }

.status-pill {
  display: inline-flex; align-items: center;
  padding: 1px 9px; border-radius: 999px; font-size: 12px; font-weight: 600;
}

.entity-form { display: flex; flex-direction: column; gap: 12px; }
.row-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }

@media (max-width: 768px) { .row-2 { grid-template-columns: 1fr; } }
</style>