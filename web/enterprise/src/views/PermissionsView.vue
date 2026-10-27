<template>
  <PageShell :title="$t('permissions.title')" :desc="$t('permissions.subtitle')" testid="permissions" :error="loadErr">
    <div class="btnbar">
      <button class="outline" @click="fetchPermissions">
        <Icon name="refresh" :size="14" />
        {{ $t('common.refresh') }}
      </button>
    </div>

    <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
    <div v-else-if="!groups.length" class="muted">{{ $t('permissions.empty') }}</div>
    <div v-else>
      <div v-for="g in groups" :key="g.group" class="perm-section card">
        <h3>
          <Icon name="permissions" :size="16" />
          {{ g.group || '—' }}
          <span class="count">({{ g.items.length }})</span>
        </h3>
        <DataTable :columns="columns" :rows="g.items" row-key="id" :empty-text="$t('permissions.empty')">
          <template #cell-name="{ value }"><code>{{ value }}</code></template>
        </DataTable>
      </div>
    </div>
  </PageShell>
</template>

<script setup>
// 权限管理页 — 按功能组分类展示所有权限项
import PageShell from '@/components/PageShell.vue'
import { ref, computed, onMounted } from 'vue'
import { authApi } from '@/api/auth'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import Icon from '@/components/Icon.vue'

const permissions = ref([])
const loading = ref(false)
const loadErr = ref('')

const columns = [
  { key: 'name', title: t('permissions.name'), slot: 'cell-name' },
  { key: 'description', title: t('permissions.description') }
]

// 按 group 分组
const groups = computed(() => {
  const map = {}
  for (const p of permissions.value) {
    const g = p.group || '—'
    if (!map[g]) map[g] = []
    map[g].push(p)
  }
  return Object.entries(map).map(([group, items]) => ({ group, items }))
})

async function fetchPermissions() {
  loading.value = true
  loadErr.value = ''
  try {
    const r = await authApi.listPermissions()
    permissions.value = (r && r.permissions) || r || []
  } catch (e) {
    // 此前静默吞错（只见空态）——统一骨架批次补上可见的加载错误
    permissions.value = []
    loadErr.value = e.j?.error || t('common.load_failed')
  } finally {
    loading.value = false
  }
}

onMounted(() => fetchPermissions())
</script>

<style scoped>
.perm-section h3 { display: flex; align-items: center; gap: 8px; margin-top: 0; }
.perm-section .count { color: var(--text-3); font-weight: 400; font-size: 13px; }
</style>