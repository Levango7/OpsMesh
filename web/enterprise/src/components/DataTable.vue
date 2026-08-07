<template>
  <div class="dt-wrap">
    <table class="data-table">
      <thead>
        <tr>
          <th
            v-for="col in columns"
            :key="col.key"
            :style="col.width ? { width: col.width } : null"
          >
            {{ col.title }}
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="!rows || rows.length === 0">
          <td :colspan="columns.length" class="empty">{{ emptyText || $t('common.no_data') }}</td>
        </tr>
        <tr
          v-for="(row, idx) in rows"
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
defineProps({
  columns: { type: Array, required: true },     // [{ key, title, width?, slot?, format? }]
  rows: { type: Array, default: () => [] },
  rowKey: { type: String, default: '' },
  clickable: { type: Boolean, default: false },
  rowClass: { type: Function, default: null },
  emptyText: { type: String, default: '' }
})
defineEmits(['row-click'])

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
</style>