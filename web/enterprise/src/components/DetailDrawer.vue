<template>
  <transition name="drawer">
    <aside v-if="open" class="drawer">
      <header class="drawer-head">
        <h3>{{ title }}</h3>
        <button class="xs outline" @click="$emit('close')">✕</button>
      </header>
      <div class="drawer-body">
        <slot />
      </div>
    </aside>
  </transition>
</template>

<script setup>
defineProps({
  open: { type: Boolean, default: false },
  title: { type: String, default: '' }
})
defineEmits(['close'])
</script>

<style scoped>
.drawer {
  position: fixed; top: 0; right: 0;
  width: 460px; max-width: 92vw; height: 100%;
  background: var(--surface); border-left: 1px solid var(--border);
  box-shadow: -8px 0 30px rgba(31,37,64,.14);
  padding: 22px; overflow: auto; z-index: 40;
}
.drawer-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
.drawer-head h3 { margin: 0; }
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