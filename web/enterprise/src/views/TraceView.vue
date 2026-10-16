<template>
  <div>
    <h2 data-testid="trace-title">{{ $t('trace.title') }}</h2>
    <p class="muted">{{ $t('trace.desc') }}</p>

    <!-- 服务状态 -->
    <div v-if="servicesError" class="poll-err">
      <Icon name="warning" :size="14" />
      {{ $t('trace.services_unavailable') }}: {{ servicesError }}
      <button class="xs outline" @click="loadServices">{{ $t('common.refresh') }}</button>
    </div>

    <div class="query-card">
      <div class="flowbar">
        <h3 style="margin: 0">{{ $t('trace.query_title') }}</h3>
      </div>

      <div class="query-form">
      <div class="form-row">
        <div class="form-field">
          <label>{{ $t('trace.trace_id_label_short') }}</label>
          <input v-model="query.traceID" placeholder="trace ID" />
        </div>
        <div class="form-field">
          <label>{{ $t('trace.service') }}</label>
          <select v-model="query.service" @change="onServiceChange">
            <option value="">{{ $t('trace.all_services') }}</option>
            <option v-for="s in services" :key="s" :value="s">{{ s }}</option>
          </select>
        </div>
        <div class="form-field">
          <label>{{ $t('trace.operation') }}</label>
          <select v-model="query.operation">
            <option value="">{{ $t('trace.all_operations') }}</option>
            <option v-for="op in operations" :key="op" :value="op">{{ op }}</option>
          </select>
        </div>
        <div class="form-field">
          <label>{{ $t('trace.limit') }}</label>
          <select v-model="query.limit">
            <option value="10">10</option>
            <option value="20">20</option>
            <option value="50">50</option>
          </select>
        </div>
        <div class="form-field">
          <label>{{ $t('trace.lookback') }}</label>
          <select v-model="query.lookback">
            <option value="1h">1 小时</option>
            <option value="6h">6 小时</option>
            <option value="24h">24 小时</option>
          </select>
        </div>
      </div>
      <div class="form-row">
        <div class="form-field">
          <label>{{ $t('trace.min_duration') }}</label>
          <input v-model="query.minDuration" placeholder="100ms" />
        </div>
        <div class="form-field">
          <label>{{ $t('trace.tags') }}</label>
          <input v-model="query.tags" placeholder="key=value,key2=value2" />
        </div>
        <div class="form-field action-field">
          <button class="xs primary" @click="searchTraces" :disabled="queryLoading" data-testid="trace-search-btn">
            {{ $t('trace.search') }}
          </button>
          <button class="xs outline" @click="loadServices">↻ {{ $t('common.refresh') }}</button>
        </div>
      </div>
      </div>

      <div v-if="queryLoading" class="muted">{{ $t('trace.searching') }}</div>
    </div>

    <!-- Trace 结果 -->
    <div v-if="traces.length > 0" class="results-card">
      <div class="flowbar">
        <h3 style="margin: 0">{{ $t('trace.results') }} ({{ traces.length }})</h3>
      </div>
      <table class="data-table">
        <thead>
          <tr>
            <th>{{ $t('trace.trace_id') }}</th>
            <th>{{ $t('trace.service') }}</th>
            <th>{{ $t('trace.operation') }}</th>
            <th>{{ $t('trace.duration') }}</th>
            <th>{{ $t('trace.spans') }}</th>
            <th>{{ $t('trace.start_time') }}</th>
            <th>{{ $t('trace.action') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="t in traces" :key="t.traceID">
            <td>
              <code class="trace-id">{{ t.traceID }}</code>
            </td>
            <td>{{ t.service || '-' }}</td>
            <td>{{ t.operation || '-' }}</td>
            <td>{{ t.totalDurationMs }} ms</td>
            <td>{{ t.spanCount }}</td>
            <td>{{ fmtTime(t.startTimeMs) }}</td>
            <td>
              <button class="xs primary" @click="selectTrace(t)" data-testid="trace-detail-btn">
                {{ $t('trace.detail') }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-else-if="!queryLoading && searched" class="muted">{{ $t('trace.no_traces') }}</div>

    <!-- Trace 详情 -->
    <div v-if="selectedTrace" class="detail-card">
      <div class="flowbar">
        <h3 style="margin: 0">{{ $t('trace.detail_title') }}</h3>
        <button class="xs outline" @click="selectedTrace = null">{{ $t('trace.back') }}</button>
        <span class="muted">{{ $t('trace.trace_id_label') }}: {{ selectedTrace.traceID }}</span>
      </div>

      <div class="metrics-row">
        <MetricsCard :title="$t('trace.total_duration')" icon="clock">
          <div class="metric-value">{{ selectedTrace.totalDurationMs }} ms</div>
        </MetricsCard>
        <MetricsCard :title="$t('trace.span_count')" icon="task">
          <div class="metric-value">{{ selectedTrace.spanCount }}</div>
        </MetricsCard>
      </div>

      <div v-if="traceLoading" class="muted">{{ $t('trace.loading_detail') }}</div>

      <div v-else class="spans-tree">
        <h4>{{ $t('trace.spans') }}</h4>
        <div v-for="(span, i) in selectedTrace.spans" :key="i" class="span-node">
          <div class="span-header">
            <span class="span-name">{{ span.name }}</span>
            <span class="span-kind">{{ span.kind || 'internal' }}</span>
            <span class="span-duration">{{ span.durationMs }} ms</span>
            <span class="span-time">{{ fmtTime(span.startTimeMs) }}</span>
          </div>
          <div v-if="span.tags && Object.keys(span.tags).length" class="span-tags">
            <span class="tag-label">{{ $t('trace.tags') }}:</span>
            <span v-for="(v, k) in span.tags" :key="k" class="tag">{{ k }}={{ v }}</span>
          </div>
          <div v-if="span.children.length > 0" class="span-children">
            <div v-for="(child, j) in span.children" :key="j" class="span-node child">
              <div class="span-header">
                <span class="span-name">{{ child.name }}</span>
                <span class="span-kind">{{ child.kind || 'internal' }}</span>
                <span class="span-duration">{{ child.durationMs }} ms</span>
              </div>
              <div v-if="child.tags && Object.keys(child.tags).length" class="span-tags">
                <span class="tag-label">{{ $t('trace.tags') }}:</span>
                <span v-for="(v, k) in child.tags" :key="k" class="tag">{{ k }}={{ v }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { reactive, ref, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import {
  getTraceServices,
  getTraceServiceOperations,
  getTraces,
  getTrace,
  normalizeTrace,
} from '@/api/trace'
import { t } from '@/i18n'
import Icon from '@/components/Icon.vue'
import MetricsCard from '@/components/MetricsCard.vue'

const route = useRoute()

const query = reactive({
  service: '',
  operation: '',
  limit: '20',
  lookback: '1h',
  minDuration: '',
  tags: '',
  traceID: '',
})

const services = reactive([])
const operations = reactive([])
const traces = reactive([])
const selectedTrace = ref(null)

const queryLoading = ref(false)
const traceLoading = ref(false)
const servicesError = ref(null)
const searched = ref(false)

async function loadServices() {
  try {
    const resp = await getTraceServices()
    services.splice(0, services.length, ...(resp.data || []))
  } catch (e) {
    servicesError.value = e.j?.error || e.s
  }
}

async function onServiceChange() {
  if (!query.service) {
    operations.splice(0, operations.length)
    return
  }
  try {
    const resp = await getTraceServiceOperations(query.service)
    operations.splice(0, operations.length, ...(resp.data || []))
  } catch {
    operations.splice(0, operations.length)
  }
}

async function searchTraces() {
  queryLoading.value = true
  searched.value = true
  traces.splice(0, traces.length)
  selectedTrace.value = null
  try {
    // 如果指定了 traceID，直接查看详情
    if (query.traceID) {
      await loadTraceFromQuery(query.traceID)
      return
    }
    const params = {
      service: query.service || undefined,
      operation: query.operation || undefined,
      limit: parseInt(query.limit, 10) || 20,
      lookback: query.lookback,
      minDuration: query.minDuration || undefined,
      tags: query.tags || undefined,
    }
    const resp = await getTraces(params)
    const raw = resp.data || []
    raw.forEach((t) => {
      const normalized = normalizeTrace(t)
      if (normalized) traces.push(normalized)
    })
  } catch (e) {
    servicesError.value = e.j?.error || e.s
  } finally {
    queryLoading.value = false
  }
}

async function selectTrace(trace) {
  traceLoading.value = true
  try {
    const resp = await getTrace(trace.traceID)
    selectedTrace.value = normalizeTrace(resp)
  } catch (e) {
    servicesError.value = e.j?.error || e.s
  } finally {
    traceLoading.value = false
  }
}

// 从 URL query 参数加载 trace 详情（用于从告警页跳转）
async function loadTraceFromQuery(traceID) {
  if (!traceID) return
  traceLoading.value = true
  try {
    const resp = await getTrace(traceID)
    selectedTrace.value = normalizeTrace(resp)
  } catch (e) {
    servicesError.value = e.j?.error || e.s
  } finally {
    traceLoading.value = false
  }
}

function fmtTime(ms) {
  if (!ms) return '-'
  return new Date(ms).toLocaleString()
}

onMounted(() => {
  loadServices()
  // 从 URL query 参数加载 trace 详情（用于从告警页跳转）
  if (route.query.traceID) {
    loadTraceFromQuery(route.query.traceID)
  }
})

watch(
  () => query.service,
  () => {
    onServiceChange()
  }
)
</script>

<style scoped>
.query-card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 16px;
  margin-bottom: 14px;
}
.query-form { display: contents; }
.form-row { display: flex; gap: 14px; flex-wrap: wrap; align-items: flex-end; }
.form-field { display: flex; flex-direction: column; gap: 4px; min-width: 140px; }
.form-field label { font-size: 12px; color: var(--text-2); }
.form-field input, .form-field select {
  padding: 6px 8px; border: 1px solid var(--border); border-radius: var(--radius-sm); font-size: 13px;
}
.action-field { display: flex; align-items: flex-end; gap: 8px; }
.results-card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 16px;
  margin-bottom: 14px;
}
.detail-card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 16px;
  margin-top: 14px;
}
.data-table { width: 100%; border-collapse: collapse; margin-top: 10px; font-size: 13px; }
.data-table th, .data-table td { border: 1px solid var(--border); padding: 6px 8px; text-align: left; }
.data-table th { background: var(--muted); color: var(--text-2); font-size: 12px; }
.data-table tr:hover { background: var(--muted-soft); }
.trace-id { font-size: 11px; font-family: monospace; color: var(--text-2); }
.spans-tree { margin-top: 12px; }
.span-node {
  background: var(--muted-soft);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 8px 12px;
  margin-bottom: 6px;
}
.span-node.child {
  margin-left: 24px;
  background: var(--surface);
  border-left: 3px solid var(--accent);
}
.span-header { display: flex; gap: 10px; align-items: center; margin-bottom: 4px; flex-wrap: wrap; }
.span-name { font-weight: 600; color: var(--text); font-size: 13px; }
.span-kind { font-size: 11px; padding: 2px 6px; border-radius: 10px; background: var(--muted); color: var(--text-2); }
.span-duration { font-size: 11px; color: var(--text-2); font-family: monospace; }
.span-time { font-size: 11px; color: var(--text-2); font-family: monospace; }
.span-tags { margin-top: 4px; }
.tag-label { font-size: 11px; color: var(--text-2); font-weight: 600; margin-right: 6px; }
.tag { font-size: 11px; padding: 1px 5px; border: 1px solid var(--border); border-radius: 4px; color: var(--text-2); margin-right: 4px; }
.metrics-row { display: grid; grid-template-columns: repeat(2, 1fr); gap: 14px; margin-bottom: 14px; }
.poll-err { padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm); background: var(--fail-soft); border: 1px solid var(--fail-bg); color: var(--fail); font-size: 12.5px; font-weight: 500; }
</style>