<template>
  <div>
    <h2 data-testid="helm-releases-title">{{ $t('helm.releases_title') }}</h2>
    <p class="muted">{{ $t('helm.releases_subtitle') }}</p>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <div class="btnbar">
      <button class="primary" @click="openInstall" data-testid="helm-release-install-btn">
        <Icon name="add" :size="14" /> {{ $t('helm.install_release') }}
      </button>
      <button class="outline" @click="store.fetchReleases()"><Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}</button>
    </div>

    <div v-if="store.loading && !store.releases.length" class="muted">{{ $t('common.loading') }}</div>
    <DataTable
      v-else
      :columns="columns"
      :rows="store.releases"
      row-key="name"
      :loading="store.loading"
      :empty-text="$t('helm.empty_releases')"
    >
      <template #cell-name="{ value }"><code>{{ value }}</code></template>
      <template #cell-namespace="{ value }"><span class="tag">{{ value }}</span></template>
      <template #cell-status="{ value }">
        <span class="tag" :class="'st-' + value">{{ $t('helm.status_' + value) }}</span>
      </template>
      <template #cell-actions="{ row }">
        <div class="row-actions">
          <button class="xs outline" :title="$t('helm.upgrade')" @click="openUpgrade(row)" data-testid="helm-release-upgrade-btn"><Icon name="edit" :size="13" /></button>
          <button class="xs outline" :title="$t('helm.history')" @click="openHistory(row)" data-testid="helm-release-history-btn"><Icon name="logs" :size="13" /></button>
          <button class="xs outline" :title="$t('helm.rollback')" @click="openRollback(row)" data-testid="helm-release-rollback-btn"><Icon name="success" :size="13" /></button>
          <button class="xs danger" :title="$t('helm.uninstall')" @click="onUninstall(row)" data-testid="helm-release-uninstall-btn"><Icon name="delete" :size="13" /></button>
        </div>
      </template>
    </DataTable>

    <!-- 安装/升级抽屉 -->
    <DetailDrawer :open="!!form" :title="form && form.mode === 'upgrade' ? $t('helm.upgrade_title', { name: form.name }) : $t('helm.install_release')" @close="form = null">
      <form v-if="form" class="entity-form" @submit.prevent="onSave">
        <div class="field">
          <label>{{ $t('helm.field_release_name') }}</label>
          <input v-model.trim="form.name" type="text" :disabled="form.mode === 'upgrade'" required />
        </div>
        <div class="field">
          <label>{{ $t('helm.field_release_namespace') }}</label>
          <input v-model.trim="form.namespace" type="text" placeholder="default" required />
        </div>
        <div class="field">
          <label>{{ $t('helm.field_release_chart') }}</label>
          <input v-model.trim="form.chart" type="text" placeholder="nginx-ingress" required />
        </div>
        <div class="field">
          <label>{{ $t('helm.field_release_repo') }}</label>
          <input v-model.trim="form.repo" type="text" placeholder="stable" />
        </div>
        <div class="field">
          <label>{{ $t('helm.field_release_values') }}</label>
          <textarea v-model="form.valuesRaw" rows="6" :placeholder="$t('helm.values_placeholder')"></textarea>
          <small class="hint">{{ $t('helm.values_hint') }}</small>
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('common.save') }}</button>
          <button type="button" class="outline" @click="form = null">{{ $t('common.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 历史抽屉 -->
    <DetailDrawer :open="!!historyRelease" :title="$t('helm.history_title', { name: historyRelease })" @close="closeHistory">
      <div v-if="store.loading && !store.history.length" class="muted">{{ $t('common.loading') }}</div>
      <DataTable
        v-else
        :columns="historyColumns"
        :rows="store.history"
        row-key="revision"
        :loading="store.loading"
        :empty-text="$t('helm.empty_history')"
      >
        <template #cell-status="{ value }">
          <span class="tag" :class="'st-' + value">{{ $t('helm.status_' + value) }}</span>
        </template>
      </DataTable>
    </DetailDrawer>

    <!-- 回滚确认（带 revision 输入） -->
    <DetailDrawer :open="!!rollbackForm" :title="$t('helm.rollback_title', { name: rollbackForm && rollbackForm.name })" @close="rollbackForm = null">
      <form v-if="rollbackForm" class="entity-form" @submit.prevent="onRollbackConfirm">
        <div class="field">
          <label>{{ $t('helm.field_rollback_revision') }}</label>
          <input v-model.number="rollbackForm.revision" type="number" min="1" required />
          <small class="hint">{{ $t('helm.rollback_hint') }}</small>
        </div>
        <div v-if="rollbackError" class="msg err">{{ rollbackError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('helm.rollback') }}</button>
          <button type="button" class="outline" @click="rollbackForm = null">{{ $t('common.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 卸载确认 -->
    <ConfirmModal
      v-model="uninstallConfirm.show"
      data-testid="helm-release-uninstall-confirm-modal"
      :title="$t('helm.uninstall')"
      :message="$t('helm.confirm_uninstall')"
      @confirm="onUninstallConfirm"
    />
    <!-- 错误提示 -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="helm-release-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// Helm Release 管理页 — 列表 + 安装 + 升级 + 卸载 + 回滚 + 历史
import { ref, computed, reactive, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useHelmStore } from '@/stores/helm'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'

const store = useHelmStore()
const route = useRoute()
const formError = ref('')
const rollbackError = ref('')
const uninstallConfirm = reactive({ show: false, name: null })
const errorConfirm = reactive({ show: false, message: '' })
const historyRelease = ref('')

const columns = computed(() => [
  { key: 'name', title: t('helm.field_release_name'), slot: 'cell-name' },
  { key: 'namespace', title: t('helm.field_release_namespace'), slot: 'cell-namespace', width: '110px' },
  { key: 'chart', title: t('helm.field_release_chart') },
  { key: 'chartVersion', title: t('helm.field_chart_version'), width: '100px' },
  { key: 'status', title: t('helm.field_release_status'), slot: 'cell-status', width: '90px' },
  { key: 'revision', title: t('helm.field_release_revision'), width: '70px' },
  { key: 'updatedAt', title: t('helm.field_updatedAt') },
  { key: 'actions', title: t('helm.col_actions'), slot: 'cell-actions', width: '140px' }
])

const historyColumns = computed(() => [
  { key: 'revision', title: t('helm.field_release_revision'), width: '70px' },
  { key: 'chart', title: t('helm.field_release_chart') },
  { key: 'chartVersion', title: t('helm.field_chart_version'), width: '100px' },
  { key: 'status', title: t('helm.field_release_status'), slot: 'cell-status', width: '90px' },
  { key: 'updatedAt', title: t('helm.field_updatedAt') }
])

const form = ref(null)
const rollbackForm = ref(null)

function openInstall() {
  formError.value = ''
  // 从 catalog 跳转携带预填参数
  const q = route.query
  form.value = {
    mode: 'install',
    name: '',
    namespace: 'default',
    chart: q.chart || '',
    repo: q.repo || '',
    valuesRaw: ''
  }
}

function openUpgrade(row) {
  formError.value = ''
  form.value = {
    mode: 'upgrade',
    name: row.name,
    namespace: row.namespace || 'default',
    chart: row.chart || '',
    repo: '',
    valuesRaw: ''
  }
}

function parseValues(raw) {
  if (!raw || !raw.trim()) return {}
  try {
    return JSON.parse(raw)
  } catch {
    return null
  }
}

async function onSave() {
  formError.value = ''
  if (!form.value.name || !form.value.namespace || !form.value.chart) {
    formError.value = t('helm.release_form_required')
    return
  }
  const values = parseValues(form.value.valuesRaw)
  if (values === null) {
    formError.value = t('helm.values_invalid')
    return
  }
  try {
    const body = {
      name: form.value.name,
      chart: form.value.chart,
      namespace: form.value.namespace,
      values,
      repo: form.value.repo
    }
    if (form.value.mode === 'upgrade') {
      await store.upgradeRelease(form.value.name, body)
      toast.success(t('helm.release_upgraded'))
    } else {
      await store.createRelease(body)
      toast.success(t('helm.release_installed'))
    }
    form.value = null
  } catch (e) {
    formError.value = e.j?.error || (form.value.mode === 'upgrade' ? t('helm.release_upgrade_failed') : t('helm.release_install_failed'))
  }
}

function openHistory(row) {
  historyRelease.value = row.name
  store.fetchHistory(row.name).catch((e) => {
    errorConfirm.message = e.j?.error || t('error.helmHistoryFailed')
    errorConfirm.show = true
  })
}

function closeHistory() {
  historyRelease.value = ''
  store.history = []
}

function openRollback(row) {
  rollbackError.value = ''
  rollbackForm.value = { name: row.name, revision: row.revision ? row.revision - 1 : 1 }
}

async function onRollbackConfirm() {
  rollbackError.value = ''
  const { name, revision } = rollbackForm.value
  if (!name || !revision || revision < 1) {
    rollbackError.value = t('helm.rollback_invalid')
    return
  }
  try {
    await store.rollbackRelease(name, { revision })
    toast.success(t('helm.release_rolled_back'))
    rollbackForm.value = null
  } catch (e) {
    rollbackError.value = e.j?.error || t('error.helmRollbackFailed')
  }
}

function onUninstall(row) {
  uninstallConfirm.name = row.name
  uninstallConfirm.show = true
}

async function onUninstallConfirm() {
  const { name } = uninstallConfirm
  if (!name) return
  try {
    await store.removeRelease(name)
    toast.success(t('helm.release_uninstalled'))
  } catch (e) {
    errorConfirm.message = e.j?.error || t('error.helmUninstallFailed')
    errorConfirm.show = true
  }
}

onMounted(() => {
  store.fetchReleases().catch((e) => {
    errorConfirm.message = e.j?.error || t('error.helmReleasesFailed')
    errorConfirm.show = true
  })
  // 从 catalog 跳转携带预填参数时自动打开安装抽屉
  if (route.query.chart) openInstall()
})
</script>

<style scoped>
.btnbar { display: flex; gap: 8px; margin-bottom: 12px; }
.row-actions { display: flex; gap: 4px; }

.poll-err {
  padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}

.entity-form { display: flex; flex-direction: column; gap: 12px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field > label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.field .hint { font-size: 11.5px; color: var(--text-3); }
.btnbar { margin-top: 8px; }

.tag {
  display: inline-block; padding: 2px 8px; border-radius: 999px;
  font-size: 12px; background: var(--accent-soft); color: var(--accent);
}
.tag.st-deployed { background: var(--ok-bg); color: var(--ok); }
.tag.st-failed { background: var(--fail-bg); color: var(--fail); }
.tag.st-pending { background: var(--warn-bg); color: var(--warn); }
</style>