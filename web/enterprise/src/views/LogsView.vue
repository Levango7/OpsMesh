<template>
  <div>
    <h2>{{ $t('logs.title') }}</h2>
    <p class="muted">{{ $t('logs.subtitle') }}</p>

    <div class="card">
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
          <input v-model="store.filters.keyword" :placeholder="$t('logs.keyword_placeholder')" />
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
      <div class="btnbar">
        <button class="primary" @click="store.search(0)">{{ $t('logs.search_btn') }}</button>
        <button @click="onReset">{{ $t('logs.reset_btn') }}</button>
      </div>
      <p v-if="store.error" class="msg err">{{ store.error }}</p>
    </div>

    <div class="card">
      <DataTable :columns="columns" :rows="store.list" :empty-text="$t('logs.empty')">
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
import { onMounted } from 'vue'
import { useLogStore } from '@/stores/log'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import Pagination from '@/components/Pagination.vue'

const store = useLogStore()

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

onMounted(() => { /* 不自动查询，等用户点查询 */ })
</script>

<style scoped>
.field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 8px; min-width: 200px; }
.field label { margin: 0; }
</style>
