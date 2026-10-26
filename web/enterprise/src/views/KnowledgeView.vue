<template>
  <div class="knowledge-page">
    <div class="page-head">
      <h2 class="page-title">{{ $t('knowledge.title') }}</h2>
      <p class="page-desc">{{ $t('knowledge.desc') }}</p>
    </div>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <!-- 统计 -->
    <div class="metrics-row" v-if="store.stats">
      <MetricsCard :title="$t('knowledge.total_docs')" icon="cmdb">
        <div class="metric-value">{{ store.stats.totalDocs || 0 }}</div>
      </MetricsCard>
      <MetricsCard :title="$t('knowledge.sources')" icon="info">
        <div class="metric-value">{{ store.syncConfigs.length || 0 }}</div>
      </MetricsCard>
    </div>

    <!-- 搜索 -->
    <div class="card">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('knowledge.search_title') }}</h3>
      </div>
      <div class="search-bar">
        <input v-model="searchQuery" :placeholder="$t('knowledge.search_placeholder')" class="search-input" @keyup.enter="doSearch" />
        <Button variant="primary" size="sm" @click="doSearch" :disabled="store.searchLoading">{{ $t('common.search') }}</Button>
        <label class="chk"><input type="checkbox" v-model="rerank" /> {{ $t('knowledge.rerank') }}</label>
      </div>
      <div v-if="store.searchLoading" class="muted">{{ $t('common.loading') }}</div>
      <div v-else-if="store.searchResults.length">
        <div v-for="doc in store.searchResults" :key="doc.id" class="search-result">
          <div class="result-title">{{ doc.title }}</div>
          <div class="result-content">{{ doc.content }}</div>
          <div class="result-meta">
            <span class="result-source">{{ doc.source }}</span>
            <span class="result-score">{{ $t('knowledge.relevance') }}: {{ (doc.score || 0).toFixed(3) }}</span>
          </div>
          <div v-if="doc.citation" class="result-citation">{{ doc.citation.excerpt }}</div>
        </div>
      </div>
      <div v-else-if="store.searchQuery" class="muted">{{ $t('knowledge.noResults') }}</div>
    </div>

    <!-- 同步配置 -->
    <div class="card">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('knowledge.sync_title') }}</h3>
        <Button variant="primary" size="sm" @click="syncAll" :disabled="store.loading">{{ $t('knowledge.sync_all') }}</Button>
      </div>
      <DataTable
        v-if="store.syncConfigs.length"
        :columns="syncColumns"
        :rows="store.syncConfigs"
        row-key="source"
      >
        <template #cell-enabled="{ value }">
          <span :class="value ? 'status-on' : 'status-off'">{{ value ? $t('common.yes') : $t('common.no') }}</span>
        </template>
        <template #cell-actions="{ row }">
          <div class="row-actions" @click.stop>
            <Button variant="ghost" size="sm" @click="syncSource(row.source)">{{ $t('knowledge.sync_one') }}</Button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- 文档列表 -->
    <div class="card">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('knowledge.docs_title') }}</h3>
        <select v-model="sourceFilter" @change="fetchDocs" class="filter-select">
          <option value="">{{ $t('common.all') }}</option>
          <option v-for="s in sources" :key="s" :value="s">{{ s }}</option>
        </select>
        <Button variant="ghost" size="sm" @click="fetchDocs">{{ $t('common.refresh') }}</Button>
      </div>
      <DataTable
        v-if="store.documents.length"
        :columns="docColumns"
        :rows="store.documents"
        row-key="id"
      >
        <template #cell-source="{ value }"><span class="source-tag">{{ value }}</span></template>
        <template #cell-actions="{ row }">
          <div class="row-actions" @click.stop>
            <Button variant="danger" size="sm" @click="deleteDoc(row.id)">{{ $t('common.delete') }}</Button>
          </div>
        </template>
      </DataTable>
      <div v-else class="muted">{{ $t('knowledge.noDocs') }}</div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useKnowledgeStore } from '@/stores/knowledge'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import MetricsCard from '@/components/MetricsCard.vue'
import Icon from '@/components/Icon.vue'
import { Button } from '@/components/ui'

const store = useKnowledgeStore()

const searchQuery = ref('')
const rerank = ref(false)
const sourceFilter = ref('')
const sources = ['cmdb', 'alert', 'runbook', 'git', 'confluence', 'manual']

const syncColumns = [
  { key: 'source', title: t('knowledge.source') },
  { key: 'enabled', title: t('knowledge.enabled'), slot: 'cell-enabled' },
  { key: 'interval', title: t('knowledge.interval') },
  { key: 'lastSync', title: t('knowledge.last_sync') },
  { key: 'actions', title: t('knowledge.actions'), slot: 'cell-actions', width: '100px' }
]

const docColumns = [
  { key: 'id', title: 'ID' },
  { key: 'title', title: t('knowledge.title_col') },
  { key: 'source', title: t('knowledge.source'), slot: 'cell-source' },
  { key: 'createdAt', title: t('knowledge.created_at') },
  { key: 'actions', title: t('knowledge.actions'), slot: 'cell-actions', width: '100px' }
]

async function doSearch() {
  if (!searchQuery.value.trim()) return
  try { await store.search(searchQuery.value, rerank.value) } catch { /* ignore */ }
}

async function syncAll() {
  try { await store.sync('') } catch { /* ignore */ }
}

async function syncSource(source) {
  try { await store.sync(source) } catch { /* ignore */ }
}

async function fetchDocs() {
  try { await store.fetchDocs(sourceFilter.value) } catch { /* ignore */ }
}

async function deleteDoc(id) {
  try { await store.deleteDoc(id) } catch { /* ignore */ }
}

onMounted(() => {
  store.fetchDocs()
  store.fetchStats()
  store.fetchSyncConfigs()
})
</script>

<style scoped>
.knowledge-page { max-width: 1000px; margin: 0 auto; }
.page-head { margin-bottom: 20px; }
.page-title { margin: 0 0 4px 0; font-size: 20px; font-weight: 600; }
.page-desc { margin: 0; color: var(--color-ink-muted, #6b7280); font-size: 13px; }
.metrics-row { display: grid; grid-template-columns: repeat(2, 1fr); gap: 14px; margin-bottom: 18px; }
.card { background: var(--color-surface-1, #ffffff); border: 1px solid var(--color-hairline, #d3dbe8); border-radius: var(--radius-md, 8px); padding: 16px 18px; margin-bottom: 16px; }
.card__title { margin: 0 0 12px 0; font-size: 13px; font-weight: 600; color: var(--color-ink-muted, #6b7280); }
.flowbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 12px; }
.row-actions { display: inline-flex; gap: 4px; flex-wrap: wrap; }
.muted { color: var(--color-ink-muted, #6b7280); font-size: 12.5px; }
.poll-err { padding: 8px 12px; margin: 6px 0 12px; border-radius: var(--radius-sm, 6px); background: var(--color-error-soft, #fef2f2); border: 1px solid var(--color-error-light, #fecaca); color: var(--color-error, #ef4444); font-size: 12.5px; font-weight: 500; }
.search-bar { display: flex; gap: 8px; align-items: center; margin-bottom: 12px; }
.search-input { flex: 1; padding: 7px 9px; border: 1px solid var(--color-hairline, #d3dbe8); border-radius: var(--radius-sm, 6px); font-size: 13px; background: var(--color-surface-1, #ffffff); }
.filter-select { padding: 6px 8px; border: 1px solid var(--color-hairline, #d3dbe8); border-radius: var(--radius-sm, 6px); font-size: 12px; background: var(--color-surface-1, #ffffff); }
.chk { display: flex; align-items: center; gap: 4px; font-size: 12px; }
.search-result { padding: 10px 12px; margin-bottom: 8px; border: 1px solid var(--color-hairline, #d3dbe8); border-radius: var(--radius-sm, 6px); }
.result-title { font-weight: 600; font-size: 13px; margin-bottom: 4px; }
.result-content { font-size: 12px; color: var(--color-ink, #1f2937); margin-bottom: 4px; white-space: pre-wrap; }
.result-meta { display: flex; gap: 12px; font-size: 11px; color: var(--color-ink-muted, #6b7280); }
.result-citation { font-size: 11px; color: var(--color-ink-muted, #6b7280); font-style: italic; margin-top: 4px; }
.source-tag { font-size: 11px; padding: 2px 6px; background: var(--color-surface-2, #f7f9fc); border-radius: 4px; }
.status-on { color: var(--color-success, #059669); }
.status-off { color: var(--color-error, #ef4444); }
.metric-value { font-size: 24px; font-weight: 700; }
</style>