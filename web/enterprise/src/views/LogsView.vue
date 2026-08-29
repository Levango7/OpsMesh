<template>
  <div>
    <h2>{{ $t('logs.title') }}</h2>
    <p class="muted">{{ $t('logs.subtitle') }}</p>

    <div class="card">
      <!-- 模式切换 Tab -->
      <div class="mode-tabs" role="tablist">
        <button
          role="tab"
          :class="{ active: store.mode === 'simple' }"
          @click="store.setMode('simple')"
        >{{ $t('logs.simpleSearch') }}</button>
        <button
          role="tab"
          :class="{ active: store.mode === 'advanced' }"
          @click="store.setMode('advanced')"
        >{{ $t('logs.advancedQuery') }}</button>
      </div>

      <!-- 简单搜索：字段过滤 -->
      <template v-if="store.mode === 'simple'">
        <div class="row">
          <div class="field">
            <label>{{ $t('logs.device_id_label') }}</label>
            <input v-model="store.filters.deviceID" :placeholder="$t('logs.device_id_placeholder')" />
          </div>
          <div class="field">
            <label>{{ $t('logs.agent_label') }}</label>
            <input v-model="store.filters.agentID" />
          </div>
          <div class="field">
            <label>{{ $t('logs.level_label') }}</label>
            <select v-model="store.filters.level">
              <option value="">{{ $t('common.all') }}</option>
              <option value="error">error</option>
              <option value="warn">warn</option>
              <option value="info">info</option>
            </select>
          </div>
          <div class="field">
            <label>{{ $t('logs.source_label') }}</label>
            <select v-model="store.filters.source">
              <option value="">{{ $t('common.all') }}</option>
              <option value="agent">agent</option>
              <option value="task">task</option>
              <option value="workflow">workflow</option>
              <option value="deploy">deploy</option>
              <option value="system">system</option>
            </select>
          </div>
        </div>
        <div class="row">
          <div class="field">
            <label>{{ $t('logs.keyword_label') }}</label>
            <input
              v-model="store.filters.keyword"
              :placeholder="$t('logs.keyword_placeholder')"
              @keyup.enter="store.search(0)"
            />
          </div>
          <div class="field">
            <label>{{ $t('logs.from_label') }}</label>
            <input v-model="store.filters.from" placeholder="2026-01-01T00:00:00Z" />
          </div>
          <div class="field">
            <label>{{ $t('logs.to_label') }}</label>
            <input v-model="store.filters.to" />
          </div>
          <div class="field">
            <label>{{ $t('logs.limit_label') }}</label>
            <input v-model.number="store.filters.limit" type="number" min="10" max="1000" />
          </div>
        </div>
      </template>

      <!-- 高级查询：KQL/Lucene 语法 -->
      <template v-else>
        <div class="qfield">
          <label>{{ $t('logs.querySyntax') }}</label>
          <input
            v-model="store.q"
            class="qinput"
            :placeholder="$t('logs.querySyntaxHint')"
            @keyup.enter="store.search(0)"
          />
        </div>
        <div class="row">
          <div class="field">
            <label>{{ $t('logs.from_label') }}</label>
            <input v-model="store.filters.from" placeholder="2026-01-01T00:00:00Z" />
          </div>
          <div class="field">
            <label>{{ $t('logs.to_label') }}</label>
            <input v-model="store.filters.to" />
          </div>
          <div class="field">
            <label>{{ $t('logs.limit_label') }}</label>
            <input v-model.number="store.filters.limit" type="number" min="10" max="1000" />
          </div>
        </div>

        <!-- 语法提示面板（可折叠） -->
        <div class="syntax-hint">
          <button class="hint-toggle" @click="hintOpen = !hintOpen">
            <span class="caret">{{ hintOpen ? '▾' : '▸' }}</span>
            <span>{{ $t('logs.querySyntaxHint') }}</span>
          </button>
          <div v-if="hintOpen" class="hint-body">
            <div class="hint-grid">
              <div>
                <h4>{{ $t('logs.supportedFields') }}</h4>
                <ul class="kw-list">
                  <li><code>level</code></li>
                  <li><code>device</code></li>
                  <li><code>agent</code></li>
                  <li><code>source</code></li>
                  <li><code>message</code></li>
                  <li><code>task</code></li>
                </ul>
              </div>
              <div>
                <h4>{{ $t('logs.supportedOperators') }}</h4>
                <ul class="kw-list">
                  <li><code>=</code> {{ $t('logs.opEqual') }}</li>
                  <li><code>!=</code> {{ $t('logs.opNotEqual') }}</li>
                  <li><code>~</code> {{ $t('logs.opContains') }}</li>
                  <li><code>!~</code> {{ $t('logs.opNotContains') }}</li>
                  <li><code>AND OR NOT</code> {{ $t('logs.opLogical') }}</li>
                  <li><code>( )</code> {{ $t('logs.opGroup') }}</li>
                </ul>
              </div>
            </div>
            <h4>{{ $t('logs.examples') }}</h4>
            <ul class="example-list">
              <li v-for="ex in examples" :key="ex.q">
                <code class="example-q">{{ ex.q }}</code>
                <span class="muted"> — {{ $t(ex.descKey) }}</span>
                <button class="xs outline try-btn" @click="tryExample(ex.q)">{{ $t('logs.tryIt') }}</button>
              </li>
            </ul>
          </div>
        </div>
      </template>

      <div class="btnbar">
        <button class="primary" @click="store.search(0)">{{ $t('logs.search_btn') }}</button>
        <button @click="onReset">{{ $t('logs.reset_btn') }}</button>
      </div>
      <p v-if="store.error" class="msg err">{{ store.error }}</p>
    </div>

    <div class="card">
      <DataTable :columns="columns" :rows="store.list" :loading="store.loading" :empty-text="$t('logs.empty')">
        <template #cell-timestamp="{ value }">
          <small class="muted">{{ fmtTs(value) }}</small>
        </template>
        <template #cell-level="{ value }">
          <StatusBadge :status="levelStatus(value)" :text="value" />
        </template>
        <template #cell-deviceID="{ value }"><code>{{ value || '' }}</code></template>
        <template #cell-agentID="{ value }"><code>{{ value || '' }}</code></template>
        <template #cell-message="{ value }">
          <span style="white-space: pre-wrap">{{ value || '' }}</span>
        </template>
      </DataTable>
      <Pagination
        :page="store.page"
        :page-size="store.pageSize"
        :limit="store.filters.limit"
        @prev="store.prev()"
        @next="store.next()"
      />
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { onMounted } from 'vue'
import { useLogStore } from '@/stores/log'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import Pagination from '@/components/Pagination.vue'

const store = useLogStore()

// 语法提示面板默认折叠
const hintOpen = ref(false)

// 示例查询：q 为语法字符串，descKey 为 i18n 键
const examples = [
  { q: 'level=error', descKey: 'logs.example1' },
  { q: 'level=error AND device=dev-1', descKey: 'logs.example2' },
  { q: 'source=task AND (level=warn OR level=error)', descKey: 'logs.example3' },
  { q: 'message~"panic"', descKey: 'logs.example4' },
  { q: 'level!=info', descKey: 'logs.example5' }
]

const columns = [
  { key: 'timestamp', title: t('logs.col_time'), slot: 'cell-timestamp' },
  { key: 'level', title: t('logs.col_level'), slot: 'cell-level' },
  { key: 'source', title: t('logs.col_source') },
  { key: 'deviceID', title: t('logs.col_device'), slot: 'cell-deviceID' },
  { key: 'agentID', title: t('logs.agent_label'), slot: 'cell-agentID' },
  { key: 'message', title: t('logs.col_message'), slot: 'cell-message' }
]

function fmtTs(s) {
  return (s || '').toString().replace('T', ' ').replace('Z', '')
}
function levelStatus(lv) {
  return lv === 'error' ? 'error' : lv === 'warn' ? 'warn' : 'info'
}
function onReset() {
  store.reset()
}
// 点击示例"试一下"：填入查询框并立即查询
function tryExample(q) {
  store.q = q
  store.search(0)
}

onMounted(() => { /* 不自动查询，等用户点查询 */ })
</script>

<style scoped>
.field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 8px; min-width: 200px; }
.field label { margin: 0; }

/* 模式切换 Tab */
.mode-tabs {
  display: inline-flex;
  gap: 4px;
  margin-bottom: 14px;
  padding: 3px;
  background: var(--surface-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
}
.mode-tabs button {
  border: none;
  background: transparent;
  color: var(--text-2);
  padding: 6px 16px;
  border-radius: 6px;
  font-weight: 500;
}
.mode-tabs button.active {
  background: var(--surface-2);
  color: var(--accent);
  font-weight: 600;
  box-shadow: 0 1px 2px rgba(31,37,64,.08);
}

/* 高级查询输入框 */
.qfield { display: flex; flex-direction: column; gap: 4px; margin-bottom: 10px; }
.qfield label { margin: 0; color: var(--text); font-weight: 500; }
.qinput {
  font-family: var(--font-mono);
  font-size: var(--fs-base);
  width: 100%;
  padding: 9px 12px;
}

/* 语法提示面板 */
.syntax-hint {
  margin: 10px 0 4px;
  border: 1px dashed var(--border-2);
  border-radius: var(--radius-sm);
  background: var(--surface-2);
}
.hint-toggle {
  width: 100%;
  text-align: left;
  border: none;
  background: transparent;
  padding: 8px 12px;
  color: var(--text-2);
  font-weight: 500;
  display: flex;
  align-items: center;
  gap: 8px;
}
.hint-toggle:hover { color: var(--accent); }
.caret { font-size: 12px; color: var(--text-3); }
.hint-body { padding: 4px 14px 14px; }
.hint-body h4 { margin: 10px 0 6px; color: var(--text); }
.hint-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 18px;
  margin-bottom: 8px;
}
@media (max-width: 600px) {
  .hint-grid { grid-template-columns: 1fr; }
}
.kw-list, .example-list {
  list-style: none;
  padding: 0;
  margin: 0;
}
.kw-list li, .example-list li {
  padding: 4px 0;
  font-size: var(--fs-sm);
  color: var(--text-2);
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.example-q { flex: 0 0 auto; }
.try-btn { margin-left: auto; }

/* 深色主题下提示面板背景 */
:global([data-theme="dark"]) .syntax-hint { background: var(--surface-3); }
</style>
