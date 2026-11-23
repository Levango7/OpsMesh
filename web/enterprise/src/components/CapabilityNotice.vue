<template>
  <div class="capability-notice" role="status" :data-testid="testid || undefined">
    <Icon name="warning" :size="20" />
    <div>
      <strong>{{ $t('common.capability_unavailable_title') }}</strong>
      <p>{{ $t('common.capability_unavailable_desc') }}</p>
      <code>{{ message }}</code>
    </div>
  </div>
</template>

<script setup>
import Icon from '@/components/Icon.vue'

defineProps({
  message: { type: String, required: true },
  testid: { type: String, default: '' }
})
</script>

<style scoped>
.capability-notice {
  display: flex; gap: 12px; align-items: flex-start;
  padding: 18px; margin-bottom: 16px;
  border: 1px solid var(--border); border-radius: var(--radius-md);
  background: var(--surface-2); color: var(--text-2);
}
.capability-notice strong { color: var(--text); }
.capability-notice p { margin: 6px 0 10px; }
.capability-notice code { white-space: pre-wrap; word-break: break-word; }
</style>
