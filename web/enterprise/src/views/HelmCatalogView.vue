<template>
  <div>
    <h2 data-testid="helm-catalog-title">{{ $t('helm.catalog_title') }}</h2>
    <p class="muted">{{ $t('helm.catalog_subtitle') }}</p>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <!-- 搜索框 -->
    <div class="btnbar">
      <div class="search-box">
        <Icon name="search" :size="14" />
        <input
          v-model.trim="query"
          type="text"
          :placeholder="$t('helm.search_placeholder')"
          @keyup.enter="onSearch"
          data-testid="helm-catalog-search-input"
        />
        <button class="primary xs" @click="onSearch" data-testid="helm-catalog-search-btn">{{ $t('common.search') }}</button>
      </div>
      <button class="outline" @click="loadCatalog"><Icon name="refresh" :size="14" /> {{ $t('helm.catalog_btn') }}</button>
    </div>

    <!-- 搜索结果模式 -->
    <div v-if="searchMode">
      <h3>{{ $t('helm.search_results') }}</h3>
      <div v-if="store.loading && !store.charts.length" class="muted">{{ $t('common.loading') }}</div>
      <DataTable
        v-else
        :columns="chartColumns"
        :rows="store.charts"
        row-key="name"
        :loading="store.loading"
        :empty-text="$t('helm.empty_charts')"
      >
        <template #cell-name="{ row }">
          <a class="chart-link" @click="openChart(row)"><code>{{ row.name }}</code></a>
        </template>
        <template #cell-version="{ value }"><span class="tag">{{ value }}</span></template>
        <template #cell-appVersion="{ value }"><span class="tag">{{ value }}</span></template>
        <template #cell-actions="{ row }">
          <button class="xs outline" @click="openChart(row)" data-testid="helm-chart-detail-btn"><Icon name="edit" :size="13" /></button>
        </template>
      </DataTable>
    </div>

    <!-- 预置目录模式 -->
    <div v-else>
      <div v-if="store.loading && !store.catalog.length" class="muted">{{ $t('common.loading') }}</div>
      <div v-else>
        <div v-for="cat in store.catalog" :key="cat.name" class="catalog-cat">
          <h3>{{ cat.name }} <small class="muted">· {{ cat.description }}</small></h3>
          <DataTable
            :columns="chartColumns"
            :rows="cat.charts || []"
            row-key="name"
            :empty-text="$t('helm.empty_charts')"
          >
            <template #cell-name="{ row }">
              <a class="chart-link" @click="openChart(row)"><code>{{ row.name }}</code></a>
            </template>
            <template #cell-version="{ value }"><span class="tag">{{ value }}</span></template>
            <template #cell-appVersion="{ value }"><span class="tag">{{ value }}</span></template>
            <template #cell-actions="{ row }">
              <button class="xs outline" @click="openChart(row)"><Icon name="edit" :size="13" /></button>
            </template>
          </DataTable>
        </div>
        <div v-if="!store.catalog.length" class="muted">{{ $t('helm.empty_catalog') }}</div>
      </div>
    </div>

    <!-- Chart 详情抽屉 -->
    <DetailDrawer :open="!!currentChart" :title="currentChart ? currentChart.name : ''" @close="currentChart = null">
      <div v-if="currentChart" class="chart-detail">
        <div class="field"><label>{{ $t('helm.field_chart_name') }}</label><code>{{ currentChart.name }}</code></div>
        <div class="field"><label>{{ $t('helm.field_chart_repo') }}</label><code>{{ currentChart.repo }}</code></div>
        <div class="field"><label>{{ $t('helm.field_chart_version') }}</label><span class="tag">{{ currentChart.version }}</span></div>
        <div class="field"><label>{{ $t('helm.field_app_version') }}</label><span class="tag">{{ currentChart.appVersion }}</span></div>
        <div class="field"><label>{{ $t('helm.field_chart_desc') }}</label><p>{{ currentChart.description || '—' }}</p></div>
        <div class="field"><label>{{ $t('helm.field_chart_home') }}</label><code>{{ currentChart.home || '—' }}</code></div>
        <div class="field">
          <label>{{ $t('helm.field_chart_keywords') }}</label>
          <span v-if="currentChart.keywords && currentChart.keywords.length" class="keywords">
            <span v-for="kw in currentChart.keywords" :key="kw" class="tag">{{ kw }}</span>
          </span>
          <span v-else>—</span>
        </div>
        <div class="btnbar">
          <button class="primary" @click="goInstall" data-testid="helm-chart-install-btn">
            <Icon name="success" :size="14" /> {{ $t('helm.install_btn') }}
          </button>
        </div>
      </div>
    </DetailDrawer>

    <!-- 错误提示 -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="helm-catalog-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// Helm 应用目录页 — 预置分类 + Chart 搜索 + Chart 详情
import { ref, computed, reactive, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useHelmStore } from '@/stores/helm'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'

const store = useHelmStore()
const router = useRouter()
const query = ref('')
const searchMode = ref(false)
const currentChart = ref(null)
const errorConfirm = reactive({ show: false, message: '' })

const chartColumns = computed(() => [
  { key: 'name', title: t('helm.field_chart_name'), slot: 'cell-name' },
  { key: 'repo', title: t('helm.field_chart_repo') },
  { key: 'version', title: t('helm.field_chart_version'), slot: 'cell-version', width: '100px' },
  { key: 'appVersion', title: t('helm.field_app_version'), slot: 'cell-appVersion', width: '110px' },
  { key: 'description', title: t('helm.field_chart_desc') },
  { key: 'actions', title: t('helm.col_actions'), slot: 'cell-actions', width: '70px' }
])

async function loadCatalog() {
  searchMode.value = false
  try {
    await store.fetchCatalog()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('error.helmCatalogFailed')
    errorConfirm.show = true
  }
}

async function onSearch() {
  if (!query.value) {
    searchMode.value = false
    await loadCatalog()
    return
  }
  searchMode.value = true
  try {
    await store.searchCharts(query.value)
  } catch (e) {
    errorConfirm.message = e.j?.error || t('error.helmSearchFailed')
    errorConfirm.show = true
  }
}

function openChart(row) {
  currentChart.value = row
}

function goInstall() {
  // 跳转到 Release 管理页安装；通过 query 传递预填参数
  const chart = currentChart.value
  currentChart.value = null
  router.push({
    name: 'helm-releases',
    query: { chart: chart.name, repo: chart.repo, version: chart.version }
  })
}

onMounted(loadCatalog)
</script>

<style scoped>
.btnbar { display: flex; gap: 8px; margin-bottom: 12px; align-items: center; }
.search-box {
  display: flex; align-items: center; gap: 6px; flex: 1; max-width: 480px;
  padding: 4px 8px; border: 1px solid var(--border); border-radius: var(--radius-sm);
  background: var(--bg-1);
}
.search-box input { flex: 1; border: none; background: transparent; outline: none; font-size: 13px; }

.poll-err {
  padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}

.catalog-cat { margin-bottom: 18px; }
.catalog-cat h3 { margin: 0 0 8px 0; font-size: 14px; }
.catalog-cat h3 small { font-size: 12px; color: var(--text-3); font-weight: normal; }

.chart-link { cursor: pointer; }
.chart-link:hover code { color: var(--accent); }

.chart-detail { display: flex; flex-direction: column; gap: 12px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field > label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.keywords { display: flex; flex-wrap: wrap; gap: 4px; }
.btnbar { margin-top: 8px; }

.tag {
  display: inline-block; padding: 2px 8px; border-radius: 999px;
  font-size: 12px; background: var(--accent-soft); color: var(--accent);
}
</style>