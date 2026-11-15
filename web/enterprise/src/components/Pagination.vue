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
const hasPrev = computed(() => props.page > 1)
const hasNext = computed(() =>
  props.total > 0 ? props.page * props.limit < props.total : props.pageSize >= props.limit
)
const info = computed(() => t('common.page', { n: props.page, m: props.pageSize }))
</script>

<style scoped>
.pager { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-top: 12px; }
.info { font-size: 12.5px; color: var(--text-3); }
.btns { display: flex; gap: 6px; }
</style>