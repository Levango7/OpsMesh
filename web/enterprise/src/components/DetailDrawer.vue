<template>
  <transition name="drawer">
    <div v-if="open" class="drawer-overlay" @click.self="close">
      <aside
        ref="drawerRef"
        class="drawer"
        role="dialog"
        aria-modal="true"
        :aria-labelledby="titleId"
        :aria-describedby="bodyId"
        tabindex="-1"
      >
        <header class="drawer-head">
          <h3 :id="titleId">{{ title }}</h3>
          <button type="button" class="xs outline drawer-close" aria-label="Close" @click="emit('close')">✕</button>
        </header>
        <div :id="bodyId" class="drawer-body">
          <slot />
        </div>
      </aside>
    </div>
  </transition>
</template>

<script setup>
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { trapFocus } from '@/utils/a11y'
import { useScrollLock } from '@/composables/useScrollLock'

const props = defineProps({
  open: { type: Boolean, default: false },
  title: { type: String, default: '' }
})
const emit = defineEmits(['close'])
const drawerRef = ref(null)
const uid = Math.random().toString(36).slice(2, 9)
const titleId = `detail-drawer-title-${uid}`
const bodyId = `detail-drawer-body-${uid}`
let focusTrap = null
const scrollLocked = ref(false)
useScrollLock(scrollLocked)

function close() {
  emit('close')
}

watch(() => props.open, async (open) => {
  scrollLocked.value = open
  if (open) {
    await nextTick()
    focusTrap = trapFocus(drawerRef.value, close)
  } else {
    focusTrap?.destroy()
    focusTrap = null
  }
}, { immediate: true })

onBeforeUnmount(() => {
  focusTrap?.destroy()
  focusTrap = null
})
</script>

<style scoped>
.drawer-overlay {
  position: fixed; inset: 0; z-index: 39;
  background: rgba(15, 23, 42, .32);
}
.drawer {
  position: fixed; top: 0; right: 0;
  width: min(460px, 92vw); height: 100%;
  background: var(--surface); border-left: 1px solid var(--border);
  box-shadow: -8px 0 30px rgba(31,37,64,.14);
  padding: 22px; overflow: auto; z-index: 40;
}
.drawer-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
.drawer-head h3 { margin: 0; }
.drawer-close { min-width: 36px; min-height: 36px; }
.drawer-body :deep(h4) { margin-top: 16px; }
.drawer-body :deep(table) {
  border-collapse: separate; border-spacing: 0; width: 100%;
  font-size: 13px; margin-top: 6px; border-radius: var(--radius-sm); overflow: hidden;
}
.drawer-body :deep(th), .drawer-body :deep(td) {
  text-align: left; padding: 7px 10px; border-bottom: 1px solid var(--border);
}
.drawer-body :deep(th) { background: var(--surface-3); color: var(--text-2); font-weight: 600; font-size: 12px; }
.drawer-body :deep(tr:last-child td) { border-bottom: none; }

.drawer-enter-active, .drawer-leave-active { transition: transform .22s ease; }
.drawer-enter-from, .drawer-leave-to { transform: translateX(100%); }
</style>