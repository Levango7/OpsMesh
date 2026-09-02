<template>
  <div>
    <h2 data-testid="helm-repos-title">{{ $t('helm.repos_title') }}</h2>
    <p class="muted">{{ $t('helm.repos_subtitle') }}</p>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <div class="btnbar">
      <button class="primary" @click="openCreate" data-testid="helm-repo-create-btn">
        <Icon name="add" :size="14" /> {{ $t('helm.add_repo') }}
      </button>
      <button class="outline" @click="store.fetchRepos()"><Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}</button>
    </div>

    <div v-if="store.loading && !store.repos.length" class="muted">{{ $t('common.loading') }}</div>
    <DataTable
      v-else
      :columns="columns"
      :rows="store.repos"
      row-key="name"
      :loading="store.loading"
      :empty-text="$t('helm.empty_repos')"
    >
      <template #cell-name="{ value }"><code>{{ value }}</code></template>
      <template #cell-url="{ value }"><code class="url">{{ value }}</code></template>
      <template #cell-actions="{ row }">
        <div class="row-actions">
          <button class="xs outline" :title="$t('helm.view_charts')" @click="openCharts(row)" data-testid="helm-repo-charts-btn"><Icon name="search" :size="13" /></button>
          <button class="xs danger" @click="onDelete(row)" data-testid="helm-repo-delete-btn"><Icon name="delete" :size="13" /></button>
        </div>
      </template>
    </DataTable>

    <!-- 添加仓库抽屉 -->
    <DetailDrawer :open="!!form" :title="$t('helm.add_repo')" @close="form = null">
      <form v-if="form" class="entity-form" @submit.prevent="onSave">
        <div class="field">
          <label>{{ $t('helm.field_repo_name') }}</label>
          <input v-model.trim="form.name" type="text" required />
        </div>
        <div class="field">
          <label>{{ $t('helm.field_repo_url') }}</label>
          <input v-model.trim="form.url" type="text" placeholder="https://charts.helm.sh/stable" required />
        </div>
        <div class="field">
          <label>{{ $t('helm.field_repo_username') }}</label>
          <input v-model.trim="form.username" type="text" />
        </div>
        <div class="field">
          <label>{{ $t('helm.field_repo_password') }}</label>
          <input v-model.trim="form.password" type="password" />
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('common.save') }}</button>
          <button type="button" class="outline" @click="form = null">{{ $t('common.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 仓库 Chart 列表抽屉 -->
    <DetailDrawer :open="!!chartsRepo" :title="$t('helm.repo_charts_title', { name: chartsRepo })" @close="closeCharts">
      <div v-if="store.loading && !store.charts.length" class="muted">{{ $t('common.loading') }}</div>
      <DataTable
        v-else
        :columns="chartColumns"
        :rows="store.charts"
        row-key="name"
        :loading="store.loading"
        :empty-text="$t('helm.empty_charts')"
      >
        <template #cell-name="{ value }"><code>{{ value }}</code></template>
        <template #cell-version="{ value }"><span class="tag">{{ value }}</span></template>
        <template #cell-appVersion="{ value }"><span class="tag">{{ value }}</span></template>
      </DataTable>
    </DetailDrawer>

    <!-- 删除确认 -->
    <ConfirmModal
      v-model="deleteConfirm.show"
      data-testid="helm-repo-delete-confirm-modal"
      :title="$t('common.delete')"
      :message="$t('helm.confirm_delete_repo')"
      @confirm="onDeleteConfirm"
    />
    <!-- 错误提示 -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="helm-repo-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// Helm 仓库管理页 — 仓库列表 + 添加 + 删除 + 查看仓库 Chart
import { ref, computed, reactive, onMounted } from 'vue'
import { useHelmStore } from '@/stores/helm'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'

const store = useHelmStore()
const formError = ref('')
const deleteConfirm = reactive({ show: false, name: null })
const errorConfirm = reactive({ show: false, message: '' })
const chartsRepo = ref('')

const columns = computed(() => [
  { key: 'name', title: t('helm.field_repo_name'), slot: 'cell-name' },
  { key: 'url', title: t('helm.field_repo_url'), slot: 'cell-url' },
  { key: 'username', title: t('helm.field_repo_username') },
  { key: 'addedAt', title: t('helm.field_addedAt') },
  { key: 'actions', title: t('helm.col_actions'), slot: 'cell-actions', width: '90px' }
])

const chartColumns = computed(() => [
  { key: 'name', title: t('helm.field_chart_name'), slot: 'cell-name' },
  { key: 'version', title: t('helm.field_chart_version'), slot: 'cell-version', width: '100px' },
  { key: 'appVersion', title: t('helm.field_app_version'), slot: 'cell-appVersion', width: '110px' },
  { key: 'description', title: t('helm.field_chart_desc') }
])

const form = ref(null)

function openCreate() {
  formError.value = ''
  form.value = { name: '', url: '', username: '', password: '' }
}

async function onSave() {
  formError.value = ''
  if (!form.value.name || !form.value.url) {
    formError.value = t('helm.repo_form_required')
    return
  }
  try {
    const body = {
      name: form.value.name,
      url: form.value.url,
      username: form.value.username,
      password: form.value.password
    }
    await store.createRepo(body)
    form.value = null
    toast.success(t('helm.repo_saved'))
  } catch (e) {
    formError.value = e.j?.error || t('helm.repo_save_failed')
  }
}

function onDelete(row) {
  deleteConfirm.name = row.name
  deleteConfirm.show = true
}

async function onDeleteConfirm() {
  const { name } = deleteConfirm
  if (!name) return
  try {
    await store.removeRepo(name)
    toast.success(t('helm.repo_deleted'))
  } catch (e) {
    errorConfirm.message = e.j?.error || t('helm.repo_delete_failed')
    errorConfirm.show = true
  }
}

async function openCharts(row) {
  chartsRepo.value = row.name
  await store.fetchRepoCharts(row.name)
}

function closeCharts() {
  chartsRepo.value = ''
  store.charts = []
}

onMounted(() => {
  store.fetchRepos().catch((e) => {
    errorConfirm.message = e.j?.error || t('error.helmReposFailed')
    errorConfirm.show = true
  })
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
.btnbar { margin-top: 8px; }

.tag {
  display: inline-block; padding: 2px 8px; border-radius: 999px;
  font-size: 12px; background: var(--accent-soft); color: var(--accent);
}
.url { font-size: 12px; }
</style>