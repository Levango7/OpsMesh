<template>
  <div
    v-if="isActive && !disabled"
    class="tabs__panel"
    :class="{ 'tabs__panel--card': tabsType === 'card' }"
    :aria-hidden="!isActive"
  >
    <slot />
  </div>
</template>

<script setup>
import { inject, computed } from "vue"
import { TabsSymbol } from "./tabs-symbol"

const props = defineProps({
  name: { type: String, required: true },
  disabled: { type: Boolean, default: false }
})

const tabs = inject(TabsSymbol)

const isActive = computed(() => tabs.active.value === props.name)
const tabsType = computed(() => tabs.type)
</script>

<style scoped>
.tabs__panel {
  width: 100%;
  overflow: auto;
  outline: none;
}

.tabs__panel--card {
  border: 1px solid var(--color-hairline, #23252a);
  border-radius: var(--radius-lg, 12px);
  background: var(--color-surface-1, #0f1011);
  padding: var(--space-6, 24px);
  margin-top: var(--space-4, 16px);
}
</style>