<template>
  <div>
    <h2 data-testid="config-hotpush-title">{{ $t('configHotpush.title') }}</h2>
    <p class="muted">{{ $t('configHotpush.subtitle') }}</p>

    <!-- Tab 切换 -->
    <div class="tabbar">
      <button
        v-for="tab in tabs"
        :key="tab.key"
        :class="['tab', { active: activeTab === tab.key }]"
        :data-testid="'config-tab-' + tab.key"
        @click="switchTab(tab.key)"
      >{{ $t(tab.label) }}</button>
    </div>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <!-- ========== Tab 1: 热推送 ========== -->
    <div v-show="activeTab === 'hotpush'">
      <div class="card">
        <h3>{{ $t('configHotpush.hotpush_form_title') }}</h3>
        <form @submit.prevent="onHotpush">
          <div class="row-2">
            <div class="field">
              <label>{{ $t('configHotpush.field_agentID') }}</label>
              <input v-model.trim="hotpushForm.agentID" type="text" :placeholder="$t('configHotpush.agentID_placeholder')" required />
            </div>
            <div class="field">
              <label>{{ $t('configHotpush.field_key') }}</label>
              <input v-model.trim="hotpushForm.key" type="text" :placeholder="$t('configHotpush.key_placeholder')" required />
            </div>
          </div>
          <div class="field">
            <label>{{ $t('configHotpush.field_value') }}</label>
            <textarea v-model="hotpushForm.value" rows="3" :placeholder="$t('configHotpush.value_placeholder')" required></textarea>
          </div>
          <div class="row-2">
            <div class="field">
              <label>{{ $t('configHotpush.field_path') }}</label>
              <input v-model.trim="hotpushForm.path" type="text" :placeholder="$t('configHotpush.path_placeholder')" />
            </div>
            <div class="field">
              <label>{{ $t('configHotpush.field_format') }}</label>
              <select v-model="hotpushForm.format">
                <option value="json">json</option>
                <option value="yaml">yaml</option>
                <option value="toml">toml</option>
                <option value="ini">ini</option>
                <option value="text">text</option>
              </select>
            </div>
          </div>
          <div class="field">
            <label>{{ $t('configHotpush.field_description') }}</label>
            <input v-model.trim="hotpushForm.description" type="text" :placeholder="$t('configHotpush.description_placeholder')" />
          </div>
          <div class="btnbar">
            <button type="submit" class="primary" :disabled="submitting" data-testid="config-hotpush-btn">
              {{ submitting ? $t('common.loading') : $t('configHotpush.hotpush') }}
            </button>
          </div>
          <p v-if="hotpushMsg" :class="['msg', hotpushMsgOk ? 'ok' : 'err']">{{ hotpushMsg }}</p>
        </form>
      </div>
      <div class="card" v-if="store.lastHotpush">
        <h3>{{ $t('configHotpush.result_title') }}</h3>
        <div class="kv-grid">
          <div class="kv"><span class="k">{{ $t('configHotpush.field_taskID') }}</span><span class="v"><code>{{ store.lastHotpush.taskID || '—' }}</code></span></div>
          <div class="kv" v-if="store.lastHotpush.configVersion">
            <span class="k">{{ $t('configHotpush.field_version') }}</span>
            <span class="v">{{ store.lastHotpush.configVersion.version ?? '—' }}</span>
          </div>
        </div>
      </div>
    </div>

    <!-- ========== Tab 2: 灰度发布 ========== -->
    <div v-show="activeTab === 'canary'">
      <div class="card">
        <h3>{{ $t('configHotpush.canary_form_title') }}</h3>
        <form @submit.prevent="onCanary">
          <div class="field">
            <label>{{ $t('configHotpush.field_agentIDs') }}</label>
            <textarea v-model.trim="canaryForm.agentIDsRaw" rows="2" :placeholder="$t('configHotpush.agentIDs_placeholder')" required></textarea>
          </div>
          <div class="row-2">
            <div class="field">
              <label>{{ $t('configHotpush.field_key') }}</label>
              <input v-model.trim="canaryForm.key" type="text" required />
            </div>
            <div class="field">
              <label>{{ $t('configHotpush.field_format') }}</label>
              <select v-model="canaryForm.format">
                <option value="json">json</option>
                <option value="yaml">yaml</option>
                <option value="toml">toml</option>
                <option value="ini">ini</option>
                <option value="text">text</option>
              </select>
            </div>
          </div>
          <div class="field">
            <label>{{ $t('configHotpush.field_value') }}</label>
            <textarea v-model="canaryForm.value" rows="3" required></textarea>
          </div>
          <div class="row-2">
            <div class="field">
              <label>{{ $t('configHotpush.field_path') }}</label>
              <input v-model.trim="canaryForm.path" type="text" />
            </div>
            <div class="field">
              <label>{{ $t('configHotpush.field_strategy') }}</label>
              <select v-model="canaryForm.strategy">
                <option value="rollout">rollout</option>
                <option value="blue-green">blue-green</option>
                <option value="ab">a/b</option>
              </select>
            </div>
          </div>
          <div class="field">
            <label>{{ $t('configHotpush.field_percentage') }}</label>
            <input v-model.number="canaryForm.percentage" type="number" min="0" max="100" />
          </div>
          <div class="btnbar">
            <button type="submit" class="primary" :disabled="submitting" data-testid="config-canary-btn">
              {{ submitting ? $t('common.loading') : $t('configHotpush.canary') }}
            </button>
          </div>
          <p v-if="canaryMsg" :class="['msg', canaryMsgOk ? 'ok' : 'err']">{{ canaryMsg }}</p>
        </form>
      </div>
      <div class="card" v-if="store.lastCanary">
        <h3>{{ $t('configHotpush.canary_result_title') }}</h3>
        <div class="kv"><span class="k">{{ $t('configHotpush.field_canaryID') }}</span><span class="v"><code>{{ store.lastCanary.canaryID || '—' }}</code></span></div>
      </div>
    </div>

    <!-- ========== Tab 3: 版本历史 ========== -->
    <div v-show="activeTab === 'versions'">
      <div class="filter-bar">
        <input v-model.trim="filter.key" :placeholder="$t('configHotpush.filter_key')" class="filter-input" />
        <input v-model.trim="filter.agentID" :placeholder="$t('configHotpush.filter_agentID')" class="filter-input" />
        <input v-model.number="filter.limit" type="number" min="1" max="500" :placeholder="$t('configHotpush.filter_limit')" class="filter-input narrow" />
        <button class="primary" @click="searchVersions" data-testid="config-versions-search-btn">{{ $t('common.search') }}</button>
        <button class="outline" @click="resetFilter">{{ $t('configHotpush.reset') }}</button>
      </div>
      <div v-if="store.loading && !store.versions.length" class="muted">{{ $t('common.loading') }}</div>
      <DataTable
        v-else
        :columns="versionColumns"
        :rows="store.versions"
        row-key="version"
        :loading="store.loading"
        :empty-text="$t('configHotpush.empty_versions')"
      >
        <template #cell-key="{ value }"><code>{{ value }}</code></template>
        <template #cell-agentID="{ value }"><code>{{ value }}</code></template>
        <template #cell-value="{ value }"><code class="value-cell">{{ truncate(value) }}</code></template>
      </DataTable>
    </div>

    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="config-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// 配置热推送页 — 三 Tab：热推送 / 灰度发布 / 版本历史
import { ref, reactive, computed, onMounted } from 'vue'
import { useConfigStore } from '@/stores/config'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'

const store = useConfigStore()
const activeTab = ref('hotpush')
const submitting = ref(false)
const errorConfirm = reactive({ show: false, message: '' })

const tabs = [
  { key: 'hotpush', label: 'configHotpush.tab_hotpush' },
  { key: 'canary', label: 'configHotpush.tab_canary' },
  { key: 'versions', label: 'configHotpush.tab_versions' }
]

const hotpushForm = ref({ agentID: '', key: '', value: '', path: '', format: 'json', description: '' })
const canaryForm = ref({ agentIDsRaw: '', key: '', value: '', path: '', format: 'json', strategy: 'rollout', percentage: 10 })
const hotpushMsg = ref('')
const hotpushMsgOk = ref(false)
const canaryMsg = ref('')
const canaryMsgOk = ref(false)
const filter = ref({ key: '', agentID: '', limit: 50 })

const versionColumns = computed(() => [
  { key: 'key', title: t('configHotpush.field_key'), slot: 'cell-key' },
  { key: 'version', title: t('configHotpush.field_version') },
  { key: 'agentID', title: t('configHotpush.field_agentID'), slot: 'cell-agentID' },
  { key: 'value', title: t('configHotpush.field_value'), slot: 'cell-value' },
  { key: 'updatedBy', title: t('configHotpush.field_updatedBy') },
  { key: 'updatedAt', title: t('configHotpush.field_updatedAt') }
])

function truncate(v) {
  const s = typeof v === 'string' ? v : JSON.stringify(v)
  return s && s.length > 60 ? s.slice(0, 60) + '…' : s
}

function switchTab(key) {
  activeTab.value = key
  if (key === 'versions' && !store.versions.length) store.fetchVersions({ limit: 50 })
}

async function onHotpush() {
  if (submitting.value) return
  submitting.value = true
  hotpushMsg.value = ''
  try {
    const r = await store.hotpush({
      agentID: hotpushForm.value.agentID,
      key: hotpushForm.value.key,
      value: hotpushForm.value.value,
      path: hotpushForm.value.path,
      format: hotpushForm.value.format,
      description: hotpushForm.value.description
    })
    hotpushMsg.value = t('configHotpush.hotpush_success', { id: r.j?.taskID || '—' })
    hotpushMsgOk.value = true
  } catch (e) {
    hotpushMsg.value = t('configHotpush.hotpush_failed') + (e.j?.error || e.message || '')
    hotpushMsgOk.value = false
  } finally {
    submitting.value = false
  }
}

async function onCanary() {
  if (submitting.value) return
  submitting.value = true
  canaryMsg.value = ''
  const agentIDs = canaryForm.value.agentIDsRaw.split(/[\s,]+/).filter(Boolean)
  try {
    const r = await store.canary({
      agentIDs,
      key: canaryForm.value.key,
      value: canaryForm.value.value,
      path: canaryForm.value.path,
      format: canaryForm.value.format,
      strategy: canaryForm.value.strategy,
      percentage: canaryForm.value.percentage
    })
    canaryMsg.value = t('configHotpush.canary_success', { id: r.j?.canaryID || '—' })
    canaryMsgOk.value = true
  } catch (e) {
    canaryMsg.value = t('configHotpush.canary_failed') + (e.j?.error || e.message || '')
    canaryMsgOk.value = false
  } finally {
    submitting.value = false
  }
}

async function searchVersions() {
  const params = {}
  if (filter.value.key) params.key = filter.value.key
  if (filter.value.agentID) params.agentID = filter.value.agentID
  params.limit = filter.value.limit || 50
  try { await store.fetchVersions(params) }
  catch (e) {
    errorConfirm.message = e.j?.error || t('error.configVersionsFailed')
    errorConfirm.show = true
  }
}

function resetFilter() {
  filter.value = { key: '', agentID: '', limit: 50 }
  store.fetchVersions({ limit: 50 })
}

onMounted(() => {})
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

.filter-bar { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; margin: 10px 0; }
.filter-input { padding: 6px 10px; min-width: 160px; }
.filter-input.narrow { min-width: 90px; max-width: 110px; }

.kv-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 10px; }
.kv { display: flex; flex-direction: column; gap: 4px; padding: 8px 10px; background: var(--surface-2); border-radius: var(--radius-sm); }
.kv .k { font-size: 11.5px; color: var(--text-3); }
.kv .v { font-size: 13px; color: var(--text); word-break: break-all; }

.value-cell { display: inline-block; max-width: 240px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.poll-err {
  padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}

@media (max-width: 768px) {
  .row-2 { grid-template-columns: 1fr; }
}
</style>