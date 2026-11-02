<template>
  <PageShell
    :title="$t('cloudResources.title')"
    :desc="$t('cloudResources.desc')"
    testid="cloud-resources"
    :error="store.error"
  >
    <!-- 筛选栏：用全站 canonical 的 .flowbar（.filter-bar 是历史分歧名、pages.css 未收） -->
    <div class="flowbar">
      <div class="field field--fixed">
        <label>{{ $t('cloudResources.field_account') }}</label>
        <select v-model="accountFilter" data-testid="cloud-resources-account-filter" @change="applyFilters">
          <option value="">{{ $t('cloudResources.allAccounts') }}</option>
          <option v-for="a in accounts" :key="a.id" :value="a.id">{{ a.name }}</option>
        </select>
      </div>
      <div class="field field--fixed">
        <label>{{ $t('cloudResources.field_type') }}</label>
        <select v-model="typeFilter" data-testid="cloud-resources-type-filter" @change="applyFilters">
          <option value="">{{ $t('cloudResources.allTypes') }}</option>
          <option v-for="tp in resourceTypes" :key="tp" :value="tp">{{ tp }}</option>
        </select>
      </div>
      <Button variant="ghost" size="sm" @click="reload"><Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}</Button>
    </div>

    <div v-if="store.loading && !store.resources.length" class="muted">{{ $t('common.loading') }}</div>
    <DataTable
      v-else
      :columns="columns"
      :rows="store.resources"
      row-key="id"
      :loading="store.loading"
      :empty-text="$t('cloudResources.empty')"
    >
      <template #cell-cloudAccountId="{ row }">
        {{ accountName(row.cloudAccountId) }}
      </template>
      <template #cell-status="{ value }">
        <span class="status-pill" :class="statusClass(value)">{{ value || '-' }}</span>
      </template>
      <template #cell-resourceId="{ value }"><code>{{ value || '-' }}</code></template>
    </DataTable>
  </PageShell>
</template>

<script setup>
// 云资源列表（2026-11-02）。
// 背景：后端 GET /api/v1/cloud-resources（internal/controlplane/server_lifecycle.go:431）
// 支持 accountID / resourceType 过滤，但此前后端有能力、前端无界面（死 API 面清查中
// listCloudResources 零消费方）。POST/DELETE/PUT 三个后端写端点目前 api 层无 wrapper，
// 故本页只做只读列表与筛选；写入口见 api-usage 守卫 ALLOW 记账，属后续缺口。
import { ref, computed, onMounted } from 'vue'
import { t } from '@/i18n'
import PageShell from '@/components/PageShell.vue'
import DataTable from '@/components/DataTable.vue'
import Button from '@/components/ui/Button.vue'
import Icon from '@/components/Icon.vue'
import { useCloudResourcesStore } from '@/stores/cloud-resources'
import { listCloudAccounts } from '@/api/cloud-account'

const store = useCloudResourcesStore()

const accountFilter = ref('')
const typeFilter = ref('')
const accounts = ref([])
const resourceTypes = ref([])

// 字段名按 store.CloudResource 的 json tag 逐字对齐（internal/store/model/model.go:816-831）：
// 没有 name/accountName 字段——资源名是 resourceName，账号名需本地用 cloudAccountId 映射。
const columns = computed(() => [
  { key: 'resourceName', label: t('cloudResources.col_name') },
  { key: 'resourceType', label: t('cloudResources.col_type') },
  { key: 'provider', label: t('cloudResources.col_provider') },
  { key: 'region', label: t('cloudResources.col_region') },
  { key: 'cloudAccountId', label: t('cloudResources.col_account') },
  { key: 'status', label: t('cloudResources.col_status') },
  { key: 'resourceId', label: t('cloudResources.col_resourceId') },
  { key: 'createdAt', label: t('cloudResources.col_created') }
])

function statusClass(v) {
  if (v === 'running' || v === 'active') return 'ok'
  if (v === 'stopped' || v === 'failed') return 'off'
  return ''
}

// 后端只给 cloudAccountId，账号名靠本页拉的账号列表映射；未拉到则退回 ID 本身。
function accountName(id) {
  if (!id) return '-'
  const hit = accounts.value.find((a) => a.id === id)
  return hit ? hit.name : id
}

function applyFilters() {
  store.setFilter('accountID', accountFilter.value)
  store.setFilter('resourceType', typeFilter.value)
  reload()
}

function reload() { store.fetchResources() }

onMounted(async () => {
  reload()
  try {
    const resp = await listCloudAccounts({ limit: 200 })
    accounts.value = resp.accounts || []
    const types = new Set()
    for (const r of store.resources) if (r.resourceType) types.add(r.resourceType)
    resourceTypes.value = [...types].sort()
  } catch { /* 账号列表拉取失败不阻塞资源列表展示 */ }
})
</script>

<style scoped>
/* 共享原语（.filter-bar/.field/.status-pill/.muted/.btnbar）全部在 styles/pages.css；
   本页无页面专属样式 */
</style>
