<template>
  <span class="badge" :class="cls"><slot>{{ text }}</slot></span>
</template>

<script setup>
import { computed } from 'vue'
const props = defineProps({
  // 状态值：ok/success/done | fail/failed/error | warn/warning/running | info/pending/created | silenced/acknowledged
  status: { type: String, default: '' },
  text: { type: String, default: '' }
})
const MAP = {
  ok: 'ok', success: 'ok', done: 'ok', managed: 'ok', acknowledged: 'ok',
  fail: 'fail', failed: 'fail', error: 'fail', critical: 'fail',
  warn: 'warn', warning: 'warn', running: 'warn', rolledback: 'warn',
  info: 'info', pending: 'info', created: 'info', draft: 'info', discovered: 'info',
  silenced: 'info'
}
const cls = computed(() => MAP[props.status] || 'info')
</script>

<style scoped>
.badge {
  display: inline-flex; align-items: center; height: 20px;
  padding: 0 9px; border-radius: 999px;
  font-size: 11.5px; font-weight: 600; margin-left: 6px;
}
.badge.ok { background: var(--ok-bg); color: var(--ok); }
.badge.fail { background: var(--fail-bg); color: var(--fail); }
.badge.warn { background: var(--warn-bg); color: var(--warn); }
.badge.info { background: var(--info-bg); color: var(--info); }
</style>