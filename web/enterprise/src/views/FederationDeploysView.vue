<template>
  <div>
    <h2 data-testid="federation-deploys-title">{{ $t('federationDeploys.title') }}</h2>
    <p class="muted">{{ $t('federationDeploys.subtitle') }}</p>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <div class="btnbar">
      <button class="primary" @click="openCreate" data-testid="federation-deploys-create-btn">
        <Icon name="add" :size="14" /> {{ $t('federationDeploys.add') }}
      </button>
      <button class="outline" @click="store.fetchDeploys()"><Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}</button>
    </div>

    <!-- 联邦部署列表 -->
    <div v-if="store.loading && !store.deploys.length" class="muted">{{ $t('common.loading') }}</div>
    <DataTable
      v-else
      :columns="columns"
      :rows="store.deploys"
      row-key="id"
      :loading="store.loading"
      :empty-text="$t('federationDeploys.empty')"
    >
      <template #cell-id="{ value }"><code>{{ value }}</code></template>
      <template #cell-status="{ value }">
        <span class="status-pill" :class="statusClass(value)">{{ value || '—' }}</span>
      </template>
      <template #cell-clusters="{ value }">
        <span class="cluster-chips">
          <span v-for="c in (value || [])" :key="c.clusterID" class="cluster-chip" :class="statusClass(c.status)">
            {{ c.clusterID }}:{{ c.status || '?' }}
          </span>
        </span>
      </template>
      <template #cell-actions="{ row }">
        <div class="row-actions">
          <button class="xs outline" @click="viewDetail(row)" data-testid="federation-deploys-detail-btn">{{ $t('federationDeploys.detail') }}</button>
        </div>
      </template>
    </DataTable>

    <!-- 创建联邦部署抽屉 -->
    <DetailDrawer :open="!!form" :title="$t('federationDeploys.add')" @close="form = null">
      <form v-if="form" class="entity-form" @submit.prevent="onCreate">
        <div class="field">
          <label>{{ $t('federationDeploys.field_name') }}</label>
          <input v-model.trim="form.name" type="text" required />
        </div>
        <div class="field">
          <label>{{ $t('federationDeploys.field_clusters') }}</label>
          <textarea v-model.trim="form.clustersRaw" rows="3" :placeholder="$t('federationDeploys.clusters_placeholder')" required></textarea>
        </div>
        <div class="field">
          <label>{{ $t('federationDeploys.field_template') }}</label>
          <input v-model.trim="form.template" type="text" :placeholder="$t('federationDeploys.template_placeholder')" />
        </div>
        <div class="field">
          <label>{{ $t('federationDeploys.field_params') }}</label>
          <textarea v-model="form.paramsRaw" rows="3" :placeholder="$t('federationDeploys.params_placeholder')"></textarea>
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('common.save') }}</button>
          <button type="button" class="outline" @click="form = null">{{ $t('common.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 详情抽屉 -->
    <DetailDrawer :open="!!store.current" :title="$t('federationDeploys.detail_title', { id: store.current?.id || '—' })" @close="store.clearCurrent()">
      <div v-if="store.current" class="detail-body">
        <div class="kv"><span class="k">{{ $t('federationDeploys.field_name') }}</span><span class="v">{{ store.current.name || '—' }}</span></div>
        <div class="kv"><span class="k">{{ $t('federationDeploys.field_status') }}</span><span class="v"><span class="status-pill" :class="statusClass(store.current.status)">{{ store.current.status || '—' }}</span></span></div>
        <div class="kv"><span class="k">{{ $t('federationDeploys.field_template') }}</span><span class="v">{{ store.current.template || '—' }}</span></div>
        <div class="kv"><span class="k">{{ $t('federationDeploys.field_createdAt') }}</span><span class="v">{{ store.current.createdAt || '—' }}</span></div>
        <div class="kv"><span class="k">{{ $t('federationDeploys.field_createdBy') }}</span><span class="v">{{ store.current.createdBy || '—' }}</span></div>
        <h4>{{ $t('federationDeploys.clusters_title') }}</h4>
        <div class="cluster-list">
          <div v-for="c in (store.current.clusters || [])" :key="c.clusterID" class="cluster-item">
            <code>{{ c.clusterID }}</code>
            <span class="status-pill" :class="statusClass(c.status)">{{ c.status || '—' }}</span>
            <small v-if="c.error" class="err-text">{{ c.error }}</small>
          </div>
          <div v-if="!(store.current.clusters || []).length" class="muted">{{ $t('common.no_data') }}</div>
        </div>
        <h4 v-if="store.current.params">{{ $t('federationDeploys.field_params') }}</h4>
        <pre v-if="store.current.params" class="json-pre">{{ formatJson(store.current.params) }}</pre>
      </div>
    </DetailDrawer>

    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="federation-deploys-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// 多集群联邦部署页 — 列表 + 创建 + 详情（各集群状态）
import { ref, reactive, computed, onMounted } from 'vue'
import { useFederationDeploysStore } from '@/stores/deploys-federation'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'

const store = useFederationDeploysStore()
const formError = ref('')
const errorConfirm = reactive({ show: false, message: '' })

const columns = computed(() => [
  { key: 'id', title: t('federationDeploys.col_id'), slot: 'cell-id' },
  { key: 'name', title: t('federationDeploys.field_name') },
  { key: 'status', title: t('federationDeploys.field_status'), slot: 'cell-status' },
  { key: 'clusters', title: t('federationDeploys.col_clusters'), slot: 'cell-clusters' },
  { key: 'createdAt', title: t('federationDeploys.field_createdAt') },
  { key: 'createdBy', title: t('federationDeploys.field_createdBy') },
  { key: 'actions', title: t('federationDeploys.col_actions'), slot: 'cell-actions', width: '90px' }
])

function statusClass(status) {
  if (status === 'success' || status === 'completed' || status === 'synced') return 'ok'
  if (status === 'pending' || status === 'running') return 'warn'
  return 'off'
}

function formatJson(obj) {
  try { return JSON.stringify(obj, null, 2) } catch { return String(obj) }
}

// ---------- 创建 ----------
const form = ref(null)

function openCreate() {
  formError.value = ''
  form.value = { name: '', clustersRaw: '', template: '', paramsRaw: '' }
}

async function onCreate() {
  formError.value = ''
  const clusters = form.value.clustersRaw.split(/[\s,]+/).filter(Boolean)
  let params
  try {
    params = form.value.paramsRaw ? JSON.parse(form.value.paramsRaw) : {}
  } catch {
    params = {}
  }
  try {
    await store.create({
      name: form.value.name,
      clusters,
      template: form.value.template,
      params
    })
    form.value = null
    toast.success(t('federationDeploys.created'))
  } catch (e) {
    formError.value = e.j?.error || t('federationDeploys.create_failed')
  }
}

async function viewDetail(row) {
  try { await store.fetchDetail(row.id) }
  catch (e) {
    errorConfirm.message = e.j?.error || t('error.federationDeployDetailFailed')
    errorConfirm.show = true
  }
}

onMounted(() => {
  store.fetchDeploys().catch((e) => {
    errorConfirm.message = e.j?.error || t('error.federationDeploysListFailed')
    errorConfirm.show = true
  })
})
</script>

<style scoped>
.btnbar { display: flex; gap: 8px; margin-top: 8px; }
.field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 10px; min-width: 220px; }
.field label { margin: 0; font-size: 12.5px; color: var(--text-2); }

.cluster-chips { display: inline-flex; flex-wrap: wrap; gap: 4px; }
.cluster-chip {
  display: inline-block; padding: 1px 6px; border-radius: var(--radius-sm);
  font-size: 11px; border: 1px solid var(--border); background: var(--surface-2);
}
.cluster-chip.ok { border-color: var(--teal); }
.cluster-chip.warn { border-color: var(--amber); }
.cluster-chip.off { border-color: var(--rose); }

.detail-body .kv { display: flex; flex-direction: column; gap: 4px; margin-bottom: 8px; }
.detail-body .k { font-size: 11.5px; color: var(--text-3); }
.detail-body .v { font-size: 13px; color: var(--text); word-break: break-all; }
.detail-body h4 { margin: 14px 0 6px; font-size: 13px; }
.cluster-list { display: flex; flex-direction: column; gap: 6px; }
.cluster-item { display: flex; align-items: center; gap: 8px; font-size: 12.5px; }
.cluster-item .err-text { color: var(--fail); margin-left: auto; }
.json-pre {
  background: var(--surface-2); border: 1px solid var(--border);
  border-radius: var(--radius-sm); padding: 8px; font-size: 12px;
  white-space: pre-wrap; word-break: break-all; max-height: 240px; overflow: auto;
}

.poll-err {
  padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}
.status-pill.warn { background: var(--warn-soft, #fef3c7); color: var(--warn, #b45309); }
</style>