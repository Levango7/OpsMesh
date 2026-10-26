<template>
  <div class="table" :class="wrapClass">
    <div class="table__wrapper" ref="wrapperRef">
      <table class="table__table" :style="tableStyle" role="grid" :aria-label="ariaLabel">
        <colgroup>
          <col v-if="selectionEnabled" style="width: 48px" />
          <col
            v-for="col in visibleColumns"
            :key="col.id || col.prop"
            :style="{ width: col.width ? col.width + 'px' : undefined, minWidth: col.minWidth ? col.minWidth + 'px' : undefined }"
          />
          <col v-if="hasExpandColumn" style="width: 48px" />
        </colgroup>

        <thead class="table__thead">
          <tr class="table__header-row">
            <th
              v-if="selectionEnabled"
              class="table__th table__th--selection"
              scope="col"
            >
              <input
                type="checkbox"
                class="table__selection-all"
                :indeterminate="isIndeterminate"
                :checked="isAllSelected"
                @change="toggleAllSelection"
                @click.stop
                :aria-label="t('ui.table.selectAll', 'Select all')"
              />
            </th>

            <th
              v-for="(col, colIndex) in visibleColumns"
              :key="col.id || col.prop"
              :class="[
                'table__th',
                { 'table__th--sortable': col.sortable },
                { 'table__th--fixed': col.fixed },
                thAlignClass(col)
              ]"
              :style="col.fixed ? (col.fixed === 'left' ? { left: getFixedLeftOffset(colIndex) + 'px' } : { right: getFixedRightOffset(colIndex) + 'px' }) : {}"
              scope="col"
              :aria-sort="getAriaSort(col)"
            >
              <div class="table__th-inner">
                <span class="table__th-label">{{ col.label }}</span>
                <button
                  v-if="col.sortable"
                  type="button"
                  class="table__sort-btn"
                  @click.stop="handleSort(col)"
                  :aria-label="t('ui.table.sort', 'Sort by {{ col.label }}')"
                >
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                    <polyline points="18 15 12 9 6 15" />
                    <polyline points="6 9 12 15 18 9" />
                  </svg>
                </button>
                <span v-if="col.resizable" class="table__resize-handle" @mousedown="startResize(col, $event)" @touchstart.passive="startResize(col, $event)" />
              </div>
            </th>

            <th
              v-if="hasExpandColumn"
              class="table__th table__th--expand"
              scope="col"
            />
          </tr>
        </thead>

        <tbody class="table__tbody" ref="tbodyRef">
          <tr
            v-if="loading"
            class="table__loading-row"
          >
            <td :colspan="totalColspan" class="table__loading-cell">
              <div class="table__loading">
                <svg class="table__loading-spinner" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true">
                  <circle cx="12" cy="12" r="10" stroke-opacity="0.25" />
                  <path d="M12 2a10 10 0 0 1 10 10" stroke-opacity="0.75">
                    <animateTransform attributeName="transform" type="rotate" from="0 12 12" to="360 12 12" dur="1s" repeatCount="indefinite" />
                  </path>
                </svg>
                <span>{{ t('ui.table.loading', 'Loading') }}</span>
              </div>
            </td>
          </tr>

          <tr
            v-else-if="data.length === 0"
            class="table__empty-row"
          >
            <td :colspan="totalColspan" class="table__empty-cell">
              <div class="table__empty">
                <svg class="table__empty-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                  <rect x="3" y="3" width="18" height="18" rx="2" ry="2" />
                  <line x1="3" y1="9" x2="21" y2="9" />
                  <line x1="9" y1="21" x2="9" y2="9" />
                </svg>
                <span>{{ emptyText }}</span>
              </div>
            </td>
          </tr>

          <template v-else>
            <tr
              v-for="(row, rowIndex) in data"
              :key="getRowKey(row)"
              class="table__row"
              :class="[
                'table__row--' + size,
                { 'table__row--striped': stripe && rowIndex % 2 === 1 },
                { 'table__row--hover': hover },
                { 'table__row--current': highlightCurrentRow && currentRowKey === getRowKey(row) },
                { 'table__row--selected': isRowSelected(row) },
                { 'table__row--expanded': expandedRows.has(getRowKey(row)) }
              ]"
              @click="handleRowClick(row, $event)"
              @dblclick="handleRowDblClick(row, $event)"
              @keydown.enter="handleRowEnter(row, $event)"
              @keydown.space.prevent="handleRowSpace(row, $event)"
              tabindex="0"
              :aria-rowindex="rowIndex + 1"
              :aria-selected="isRowSelected(row) ? 'true' : 'false'"
            >
              <td
                v-if="selectionEnabled"
                class="table__td table__td--selection"
                :aria-label="t('ui.table.selectRow', 'Select row')"
              >
                <input
                  type="checkbox"
                  class="table__selection"
                  :checked="isRowSelected(row)"
                  @change="toggleRowSelection(row)"
                  @click.stop
                  :aria-label="t('ui.table.selectRow', 'Select row')"
                />
              </td>

              <td
                v-for="(col, colIndex) in visibleColumns"
                :key="col.id || col.prop"
                :class="[
                  'table__td',
                  { 'table__td--fixed': col.fixed },
                  tdAlignClass(col),
                  { 'table__td--ellipsis': col.ellipsis },
                  { 'table__td--tabular': col.tabular }
                ]"
                :style="col.fixed ? (col.fixed === 'left' ? { left: getFixedLeftOffset(colIndex) + 'px' } : { right: getFixedRightOffset(colIndex) + 'px' }) : {}"
                :aria-colindex="selectionEnabled ? colIndex + 2 : colIndex + 1"
              >
                <div class="table__cell">
                  <slot
                    :name="'cell-' + col.prop"
                    :row="row"
                    :column="col"
                    :row-index="rowIndex"
                    :value="row[col.prop]"
                  >
                    <span v-if="col.formatter">{{ col.formatter(row[col.prop], row, col) }}</span>
                    <span v-else>{{ row[col.prop] ?? '' }}</span>
                  </slot>
                </div>
              </td>

              <td
                v-if="hasExpandColumn"
                class="table__td table__td--expand"
                @click.stop="toggleExpand(row)"
              >
                <button
                  type="button"
                  class="table__expand-btn"
                  :aria-expanded="expandedRows.has(getRowKey(row)) ? 'true' : 'false'"
                  :aria-label="expandedRows.has(getRowKey(row)) ? t('ui.table.collapse', 'Collapse') : t('ui.table.expand', 'Expand')"
                >
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                    <polyline :points="expandedRows.has(getRowKey(row)) ? '18 15 12 9 6 15' : '6 9 12 15 18 9'" />
                  </svg>
                </button>
              </td>
            </tr>

            <template v-for="(row, rowIndex) in data" :key="'expand-' + getRowKey(row)">
              <tr
                v-if="expandedRows.has(getRowKey(row))"
                class="table__expand-row"
                :aria-hidden="true"
              >
                <td :colspan="totalColspan" class="table__expand-cell">
                  <div class="table__expand-content">
                    <slot name="expand" :row="row" :row-index="rowIndex" />
                  </div>
                </td>
              </tr>
            </template>
          </template>
        </tbody>
      </table>
    </div>

    <div v-if="data.length && !loading" class="table__footer">
      <div class="table__footer-left">
        <span class="table__summary">
          {{ t('ui.table.total', 'Total') }}: <strong>{{ data.length }}</strong>
          <span v-if="selectionEnabled">, {{ t('ui.table.selected', 'Selected') }}: <strong>{{ selectedRowKeys.length }}</strong></span>
        </span>
      </div>
      <div class="table__footer-right">
        <slot name="footer" />
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, ref, watch, onMounted, onUnmounted, useSlots } from 'vue'
import { t } from '@/i18n'

const slots = useSlots()

const props = defineProps({
  data: { type: Array, default: () => [] },
  columns: { type: Array, default: () => [] },
  rowKey: { type: [String, Function], default: 'id' },
  stripe: { type: Boolean, default: false },
  border: { type: Boolean, default: true },
  hover: { type: Boolean, default: true },
  loading: { type: Boolean, default: false },
  emptyText: { type: String, default: '' },
  maxHeight: { type: [String, Number], default: '' },
  size: { type: String, default: 'default' },
  highlightCurrentRow: { type: Boolean, default: false },
  selectable: { type: Boolean, default: false },
  selection: { type: Array, default: () => [] },
  fixedLeft: { type: Number, default: 0 },
  fixedRight: { type: Number, default: 0 }
})

const emit = defineEmits([
  'sort-change',
  'selection-change',
  'row-click',
  'row-dblclick',
  'cell-click',
  'header-click',
  'expand-change',
  'update:selection'
])

const wrapperRef = ref(null)
const tbodyRef = ref(null)
const sortState = ref({ prop: '', order: '' })
const expandedRows = ref(new Set())
const currentRowKey = ref(null)
const selectedRowKeys = ref([])
const columnWidths = ref(new Map())
const isResizing = ref(false)
const resizingColumn = ref(null)
const resizeStartX = ref(0)
const resizeStartWidth = ref(0)

const getRowKey = (row) => {
  if (typeof props.rowKey === 'function') return props.rowKey(row)
  return row[props.rowKey]
}

const visibleColumns = computed(() => {
  return props.columns.filter(col => !col.hidden)
})

const hasExpandColumn = computed(() => {
  return !!props.columns.find(col => col.type === 'expand') || !!slots.expand
})

const selectionEnabled = computed(() => {
  return props.selectable && !!props.rowKey
})

const totalColspan = computed(() => {
  return visibleColumns.value.length + (selectionEnabled.value ? 1 : 0) + (hasExpandColumn.value ? 1 : 0)
})

const thAlignClass = (col) => col.align ? { [`table__th--align-${col.align}`]: true } : {}
const tdAlignClass = (col) => col.align ? { [`table__td--align-${col.align}`]: true } : {}

const isAllSelected = computed(() => {
  if (!selectionEnabled.value || props.data.length === 0) return false
  return props.data.every(row => selectedRowKeys.value.includes(getRowKey(row)))
})

const isIndeterminate = computed(() => {
  if (!selectionEnabled.value) return false
  const selected = props.data.filter(row => selectedRowKeys.value.includes(getRowKey(row)))
  return selected.length > 0 && selected.length < props.data.length
})

const isRowSelected = (row) => {
  return selectedRowKeys.value.includes(getRowKey(row))
}

const wrapClass = computed(() => [
  'table--' + props.size,
  { 'table--border': props.border, 'table--stripe': props.stripe, 'table--hover': props.hover }
])

const tableStyle = computed(() => {
  const styles = {}
  if (props.maxHeight) styles.maxHeight = isNaN(props.maxHeight) ? props.maxHeight : props.maxHeight + 'px'
  return styles
})

const ariaLabel = computed(() => {
  return t('ui.table.ariaLabel', 'Data table')
})

function getAriaSort(col) {
  if (sortState.value.prop === col.prop) {
    return sortState.value.order === 'ascending' ? 'ascending' : 'descending'
  }
  return 'none'
}

function getFixedLeftOffset(colIndex) {
  let offset = 0
  if (selectionEnabled.value) offset += 48
  for (let i = 0; i < colIndex; i++) {
    const c = visibleColumns.value[i]
    if (c.fixed === 'left') offset += c.width || 120
  }
  return offset
}

function getFixedRightOffset(colIndex) {
  let offset = 0
  for (let i = visibleColumns.value.length - 1; i > colIndex; i--) {
    const c = visibleColumns.value[i]
    if (c.fixed === 'right') offset += c.width || 120
  }
  return offset
}

function handleSort(col) {
  if (!col.sortable) return
  const current = sortState.value
  let order = 'ascending'
  if (current.prop === col.prop && current.order === 'ascending') {
    order = 'descending'
  } else if (current.prop === col.prop && current.order === 'descending') {
    order = ''
  }
  sortState.value = order ? { prop: col.prop, order } : { prop: '', order: '' }
  emit('sort-change', { column: col, prop: col.prop, order: sortState.value.order })
}

function handleRowClick(row, evt) {
  if (props.highlightCurrentRow) currentRowKey.value = getRowKey(row)
  emit('row-click', row, evt)
}

function handleRowDblClick(row, evt) {
  emit('row-dblclick', row, evt)
}

function handleRowEnter(row, evt) {
  if (evt.target.tagName !== 'INPUT' && evt.target.tagName !== 'BUTTON' && evt.target.tagName !== 'A') {
    toggleRowSelection(row)
  }
  emit('row-click', row, evt)
}

function handleRowSpace(row, evt) {
  if (evt.target.tagName !== 'INPUT' && evt.target.tagName !== 'BUTTON' && evt.target.tagName !== 'A') {
    toggleRowSelection(row)
  }
}

function toggleAllSelection() {
  if (isAllSelected.value) {
    selectedRowKeys.value = []
  } else {
    selectedRowKeys.value = props.data.map(getRowKey)
  }
  emit('update:selection', [...selectedRowKeys.value])
  emit('selection-change', [...selectedRowKeys.value])
}

function toggleRowSelection(row) {
  const key = getRowKey(row)
  const idx = selectedRowKeys.value.indexOf(key)
  if (idx >= 0) selectedRowKeys.value.splice(idx, 1)
  else selectedRowKeys.value.push(key)
  emit('update:selection', [...selectedRowKeys.value])
  emit('selection-change', [...selectedRowKeys.value])
}

function toggleExpand(row) {
  const key = getRowKey(row)
  if (expandedRows.value.has(key)) {
    expandedRows.value.delete(key)
  } else {
    expandedRows.value.add(key)
  }
  emit('expand-change', Array.from(expandedRows.value))
}

function startResize(col, evt) {
  if (!col.resizable) return
  isResizing.value = true
  resizingColumn.value = col
  resizeStartX.value = evt.clientX || (evt.touches && evt.touches[0].clientX)
  resizeStartWidth.value = col.width || 120
  document.addEventListener('mousemove', onResizeMove)
  document.addEventListener('mouseup', onResizeEnd)
  document.addEventListener('touchmove', onResizeMove, { passive: true })
  document.addEventListener('touchend', onResizeEnd)
  evt.preventDefault()
}

function onResizeMove(evt) {
  if (!isResizing.value || !resizingColumn.value) return
  const clientX = evt.clientX || (evt.touches && evt.touches[0].clientX)
  const delta = clientX - resizeStartX.value
  const newWidth = Math.max(60, resizeStartWidth.value + delta)
  columnWidths.value.set(resizingColumn.value.prop, newWidth)
}

function onResizeEnd() {
  if (resizingColumn.value) {
    columnWidths.value.set(resizingColumn.value.prop, columnWidths.value.get(resizingColumn.value.prop) || 120)
  }
  isResizing.value = false
  resizingColumn.value = null
  document.removeEventListener('mousemove', onResizeMove)
  document.removeEventListener('mouseup', onResizeEnd)
  document.removeEventListener('touchmove', onResizeMove)
  document.removeEventListener('touchend', onResizeEnd)
}

watch(() => props.selection, (val) => {
  selectedRowKeys.value = val || []
})

watch(() => props.data, () => {
  if (!selectionEnabled.value) return
  selectedRowKeys.value = selectedRowKeys.value.filter(key =>
    props.data.some(row => getRowKey(row) === key)
  )
  emit('update:selection', [...selectedRowKeys.value])
})

onMounted(() => {
  if (props.maxHeight && tbodyRef.value) {
    const updateHeight = () => {
      const wrapper = wrapperRef.value
      if (wrapper) wrapper.style.maxHeight = props.maxHeight
    }
    updateHeight()
    window.addEventListener('resize', updateHeight)
    windowResizeHandler = updateHeight
  }
})

let windowResizeHandler = null
onUnmounted(() => {
  if (windowResizeHandler) window.removeEventListener('resize', windowResizeHandler)
  document.removeEventListener('mousemove', onResizeMove)
  document.removeEventListener('mouseup', onResizeEnd)
  document.removeEventListener('touchmove', onResizeMove)
  document.removeEventListener('touchend', onResizeEnd)
})
</script>

<style scoped>
.table {
  display: flex;
  flex-direction: column;
  width: 100%;
  font-family: var(--font-family-sans, "Geist", "Inter", sans-serif);
  background: var(--color-surface-1, #0f1011);
  border: 1px solid var(--color-hairline, #23252a);
  border-radius: var(--radius-md, 8px);
  overflow: hidden;
  color: var(--color-ink, #f7f8f8);
}

.table__wrapper {
  overflow: auto;
  position: relative;
  flex: 1;
}

.table__table {
  width: 100%;
  border-collapse: collapse;
  table-layout: fixed;
  min-width: 100%;
}

.table__thead {
  position: sticky;
  top: 0;
  z-index: 10;
  background: var(--color-surface-1, #0f1011);
}

.table__header-row {
  border-bottom: 1px solid var(--color-hairline, #23252a);
}

.table__th {
  padding: 10px 12px;
  height: 36px;
  font-size: 12px;
  font-weight: 600;
  color: var(--color-ink, #f7f8f8);
  text-align: left;
  white-space: nowrap;
  vertical-align: middle;
  position: relative;
  background: var(--color-surface-1, #0f1011);
}

.table__th-inner {
  display: flex;
  align-items: center;
  gap: 4px;
  height: 100%;
}

.table__th--sortable .table__th-inner {
  cursor: pointer;
  user-select: none;
}

.table__th--sortable:hover .table__sort-btn {
  opacity: 1;
}

.table__sort-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 16px;
  border: none;
  background: transparent;
  color: var(--color-ink-tertiary, #62666d);
  cursor: pointer;
  opacity: 0.5;
  transition: opacity 150ms ease, color 150ms ease;
  padding: 0;
  line-height: 1;
}

.table__sort-btn:hover,
.table__sort-btn:focus-visible {
  opacity: 1;
  color: var(--color-primary, #5e6ad2);
  outline: none;
}

.table__sort-btn:focus-visible {
  border-radius: var(--radius-xs, 4px);
}

.table__th[aria-sort="ascending"] .table__sort-btn svg:first-child,
.table__th[aria-sort="descending"] .table__sort-btn svg:last-child {
  opacity: 1;
  color: var(--color-primary, #5e6ad2);
}

.table__resize-handle {
  position: absolute;
  right: 0;
  top: 0;
  bottom: 0;
  width: 8px;
  cursor: col-resize;
  opacity: 0;
  transition: opacity 150ms ease;
}

.table__th:hover .table__resize-handle,
.table__resize-handle:hover {
  opacity: 1;
}

.table__resize-handle::after {
  content: '';
  position: absolute;
  right: 3px;
  top: 50%;
  transform: translateY(-50%);
  width: 1px;
  height: 16px;
  background: var(--color-hairline, #23252a);
}

.table__th--fixed {
  z-index: 20;
  box-shadow: inset -1px 0 0 var(--color-hairline, #23252a);
}

.table__th--align-center { text-align: center; }
.table__th--align-right { text-align: right; }

.table__th--selection,
.table__th--expand {
  width: 48px;
  padding: 0;
  text-align: center;
}

.table__selection-all {
  width: 16px;
  height: 16px;
  accent-color: var(--color-primary, #5e6ad2);
  cursor: pointer;
}

.table__tbody {
  background: var(--color-canvas, #010102);
}

.table__row {
  border-bottom: 1px solid var(--color-hairline, #23252a);
  transition: background-color 150ms ease;
  background: var(--color-canvas, #010102);
}

.table__row:last-child {
  border-bottom: none;
}

.table__row--striped {
  background: var(--color-surface-1, #0f1011);
}

.table__row--hover:hover {
  background: var(--color-surface-2, #141516);
}

.table__row--current {
  background: color-mix(in srgb, var(--color-primary, #5e6ad2) 12%, transparent) !important;
}

.table__row--selected {
  background: color-mix(in srgb, var(--color-primary, #5e6ad2) 12%, transparent) !important;
}

.table__row--selected:hover {
  background: color-mix(in srgb, var(--color-primary, #5e6ad2) 18%, transparent) !important;
}

.table__row:focus-visible {
  outline: none;
  box-shadow: inset 0 0 0 2px var(--color-primary, #5e6ad2);
}

.table__td {
  padding: 10px 12px;
  font-size: 13px;
  color: var(--color-ink, #f7f8f8);
  vertical-align: middle;
  white-space: nowrap;
  font-family: var(--font-family-sans, "Geist", "Inter", sans-serif);
  font-variant-numeric: tabular-nums;
}

.table__td--align-center { text-align: center; }
.table__td--align-right { text-align: right; }

.table__td--tabular {
  font-family: var(--font-mono, var(--font-family-mono));
  font-variant-numeric: tabular-nums;
}

.table__td--ellipsis {
  overflow: hidden;
  text-overflow: ellipsis;
}

.table__td--fixed {
  z-index: 15;
  background: var(--color-canvas, #010102);
}

.table__cell {
  display: flex;
  align-items: center;
  height: 100%;
  min-height: 36px;
}

.table__td--selection,
.table__td--expand {
  width: 48px;
  padding: 0;
  text-align: center;
}

.table__selection {
  width: 16px;
  height: 16px;
  accent-color: var(--color-primary, #5e6ad2);
  cursor: pointer;
}

.table__expand-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border: none;
  border-radius: 50%;
  background: transparent;
  color: var(--color-ink-tertiary, #62666d);
  cursor: pointer;
  transition: background-color 150ms ease, color 150ms ease;
  padding: 0;
  line-height: 1;
}

.table__expand-btn:hover,
.table__expand-btn:focus-visible {
  background: var(--color-surface-2, #141516);
  color: var(--color-primary, #5e6ad2);
  outline: none;
}

.table__expand-row {
  background: var(--color-surface-1, #0f1011);
}

.table__expand-cell {
  padding: 0 !important;
  border-top: 1px solid var(--color-hairline, #23252a);
}

.table__expand-content {
  padding: 16px;
  animation: table-expand 0.2s ease;
}

@keyframes table-expand {
  from { opacity: 0; transform: translateY(-8px); }
  to { opacity: 1; transform: translateY(0); }
}

.table__loading-row {
  height: 120px;
}

.table__loading-cell {
  padding: 0 !important;
}

.table__loading {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  min-height: 120px;
  gap: 12px;
  color: var(--color-ink-tertiary, #62666d);
  font-size: 13px;
}

.table__loading-spinner {
  width: 32px;
  height: 32px;
}

.table__empty-row {
  height: 120px;
}

.table__empty-cell {
  padding: 0 !important;
}

.table__empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  min-height: 120px;
  gap: 12px;
  color: var(--color-ink-tertiary, #62666d);
  font-size: 13px;
}

.table__empty-icon {
  width: 48px;
  height: 48px;
  opacity: 0.5;
}

.table__footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-top: 1px solid var(--color-hairline, #23252a);
  background: var(--color-surface-1, #0f1011);
  font-size: 13px;
  color: var(--color-ink-muted, #d0d6e0);
}

.table__summary {
  display: flex;
  align-items: center;
  gap: 8px;
}

.table__summary strong {
  color: var(--color-ink, #f7f8f8);
}

/* Sizes */
.table--small .table__th,
.table--sm .table__th { height: 28px; font-size: 11px; }
.table--small .table__td,
.table--sm .table__td { padding: 4px 8px; font-size: 12px; }
.table--small .table__cell,
.table--sm .table__cell { min-height: 28px; }
.table--small .table__expand-btn,
.table--sm .table__expand-btn { width: 20px; height: 20px; }

.table--default .table__th,
.table--md .table__th { height: 36px; font-size: 12px; }
.table--default .table__td,
.table--md .table__td { padding: 10px 12px; font-size: 13px; }
.table--default .table__cell,
.table--md .table__cell { min-height: 36px; }

.table--large .table__th,
.table--lg .table__th { height: 44px; font-size: 14px; }
.table--large .table__td,
.table--lg .table__td { padding: 12px 16px; font-size: 14px; }
.table--large .table__cell,
.table--lg .table__cell { min-height: 44px; }

/* Border */
.table--border .table__th,
.table--border .table__td {
  border-right: 1px solid var(--color-hairline, #23252a);
}

.table--border .table__th:last-child,
.table--border .table__td:last-child {
  border-right: none;
}

/* Scrollbar */
.table__wrapper::-webkit-scrollbar { height: 8px; width: 8px; }
.table__wrapper::-webkit-scrollbar-track { background: transparent; }
.table__wrapper::-webkit-scrollbar-thumb { background: var(--color-hairline, #23252a); border-radius: 4px; }
.table__wrapper::-webkit-scrollbar-thumb:hover { background: var(--color-surface-2, #141516); }

/* Reduced motion */
@media (prefers-reduced-motion: reduce) {
  .table__row,
  .table__sort-btn,
  .table__expand-btn,
  .table__resize-handle,
  .table__expand-content {
    transition: none !important;
    animation: none !important;
  }
}

/* Light mode */
[data-theme="light"] .table {
  background: var(--color-inverse-surface-1, #f5f6f6);
  border-color: var(--color-hairline-inverse, #e3e8ee);
  color: var(--color-inverse-ink, #000000);
}
[data-theme="light"] .table__thead,
[data-theme="light"] .table__th {
  background: var(--color-inverse-surface-1, #f5f6f6);
  color: var(--color-inverse-ink, #000000);
}
[data-theme="light"] .table__tbody {
  background: #ffffff;
}
[data-theme="light"] .table__row {
  background: #ffffff;
  border-bottom-color: var(--color-hairline-inverse, #e3e8ee);
}
[data-theme="light"] .table__row--striped {
  background: var(--color-inverse-surface-1, #f5f6f6);
}
[data-theme="light"] .table__row--hover:hover {
  background: var(--color-inverse-surface-2, #f6f7f7);
}
[data-theme="light"] .table__td {
  color: var(--color-inverse-ink, #000000);
}
[data-theme="light"] .table__td--fixed {
  background: #ffffff;
}
[data-theme="light"] .table__expand-row {
  background: var(--color-inverse-surface-1, #f5f6f6);
}
[data-theme="light"] .table__expand-cell {
  border-top-color: var(--color-hairline-inverse, #e3e8ee);
}
[data-theme="light"] .table__footer {
  background: var(--color-inverse-surface-1, #f5f6f6);
  border-top-color: var(--color-hairline-inverse, #e3e8ee);
  color: var(--color-inverse-ink-muted, #64748d);
}
[data-theme="light"] .table__summary strong {
  color: var(--color-inverse-ink, #000000);
}
[data-theme="light"] .table__loading,
[data-theme="light"] .table__empty {
  color: var(--color-inverse-ink-muted, #64748d);
}
[data-theme="light"] .table__sort-btn {
  color: var(--color-inverse-ink-muted, #64748d);
}
[data-theme="light"] .table__expand-btn {
  color: var(--color-inverse-ink-muted, #64748d);
}
[data-theme="light"] .table__resize-handle::after {
  background: var(--color-hairline-inverse, #e3e8ee);
}
[data-theme="light"] .table__wrapper::-webkit-scrollbar-thumb {
  background: var(--color-hairline-inverse, #e3e8ee);
}
[data-theme="light"] .table__wrapper::-webkit-scrollbar-thumb:hover {
  background: var(--color-inverse-surface-2, #f6f7f7);
}
</style>
