<template>
  <div>
    <h2 data-testid="network-diagnose-title">{{ $t('network.diagnose_title') }}</h2>
    <p class="muted">{{ $t('network.diagnose_subtitle') }}</p>

    <!-- Tab 切换 -->
    <div class="tabbar">
      <button
        v-for="tab in tabs"
        :key="tab.key"
        :class="['tab', { active: activeTab === tab.key }]"
        :data-testid="'network-diagnose-tab-' + tab.key"
        @click="switchTab(tab.key)"
      >{{ $t(tab.label) }}</button>
    </div>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <!-- ========== Tab 1: 诊断工具 ========== -->
    <div v-show="activeTab === 'diagnose'">
      <div class="card">
        <h3>{{ $t('network.diagnose_form_title') }}</h3>
        <form @submit.prevent="onDiagnose">
          <div class="row-2">
            <div class="field">
              <label>{{ $t('network.field_agentID') }}</label>
              <input v-model.trim="diagnoseForm.agentID" type="text" :placeholder="$t('network.agentID_placeholder')" required />
            </div>
            <div class="field">
              <label>{{ $t('network.field_command') }}</label>
              <select v-model="diagnoseForm.command">
                <option value="ping">ping</option>
                <option value="traceroute">traceroute</option>
                <option value="tcping">tcping</option>
                <option value="nslookup">nslookup</option>
                <option value="curl">curl</option>
              </select>
            </div>
          </div>
          <div class="field">
            <label>{{ $t('network.field_target') }}</label>
            <input v-model.trim="diagnoseForm.target" type="text" :placeholder="$t('network.target_placeholder')" required />
          </div>
          <div class="row-2">
            <div class="field">
              <label>{{ $t('network.field_count') }}</label>
              <input v-model.number="diagnoseForm.count" type="number" min="1" />
            </div>
            <div class="field">
              <label>{{ $t('network.field_timeout') }}</label>
              <input v-model.number="diagnoseForm.timeout" type="number" min="1" />
            </div>
          </div>
          <div class="btnbar">
            <button type="submit" class="primary" :disabled="submitting" data-testid="network-diagnose-btn">
              {{ submitting ? $t('common.loading') : $t('network.diagnose') }}
            </button>
            <button type="button" class="outline" @click="pollResult" :disabled="!store.diagnoseTask || polling">
              {{ polling ? $t('common.loading') : $t('network.poll_result') }}
            </button>
          </div>
          <p v-if="diagnoseMsg" :class="['msg', diagnoseMsgOk ? 'ok' : 'err']">{{ diagnoseMsg }}</p>
        </form>
      </div>

      <!-- 诊断任务信息 -->
      <div class="card" v-if="store.diagnoseTask">
        <h3>{{ $t('network.diagnose_task_title') }}</h3>
        <div class="kv-grid">
          <div class="kv"><span class="k">{{ $t('network.field_taskID') }}</span><span class="v"><code>{{ store.diagnoseTask.taskID || '—' }}</code></span></div>
          <div class="kv"><span class="k">{{ $t('network.field_status') }}</span><span class="v">{{ store.diagnoseTask.status || '—' }}</span></div>
        </div>
      </div>

      <!-- 诊断结果 -->
      <div class="card" v-if="store.diagnoseResult">
        <h3>{{ $t('network.diagnose_result_title') }}</h3>
        <div class="kv-grid">
          <div class="kv"><span class="k">{{ $t('network.field_taskID') }}</span><span class="v"><code>{{ store.diagnoseResult.taskID || '—' }}</code></span></div>
          <div class="kv"><span class="k">{{ $t('network.field_status') }}</span><span class="v">{{ store.diagnoseResult.status || '—' }}</span></div>
          <div class="kv"><span class="k">{{ $t('network.field_finishedAt') }}</span><span class="v">{{ store.diagnoseResult.finishedAt || '—' }}</span></div>
        </div>
        <h4>{{ $t('network.field_output') }}</h4>
        <pre class="output-pre">{{ store.diagnoseResult.output || '—' }}</pre>
      </div>
    </div>

    <!-- ========== Tab 2: 批量连通性检测 ========== -->
    <div v-show="activeTab === 'connectivity'">
      <div class="card">
        <h3>{{ $t('network.connectivity_form_title') }}</h3>
        <form @submit.prevent="onConnectivity">
          <div class="field">
            <label>{{ $t('network.field_targets') }}</label>
            <textarea v-model="connectivityForm.targetsRaw" rows="4" :placeholder="$t('network.targets_placeholder')" required></textarea>
            <small class="hint">{{ $t('network.targets_hint') }}</small>
          </div>
          <div class="field">
            <label>{{ $t('network.field_timeout') }}</label>
            <input v-model.number="connectivityForm.timeout" type="number" min="1" />
          </div>
          <div class="btnbar">
            <button type="submit" class="primary" :disabled="submitting" data-testid="network-connectivity-btn">
              {{ submitting ? $t('common.loading') : $t('network.check_connectivity') }}
            </button>
          </div>
          <p v-if="connectivityMsg" :class="['msg', connectivityMsgOk ? 'ok' : 'err']">{{ connectivityMsg }}</p>
        </form>
      </div>

      <div v-if="store.loading && !store.connectivity.length" class="muted">{{ $t('common.loading') }}</div>
      <DataTable
        v-else-if="store.connectivity.length"
        :columns="connectivityColumns"
        :rows="store.connectivity"
        row-key="source"
        :empty-text="$t('network.empty_connectivity')"
      >
        <template #cell-reachable="{ value }">
          <span class="status-pill" :class="value ? 'ok' : 'off'">{{ value ? $t('network.reachable') : $t('network.unreachable') }}</span>
        </template>
        <template #cell-latencyMs="{ value }">{{ value != null ? value + ' ms' : '—' }}</template>
        <template #cell-loss="{ value }">{{ value != null ? value : '—' }}</template>
      </DataTable>
    </div>

    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="network-diagnose-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// 网络诊断页 — 两 Tab：诊断工具表单（选 agent + 命令 + 目标 + 参数）+ 结果展示 / 批量连通性检测
import { ref, reactive, computed, onMounted } from 'vue'
import { useNetworkStore } from '@/stores/network'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'

const store = useNetworkStore()
const activeTab = ref('diagnose')
const submitting = ref(false)
const polling = ref(false)
const errorConfirm = reactive({ show: false, message: '' })

const tabs = [
  { key: 'diagnose', label: 'network.tab_diagnose' },
  { key: 'connectivity', label: 'network.tab_connectivity' }
]

const diagnoseForm = ref({ agentID: '', command: 'ping', target: '', count: 4, timeout: 10 })
const connectivityForm = ref({ targetsRaw: '', timeout: 5 })
const diagnoseMsg = ref('')
const diagnoseMsgOk = ref(false)
const connectivityMsg = ref('')
const connectivityMsgOk = ref(false)

const connectivityColumns = computed(() => [
  { key: 'source', title: t('network.field_source') },
  { key: 'target', title: t('network.field_target') },
  { key: 'reachable', title: t('network.field_reachable'), slot: 'cell-reachable' },
  { key: 'latencyMs', title: t('network.field_latencyMs'), slot: 'cell-latencyMs' },
  { key: 'loss', title: t('network.field_loss'), slot: 'cell-loss' }
])

function switchTab(key) {
  activeTab.value = key
}

async function onDiagnose() {
  if (submitting.value) return
  submitting.value = true
  diagnoseMsg.value = ''
  try {
    const r = await store.startDiagnose({
      agentID: diagnoseForm.value.agentID,
      command: diagnoseForm.value.command,
      target: diagnoseForm.value.target,
      count: diagnoseForm.value.count,
      timeout: diagnoseForm.value.timeout
    })
    diagnoseMsg.value = t('network.diagnose_started', { id: r.j?.taskID || '—' })
    diagnoseMsgOk.value = true
  } catch (e) {
    diagnoseMsg.value = t('network.diagnose_failed') + (e.j?.error || e.message || '')
    diagnoseMsgOk.value = false
  } finally {
    submitting.value = false
  }
}

async function pollResult() {
  if (!store.diagnoseTask || !store.diagnoseTask.taskID) return
  polling.value = true
  try {
    await store.fetchDiagnoseResult(store.diagnoseTask.taskID)
  } catch (e) {
    errorConfirm.message = e.j?.error || t('error.networkDiagnoseFailed')
    errorConfirm.show = true
  } finally {
    polling.value = false
  }
}

async function onConnectivity() {
  if (submitting.value) return
  submitting.value = true
  connectivityMsg.value = ''
  // 解析目标对：每行 "source,target" 或 "source target"
  const targets = connectivityForm.value.targetsRaw
    .split(/\n/)
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const parts = line.split(/[,\s]+/)
      return { source: parts[0] || '', target: parts[1] || '' }
    })
    .filter((p) => p.source && p.target)
  if (!targets.length) {
    connectivityMsg.value = t('network.targets_invalid')
    connectivityMsgOk.value = false
    submitting.value = false
    return
  }
  try {
    await store.checkConnectivity({
      targets,
      timeout: connectivityForm.value.timeout
    })
    connectivityMsg.value = t('network.connectivity_done')
    connectivityMsgOk.value = true
  } catch (e) {
    connectivityMsg.value = t('network.connectivity_failed') + (e.j?.error || e.message || '')
    connectivityMsgOk.value = false
  } finally {
    submitting.value = false
  }
}

onMounted(() => {})
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
.tab:hover { color: var(--text); }

.card {
  background: var(--surface); border: 1px solid var(--border);
  border-radius: var(--radius); padding: 16px; margin-top: 14px;
  box-shadow: var(--shadow);
}
.card h3 { margin: 0 0 12px; font-size: 14px; }
.card h4 { margin: 14px 0 6px; font-size: 12.5px; color: var(--text-2); }
.btnbar { display: flex; gap: 8px; margin-top: 8px; }
.field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 10px; min-width: 220px; }
.field label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.field .hint { font-size: 11.5px; color: var(--text-3); }
.row-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }

.kv-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 10px; }
.kv { display: flex; flex-direction: column; gap: 4px; padding: 8px 10px; background: var(--surface-2); border-radius: var(--radius-sm); }
.kv .k { font-size: 11.5px; color: var(--text-3); }
.kv .v { font-size: 13px; color: var(--text); word-break: break-all; }

.output-pre {
  background: var(--surface-2); border: 1px solid var(--border); border-radius: var(--radius-sm);
  padding: 10px 12px; font-size: 12px; color: var(--text); white-space: pre-wrap; word-break: break-all;
  max-height: 320px; overflow: auto; margin: 0;
}

.status-pill {
  display: inline-flex; align-items: center;
  padding: 1px 9px; border-radius: 999px; font-size: 12px; font-weight: 600;
}
.status-pill.ok { background: var(--accent-soft); color: var(--accent); }
.status-pill.off { background: var(--surface-3); color: var(--text-3); }

.poll-err {
  padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}

@media (max-width: 768px) { .row-2 { grid-template-columns: 1fr; } }
</style>