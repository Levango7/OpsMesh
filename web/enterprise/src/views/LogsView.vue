<template>
  <div>
    <h2>日志检索</h2>
    <p class="muted">按设备 / Agent / 级别 / 来源 / 关键字 / 时间区间检索日志，支持分页。</p>

    <div class="card">
      <div class="row">
        <div class="field">
          <label>设备 ID</label>
          <input v-model="store.filters.deviceID" placeholder="如 dev-10.0.0.1" />
        </div>
        <div class="field">
          <label>Agent</label>
          <input v-model="store.filters.agentID" />
        </div>
        <div class="field">
          <label>级别</label>
          <select v-model="store.filters.level">
            <option value="">全部</option>
            <option value="error">error</option>
            <option value="warn">warn</option>
            <option value="info">info</option>
          </select>
        </div>
        <div class="field">
          <label>来源</label>
          <select v-model="store.filters.source">
            <option value="">全部</option>
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
          <label>关键字</label>
          <input v-model="store.filters.keyword" placeholder="模糊匹配消息" />
        </div>
        <div class="field">
          <label>起始时间</label>
          <input v-model="store.filters.from" placeholder="2026-01-01T00:00:00Z" />
        </div>
        <div class="field">
          <label>结束时间</label>
          <input v-model="store.filters.to" />
        </div>
        <div class="field">
          <label>每页条数</label>
          <input v-model.number="store.filters.limit" type="number" min="10" max="1000" />
        </div>
      </div>
      <div class="btnbar">
        <button class="primary" @click="store.search(0)">🔍 查询</button>
        <button @click="onReset">清空</button>
      </div>
      <p v-if="store.error" class="msg err">{{ store.error }}</p>
    </div>

    <div class="card">
      <DataTable :columns="columns" :rows="store.list" empty-text="没有匹配的日志">
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
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import Pagination from '@/components/Pagination.vue'

const store = useLogStore()

const columns = [
  { key: 'timestamp', title: '时间', slot: 'cell-timestamp' },
  { key: 'level', title: '级别', slot: 'cell-level' },
  { key: 'source', title: '来源' },
  { key: 'deviceID', title: '设备', slot: 'cell-deviceID' },
  { key: 'agentID', title: 'Agent', slot: 'cell-agentID' },
  { key: 'message', title: '消息', slot: 'cell-message' }
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