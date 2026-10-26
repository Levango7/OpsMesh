<template>
  <div class="notification" :class="wrapClasses" :style="notificationStyle">
    <div class="notification__content">
      <div class="notification__icon" :class="`notification__icon--${type}`">
        <slot name="icon">
          <svg v-if="type === 'success'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <polyline points="20 6 9 17 4 12" />
          </svg>
          <svg v-else-if="type === 'error'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <circle cx="12" cy="12" r="10" />
            <line x1="15" y1="9" x2="9" y2="15" />
            <line x1="9" y1="9" x2="15" y2="15" />
          </svg>
          <svg v-else-if="type === 'warning'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <circle cx="12" cy="12" r="10" />
            <line x1="12" y1="8" x2="12" y2="12" />
            <line x1="12" y1="16" x2="12.01" y2="16" />
          </svg>
          <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <circle cx="12" cy="12" r="10" />
            <line x1="12" y1="16" x2="12" y2="8" />
          </svg>
        </slot>
      </div>

      <div class="notification__message">
        <h3 v-if="title" class="notification__title">{{ title }}</h3>
        <span class="notification__description">{{ message }}</span>
      </div>

      <div class="notification__actions">
        <slot name="actions" />
      </div>

      <button
        v-if="props.closable"
        type="button"
        class="notification__close"
        @click="handleClose"
        :aria-label="t('ui.notification.close', 'Close')"
      >
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <line x1="18" y1="6" x2="6" y2="18" />
          <line x1="6" y1="6" x2="18" y2="18" />
        </svg>
      </button>

      <div v-if="duration > 0" class="notification__progress" :style="{ width: progressWidth + '%' }"></div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from "vue"
import { t } from "@/i18n"

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  type: { type: String, default: "info", validator: v => ["info", "success", "warning", "error"].includes(v) },
  title: { type: String, default: "" },
  message: { type: String, default: "" },
  duration: { type: Number, default: 4500 },
  closable: { type: Boolean, default: true },
  position: { type: String, default: "top-right", validator: v => ["top-right", "top-left", "bottom-right", "bottom-left", "top-center", "bottom-center"].includes(v) },
  offset: { type: [String, Number], default: 24 }
})

const emit = defineEmits(["update:modelValue", "close", "click"])

const visible = ref(props.modelValue)
let timer = null

const wrapClasses = computed(() => [
  "notification",
  `notification--${props.type}`,
  `notification--${props.position}`
])

const progressWidth = computed(() => {
  if (props.duration <= 0) return 100
  const elapsed = Date.now() - (startTime.value || 0)
  return Math.max(0, 100 - (elapsed / props.duration) * 100)
})

const startTime = ref(Date.now())

const notificationStyle = computed(() => {
  const style = {}
  switch (props.position) {
    case "top-right":
      style.top = `${props.offset}px`
      style.right = `${props.offset}px`
      break
    case "top-left":
      style.top = `${props.offset}px`
      style.left = `${props.offset}px`
      break
    case "bottom-right":
      style.bottom = `${props.offset}px`
      style.right = `${props.offset}px`
      break
    case "bottom-left":
      style.bottom = `${props.offset}px`
      style.left = `${props.offset}px`
      break
    case "top-center":
      style.top = `${props.offset}px`
      style.left = "50%"
      style.transform = "translateX(-50%)"
      break
    case "bottom-center":
      style.bottom = `${props.offset}px`
      style.left = "50%"
      style.transform = "translateX(-50%)"
      break
  }
  return style
})

function handleClose() {
  visible.value = false
  emit("update:modelValue", false)
  emit("close")
  if (timer) clearTimeout(timer)
}

function startTimer() {
  if (props.duration <= 0) return
  if (timer) clearTimeout(timer)
  startTime.value = Date.now()
  timer = setTimeout(() => {
    handleClose()
  }, props.duration)
}

function resetTimer() {
  if (props.duration <= 0) return
  if (timer) clearTimeout(timer)
  startTime.value = Date.now()
  timer = setTimeout(() => {
    handleClose()
  }, props.duration)
}

onMounted(() => {
  startTimer()
})

onUnmounted(() => {
  if (timer) clearTimeout(timer)
})

// Expose methods
const api = {
  close: handleClose,
  resetTimer
}

import { defineExpose } from "vue"
defineExpose(api)
</script>

<style scoped>
.notification {
  display: flex;
  flex-direction: column;
  gap: var(--space-2, 8px);
  width: 100%;
  max-width: 380px;
  padding: var(--space-3, 12px) var(--space-4, 16px);
  border-radius: var(--radius-md, 8px);
  background: var(--color-surface-1, #0f1011);
  border: 1px solid var(--color-hairline, #23252a);
  box-shadow: var(--shadow-xl, 0 16px 48px rgba(0, 0, 0, 0.5));
  color: var(--color-ink, #f7f8f8);
  font-size: var(--font-size-sm, 12px);
  line-height: 1.5;
  animation: notification-fade-in 200ms ease;
  z-index: 1200;
}

.notification__content {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3, 12px);
  width: 100%;
}

.notification__icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  flex-shrink: 0;
  font-size: 16px;
}

.notification__icon--success { color: var(--color-success, #27a644); }
.notification__icon--error { color: var(--color-error, #ef4444); }
.notification__icon--warning { color: var(--color-warning, #f5a623); }
.notification__icon--info { color: var(--color-link, #0070f3); }

.notification__message {
  flex: 1;
  min-width: 0;
}

.notification__title {
  margin: 0;
  font-size: var(--font-size-sm, 12px);
  font-weight: 600;
  color: var(--color-ink, #f7f8f8);
  line-height: 1.25;
}

.notification__description {
  font-size: var(--font-size-xs, 11px);
  color: var(--color-ink-muted, #d0d6e0);
  line-height: 1.4;
  margin-top: 2px;
}

.notification__actions {
  display: flex;
  align-items: center;
  gap: var(--space-1, 4px);
  margin-top: var(--space-2, 8px);
}

.notification__close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  border: none;
  border-radius: var(--radius-sm, 6px);
  background: transparent;
  color: var(--color-ink-tertiary, #62666d);
  cursor: pointer;
  transition: background-color 150ms ease, color 150ms ease;
  flex-shrink: 0;
}

.notification__close:hover {
  background: var(--color-surface-2, #141516);
  color: var(--color-ink, #f7f8f8);
}

.notification__progress {
  height: 2px;
  border-radius: var(--radius-full, 9999px);
  background: var(--color-primary, #5e6ad2);
  opacity: 0.7;
  margin-top: var(--space-3, 12px);
  transition: width linear;
}

/* Position-based offsets */
.notification--top-right,
.notification--top-left,
.notification--top-center {
  position: fixed;
  top: 0;
  z-index: 1200;
}

.notification--bottom-right,
.notification--bottom-left,
.notification--bottom-center {
  position: fixed;
  bottom: 0;
  z-index: 1200;
}

/* Animations */
@keyframes notification-fade-in {
  from { opacity: 0; transform: translateY(-8px); }
  to { opacity: 1; transform: translateY(0); }
}

/* Light mode */
[data-theme="light"] .notification {
  background: var(--color-inverse-surface-1, #f5f6f6);
  border-color: var(--color-hairline-inverse, #e3e8ee);
  color: var(--color-inverse-ink, #000000);
}

[data-theme="light"] .notification__title {
  color: var(--color-inverse-ink, #000000);
}

[data-theme="light"] .notification__description {
  color: var(--color-inverse-ink-muted, #64748d);
}

/* Reduced motion */
@media (prefers-reduced-motion: reduce) {
  .notification {
    animation: none !important;
  }
}
</style>