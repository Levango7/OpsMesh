<template>
  <div class="pager">
    <span class="info">{{ info }}</span>
    <div class="btns">
      <button class="xs outline" :disabled="!hasPrev" @click="$emit('prev')">{{ $t('common.prev') }}</button>
      <button class="xs outline" :disabled="!hasNext" @click="$emit('next')">{{ $t('common.next') }}</button>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { t } from '@/i18n'
const props = defineProps({
  page: { type: Number, default: 1 },
  pageSize: { type: Number, default: 0 },
  limit: { type: Number, default: 200 },
  // 可选：服务端返回的总条数。提供时按总数判定是否还有下一页，
  // 避免「末页恰好满页」时多翻出一页空页；未提供时保持旧的满页判定。
  total: { type: Number, default: 0 }
})
defineEmits(['prev', 'next'])
// props 来自服务端数据，做一次防御性数字收敛（字符串/负数/NaN 不抛错）。
function toNumber(value, fallback) {
  const n = Number(value)
  return Number.isFinite(n) ? n : fallback
}
const normPage = computed(() => Math.max(1, Math.floor(toNumber(props.page, 1))))
const normPageSize = computed(() => Math.max(0, Math.floor(toNumber(props.pageSize, 0))))
const normLimit = computed(() => Math.max(0, Math.floor(toNumber(props.limit, 0))))
const normTotal = computed(() => Math.max(0, Math.floor(toNumber(props.total, 0))))
const hasPrev = computed(() => normPage.value > 1)
const hasNext = computed(() => {
  // total > 0 时按总数判定末页（page * limit == total 即无下一页）；
  // total 缺失/为负/为零时回退旧的满页启发式。
  if (normTotal.value > 0 && normLimit.value > 0) {
    return normPage.value * normLimit.value < normTotal.value
  }
  return normLimit.value > 0 && normPageSize.value >= normLimit.value
})
const info = computed(() => t('common.page', { n: normPage.value, m: normPageSize.value }))
</script>

<style scoped>
.pager { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-top: 12px; }
.info { font-size: 12.5px; color: var(--text-3); }
.btns { display: flex; gap: 6px; }
</style>