<template>
  <PageShell
    :title="$t('rag.title')"
    :desc="$t('rag.desc')"
    testid="ai-rag"
    :error="store.error"
  >
    <!-- AI 网关健康：LLM 提供商可达性（非进程探针，后端 /ai/health） -->
    <div v-if="store.health.length || store.healthError" class="card card--flat health-strip">
      <span class="status-label">{{ $t('rag.health_title') }}</span>
      <span v-if="store.healthError" class="status-pill off">{{ store.healthError }}</span>
      <span v-for="p in store.health" :key="p.name" class="status-pill" :class="p.ok === false ? 'bad' : 'ok'">
        {{ p.name || p.provider || '-' }}
      </span>
      <Button variant="ghost" size="sm" @click="store.fetchHealth()"><Icon name="refresh" :size="13" /></Button>
    </div>

    <!-- 检索 -->
    <div class="card">
      <h3>{{ $t('rag.search_title') }}</h3>
      <form class="flowbar" @submit.prevent="onSearch">
        <div class="field field--fixed">
          <label>{{ $t('rag.search_title') }}</label>
          <input v-model.trim="query" type="text" :placeholder="$t('rag.search_placeholder')" data-testid="ai-rag-query" />
        </div>
        <Button variant="primary" size="sm" native-type="submit" :disabled="store.loading" data-testid="ai-rag-search-btn">
          <Icon name="search" :size="13" /> {{ $t('rag.search_btn') }}
        </Button>
      </form>

      <div v-if="store.loading" class="muted">{{ $t('common.loading') }}</div>
      <template v-else-if="store.searched">
        <div v-if="!store.results.length" class="muted">{{ $t('rag.noResults') }}</div>
        <DataTable
          v-else
          :columns="columns"
          :rows="store.results"
          row-key="id"
          :empty-text="$t('rag.noResults')"
        >
          <template #cell-score="{ value }">
            <span class="muted">{{ fmtScore(value) }}</span>
          </template>
          <template #cell-metadata="{ value }">
            <code>{{ fmtMeta(value) }}</code>
          </template>
        </DataTable>
      </template>
      <div v-else class="muted">{{ $t('rag.empty') }}</div>
    </div>

    <!-- 入库 -->
    <div class="card">
      <h3>{{ $t('rag.ingest_title') }}</h3>
      <form class="entity-form" @submit.prevent="onIngest">
        <div class="field">
          <label>{{ $t('rag.ingest_title') }}</label>
          <textarea v-model="ingestText" rows="5" :placeholder="$t('rag.ingest_placeholder')" data-testid="ai-rag-ingest" />
        </div>
        <div class="field">
          <label>{{ $t('rag.metadata_label') }}</label>
          <input
            v-model.trim="ingestMeta"
            type="text"
            :placeholder="$t('rag.metadata_placeholder')"
            data-testid="ai-rag-ingest-meta"
          />
        </div>
        <div v-if="ingestMetaError" class="poll-err">{{ ingestMetaError }}</div>
        <div v-if="ingestMsg" class="msg ok">{{ ingestMsg }}</div>
        <div class="btnbar btnbar--bottom">
          <Button variant="primary" size="md" native-type="submit" :disabled="store.loading" data-testid="ai-rag-ingest-btn">
            <Icon name="add" :size="14" /> {{ $t('rag.ingest_btn') }}
          </Button>
        </div>
      </form>
    </div>
  </PageShell>
</template>

<script setup>
// RAG 检索/入库页（2026-11-02）。
// 背景：后端 POST /ai/rag/search、/ai/rag/ingest 与 GET /ai/health 早就有、api 层
// 也早就接好，但没有任何界面消费方（死 API 面清查里 ragSearch/ragIngest/getAIHealth
// 三条零引用）。本页补齐：检索结果展示 content/score/metadata——**embedding 不展示**
// （它是向量本身，对用户无意义且可能很大）；入库按行拆分，embedding 交后端生成。
// 权限：路由门挂 ai:read；入库是 ai:write，无权限时后端 403 由 PageShell 横幅透出。
import { computed, onMounted, ref } from 'vue'
import { t } from '@/i18n'
import PageShell from '@/components/PageShell.vue'
import DataTable from '@/components/DataTable.vue'
import Button from '@/components/ui/Button.vue'
import Icon from '@/components/Icon.vue'
import { useAIRagStore } from '@/stores/ai-rag'

const store = useAIRagStore()

const query = ref('')
const ingestText = ref('')
const ingestMeta = ref('')
const ingestMetaError = ref('')
const ingestMsg = ref('')

const columns = computed(() => [
  { key: 'score', label: t('rag.col_score'), width: '96px' },
  { key: 'content', label: t('rag.col_content') },
  { key: 'metadata', label: t('rag.col_meta') }
])

// 后端给的是 0~1 的浮点分数；保留两位并转百分位，避免裸浮点数进界面。
function fmtScore(v) {
  if (v == null) return '-'
  return (Number(v) * 100).toFixed(1) + '%'
}

function fmtMeta(v) {
  if (!v || typeof v !== 'object') return '-'
  const entries = Object.entries(v)
  if (!entries.length) return '-'
  return entries.map(([k, val]) => `${k}=${val}`).join(', ')
}

async function onSearch() {
  if (!query.value.trim()) return
  await store.search(query.value)
}

async function onIngest() {
  ingestMsg.value = ''
  ingestMetaError.value = ''
  // 元数据（裁决③ 2026-11-04）：可选单行 JSON，多行入库共享同一份；解析失败不下发。
  let metadata = undefined
  if (ingestMeta.value) {
    try {
      const parsed = JSON.parse(ingestMeta.value)
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('not an object')
      metadata = parsed
    } catch {
      ingestMetaError.value = t('rag.metadata_invalid')
      return
    }
  }
  const n = await store.ingest(ingestText.value, metadata)
  if (n > 0) {
    ingestMsg.value = t('rag.ingested_ok', { n })
    ingestText.value = ''
    if (metadata) ingestMeta.value = ''
  }
}

onMounted(() => store.fetchHealth())
</script>

<style scoped>
/* 共享原语（.card/.card--flat/.flowbar/.field/.btnbar/.msg/.muted/.status-*）
   全部在 styles/pages.css；本页仅健康状态条一处页面专属排布。 */
.health-strip {
  display: flex;
  align-items: center;
  gap: var(--space-2, 8px);
}
</style>
