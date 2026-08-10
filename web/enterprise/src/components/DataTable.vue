<template>
  <div class="dt-wrap">
    <table class="data-table">
      <thead>
        <tr>
          <th
            v-for="col in columns"
            :key="col.key"
            :style="col.width ? { width: col.width } : null"
            :class="{ sortable: col.sortable, sorted: activeSortKey === col.key }"
            @click="col.sortable ? handleSort(col) : null"
          >
            <span class="th-content">
              <span class="th-label">{{ col.title }}</span>
              <span v-if="col.sortable" class="sort-indicator">
                <template v-if="activeSortKey === col.key">
                  {{ activeSortOrder === 'asc' ? '▲' : '▼' }}
                </template>
                <template v-else>⇅</template>
              </span>
            </span>
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="!sortedRows || sortedRows.length === 0">
          <td :colspan="columns.length" class="empty">{{ emptyText || $t('common.no_data') }}</td>
        </tr>
        <tr
          v-for="(row, idx) in sortedRows"
          :key="rowKey ? row[rowKey] : idx"
          :class="[rowClass ? rowClass(row) : '', clickable ? 'clickable' : '']"
          @click="clickable ? $emit('row-click', row) : null"
        >
          <td v-for="col in columns" :key="col.key">
            <slot v-if="col.slot" :name="col.slot" :row="row" :value="row[col.key]" />
            <template v-else>{{ formatCell(row, col) }}</template>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup>
// 通用数据表格 — 支持自定义单元格 slot、格式化函数、行点击、排序
import { computed, ref } from 'vue'

const props = defineProps({
  columns: { type: Array, required: true },     // [{ key, title, width?, slot?, format?, sortable?, sort? }]
  rows: { type: Array, default: () => [] },
  rowKey: { type: String, default: '' },
  clickable: { type: Boolean, default: false },
  rowClass: { type: Function, default: null },
  emptyText: { type: String, default: '' },
  // 外部受控排序：传入则使用外部值，否则使用内部状态
  sortKey: { type: String, default: '' },
  sortOrder: { type: String, default: 'asc' }   // 'asc' | 'desc'
})
const emit = defineEmits(['row-click', 'sort-change'])

// 内部排序状态（非受控模式）
const internalSortKey = ref('')
const internalSortOrder = ref('asc')

// 实际生效的排序键：优先外部 prop，回退内部状态
const activeSortKey = computed(() => props.sortKey || internalSortKey.value)
// 实际生效的排序方向：受控时跟随 prop，非受控时跟随内部状态
const activeSortOrder = computed(() => {
  if (!activeSortKey.value) return 'asc'
  return props.sortKey ? props.sortOrder : internalSortOrder.value
})

// 排序后的数据
const sortedRows = computed(() => {
  const data = props.rows || []
  const key = activeSortKey.value
  if (!key) return data
  const col = props.columns.find(c => c.key === key)
  const order = activeSortOrder.value
  const sorted = [...data].sort((a, b) => {
    const valA = a[key]
    const valB = b[key]
    // 自定义排序函数
    if (col && typeof col.sort === 'function') {
      return order === 'asc' ? col.sort(valA, valB) : col.sort(valB, valA)
    }
    // null/undefined 统一排到末尾
    if (valA == null && valB == null) return 0
    if (valA == null) return 1
    if (valB == null) return -1
    // 数字排序
    if (typeof valA === 'number' && typeof valB === 'number') {
      return order === 'asc' ? valA - valB : valB - valA
    }
    // 字符串排序（localeCompare 支持中文）
    const cmp = String(valA).localeCompare(String(valB))
    return order === 'asc' ? cmp : -cmp
  })
  return sorted
})

// 点击表头切换排序：asc → desc → 取消
function handleSort(col) {
  if (!col || !col.sortable) return
  // 受控模式：仅通知外部由其更新 prop
  if (props.sortKey) {
    let newKey = col.key
    let newOrder = 'asc'
    if (props.sortKey === col.key) {
      if (props.sortOrder === 'asc') newOrder = 'desc'
      else { newKey = ''; newOrder = 'asc' }
    }
    emit('sort-change', { key: newKey, order: newOrder })
    return
  }
  // 非受控模式：直接更新内部状态
  if (internalSortKey.value === col.key) {
    if (internalSortOrder.value === 'asc') {
      internalSortOrder.value = 'desc'
    } else {
      internalSortKey.value = ''
      internalSortOrder.value = 'asc'
    }
  } else {
    internalSortKey.value = col.key
    internalSortOrder.value = 'asc'
  }
  emit('sort-change', { key: internalSortKey.value, order: internalSortOrder.value })
}

function formatCell(row, col) {
  const v = row[col.key]
  if (typeof col.format === 'function') return col.format(v, row)
  if (v == null) return ''
  return v
}
</script>

<style scoped>
.dt-wrap { overflow-x: auto; }
.data-table {
  border-collapse: separate; border-spacing: 0; width: 100%;
  margin-top: 8px; font-size: 13px;
  overflow: hidden; border-radius: var(--radius-sm);
}
.data-table th, .data-table td {
  text-align: left; padding: 9px 12px; border-bottom: 1px solid var(--border);
}
.data-table th {
  background: var(--surface-3); color: var(--text-2);
  font-weight: 600; font-size: 12px; letter-spacing: .02em;
}
.data-table tr:last-child td { border-bottom: none; }
.data-table tbody tr { transition: .12s; }
.data-table tbody tr:hover { background: var(--bg-soft); }
.data-table tr.clickable { cursor: pointer; }
.data-table tr.clickable:hover { background: var(--accent-soft); }
.data-table td.empty { text-align: center; color: var(--text-3); padding: 18px; }

/* 排序相关样式 */
.data-table th.sortable { cursor: pointer; user-select: none; transition: .12s; }
.data-table th.sortable:hover { background: var(--bg-soft); color: var(--text); }
.data-table th.sorted { color: var(--accent); }
.th-content { display: inline-flex; align-items: center; gap: 4px; }
.th-label { white-space: nowrap; }
.sort-indicator { font-size: 10px; opacity: .55; line-height: 1; }
.data-table th.sorted .sort-indicator { opacity: 1; }
</style>
