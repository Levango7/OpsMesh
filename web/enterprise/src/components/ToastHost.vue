<template>
  <!-- 全局 Toast 宿主 — 顶部右侧，随 token 主题自动适配 -->
  <div class="toast-host" aria-live="polite" data-testid="toast-host">
    <transition-group name="toast">
      <div v-for="t in toasts" :key="t.id" class="toast" :class="t.type" role="alert" :data-testid="'toast-' + t.type">
        <span class="toast-dot" aria-hidden="true" />
        <span class="toast-msg">{{ t.title }}</span>
        <button class="toast-close" type="button" :aria-label="$t('common.close')" @click="dismiss(t.id)">×</button>
      </div>
    </transition-group>
  </div>
</template>

<script setup>
// 全局 Toast 宿主 — 从 utils/toast 的 reactive 队列渲染，无业务逻辑
import { toasts, dismiss } from '@/utils/toast'
</script>

<style scoped>
.toast-host {
  position: fixed;
  top: 16px;
  right: 16px;
  z-index: 2000;
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: 320px;
  max-width: calc(100vw - 32px);
  pointer-events: none;
}
.toast {
  pointer-events: auto;
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 10px 12px;
  border-radius: var(--radius-sm);
  background: var(--surface);
  border: 1px solid var(--border);
  border-left: 3px solid var(--info);
  box-shadow: var(--shadow);
  font-size: var(--fs-sm);
  color: var(--text);
}
.toast.error { border-left-color: var(--fail); background: var(--fail-soft); }
.toast.warn { border-left-color: var(--warn); background: var(--warn-soft); }
.toast.success { border-left-color: var(--ok); background: var(--ok-soft); }
.toast-dot {
  flex: 0 0 8px;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  margin-top: 5px;
  background: var(--info);
}
.toast.error .toast-dot { background: var(--fail); }
.toast.warn .toast-dot { background: var(--warn); }
.toast.success .toast-dot { background: var(--ok); }
.toast-msg { flex: 1; min-width: 0; word-break: break-word; line-height: 1.5; }
.toast-close {
  flex: 0 0 auto;
  border: none;
  background: transparent;
  color: var(--text-3);
  padding: 0 2px;
  font-size: 15px;
  line-height: 1;
  border-radius: 4px;
}
.toast-close:hover { color: var(--text); background: var(--surface-3); }

.toast-enter-active, .toast-leave-active { transition: opacity .2s ease, transform .2s ease; }
.toast-enter-from, .toast-leave-to { opacity: 0; transform: translateX(12px); }
</style>
