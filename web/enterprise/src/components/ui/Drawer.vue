<template>
  <transition name="drawer-fade">
    <div
      v-if="visible"
      class="drawer-mask"
      :class="maskClass"
      @click="handleMaskClick"
      :style="maskStyle"
      aria-hidden="true"
    >
      <div
        ref="drawerRef"
        class="drawer"
        :class="drawerClass"
        :style="drawerStyle"
        role="dialog"
        aria-modal="true"
        :aria-labelledby="titleId"
        :aria-describedby="descriptionId"
        @keydown.esc="handleEsc"
        tabindex="-1"
      >
        <div v-if="showHeader" class="drawer__header" :class="headerClass">
          <h2
            v-if="title"
            :id="titleId"
            class="drawer__title"
          >{{ title }}</h2>
          <slot name="header" />
          <button
            v-if="showClose"
            type="button"
            class="drawer__close"
            :class="closeBtnClass"
            :style="closeBtnStyle"
            :aria-label="closeLabel"
            @click="handleCancel"
            @keydown.enter.prevent="handleCancel"
            @keydown.space.prevent="handleCancel"
          >
            <svg class="drawer__close-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <line x1="18" y1="6" x2="6" y2="18" />
              <line x1="6" y1="6" x2="18" y2="18" />
            </svg>
          </button>
        </div>

        <div
          v-if="showBody"
          class="drawer__body"
          :class="bodyClass"
          ref="bodyRef"
        >
          <slot name="body" />
          <slot />
        </div>

        <div v-if="showFooter" class="drawer__footer" :class="footerClass">
          <slot name="footer">
            <div class="drawer__footer-actions">
              <button
                v-if="!hideCancel"
                type="button"
                class="btn btn--ghost btn--default"
                @click="handleCancel"
                :disabled="confirmLoading"
              >
                {{ cancelText }}
              </button>
              <button
                type="button"
                class="btn btn--primary btn--default"
                @click="handleConfirm"
                :disabled="confirmLoading"
                :aria-busy="confirmLoading ? 'true' : 'false'"
              >
                <span v-if="confirmLoading" class="btn__spinner">
                  <svg class="btn__spinner-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" aria-hidden="true">
                    <circle class="btn__spinner-circle" cx="12" cy="12" r="10" />
                  </svg>
                </span>
                {{ confirmText }}
              </button>
            </div>
          </slot>
        </div>
      </div>
    </div>
  </transition>
</template>

<script setup>
import { computed, ref, watch, nextTick, onMounted, onUnmounted } from 'vue'
import { useScrollLock } from '@/composables/useScrollLock'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  title: { type: String, default: '' },
  direction: { type: String, default: 'right', validator: (v) => ['right', 'left', 'top', 'bottom'].includes(v) },
  size: { type: [String, Number], default: () => undefined },
  closable: { type: Boolean, default: true },
  closeOnClickMask: { type: Boolean, default: true },
  closeOnPressEsc: { type: Boolean, default: true },
  destroyOnClose: { type: Boolean, default: false },
  maskClosable: { type: Boolean, default: true },
  showClose: { type: Boolean, default: true },
  cancelText: { type: String, default: 'Cancel' },
  confirmText: { type: String, default: 'Confirm' },
  confirmLoading: { type: Boolean, default: false },
  hideCancel: { type: Boolean, default: false },
  width: { type: [String, Number], default: () => undefined },
  height: { type: [String, Number], default: () => undefined },
  fullscreen: { type: Boolean, default: false },
  bodyOverflow: { type: String, default: 'auto' },
  maskStyle: { type: Object, default: () => ({}) },
  wrapperClass: { type: String, default: '' },
  headerClass: { type: String, default: '' },
  bodyClass: { type: String, default: '' },
  footerClass: { type: String, default: '' },
  closeIcon: { type: String, default: '' },
  closeBtnClass: { type: String, default: '' },
  closeBtnStyle: { type: Object, default: () => ({}) },
  class: { type: String, default: '' },
  style: { type: Object, default: () => ({}) },
  zIndex: { type: Number, default: 1000 },
  appendTo: { type: [String, HTMLElement], default: 'body' },
  beforeClose: { type: Function, default: null },
  onBeforeLeave: { type: Function, default: null },
  onLeave: { type: Function, default: null },
  onAfterLeave: { type: Function, default: null },
  onBeforeEnter: { type: Function, default: null },
  onEnter: { type: Function, default: null },
  onAfterEnter: { type: Function, default: null },
})

const emit = defineEmits([
  'update:modelValue',
  'cancel',
  'confirm',
  'open',
  'opened',
  'close',
  'closed',
  'before-close',
  'before-leave',
  'leave',
  'after-leave',
  'before-enter',
  'enter',
  'after-enter',
  'visible-change',
])

const drawerRef = ref(null)
const bodyRef = ref(null)
const titleId = `drawer-title-${Math.random().toString(36).slice(2)}`
const descriptionId = `drawer-desc-${Math.random().toString(36).slice(2)}`

const visible = ref(props.modelValue)
let scrollLockCleanup = null
let previousFocusedElement = null

const isHorizontal = computed(() => props.direction === 'right' || props.direction === 'left')

const drawerSize = computed(() => {
  const fallback = isHorizontal.value ? 480 : 320
  if (isHorizontal.value) {
    if (props.width != null) {
      return typeof props.width === 'number' ? `${props.width}px` : props.width
    }
    if (props.size != null) {
      return typeof props.size === 'number' ? `${props.size}px` : props.size
    }
    return `${fallback}px`
  }
  if (props.height != null) {
    return typeof props.height === 'number' ? `${props.height}px` : props.height
  }
  if (props.size != null) {
    return typeof props.size === 'number' ? `${props.size}px` : props.size
  }
  return `${fallback}px`
})

const showHeader = computed(() => !!props.title || !!props.$slots?.header)
const showBody = computed(() => true)
const showFooter = computed(() => !!props.$slots?.footer || !props.hideCancel)

const maskClass = computed(() => [
  'drawer-mask',
  `drawer-mask--${props.direction}`,
  { 'drawer-mask--fullscreen': props.fullscreen },
  { 'drawer-mask--open': visible.value },
])

const drawerClass = computed(() => [
  'drawer',
  `drawer--${props.direction}`,
  { 'drawer--fullscreen': props.fullscreen },
  props.wrapperClass,
  props.class,
])

const drawerStyle = computed(() => {
  const style = { ...props.style }
  if (isHorizontal.value) {
    style.width = drawerSize.value
    style.maxWidth = '100vw'
    style.maxHeight = '100vh'
  } else {
    style.height = drawerSize.value
    style.maxHeight = '100vh'
    style.maxWidth = '100vw'
  }
  if (props.zIndex) {
    style.zIndex = props.zIndex
  }
  if (props.bodyOverflow) {
    style.overflow = props.bodyOverflow
  }
  return style
})

const maskStyle = computed(() => ({
  ...props.maskStyle,
  zIndex: props.zIndex ? props.zIndex - 1 : 999,
}))

const closeLabel = 'Close'

function handleMaskClick() {
  if (props.maskClosable && props.closeOnClickMask) {
    handleCancel()
  }
}

function handleEsc(_event) {
  if (props.closeOnPressEsc) {
    handleCancel()
  }
}

async function handleCancel() {
  emit('cancel')
  emit('update:modelValue', false)
}

async function handleConfirm() {
  emit('confirm')
}

function trapFocus(event) {
  if (!visible.value || !drawerRef.value) return
  if (event.key !== 'Tab') return

  const focusableElements = drawerRef.value.querySelectorAll(
    'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'
  )
  const firstElement = focusableElements[0]
  const lastElement = focusableElements[focusableElements.length - 1]

  if (event.shiftKey) {
    if (document.activeElement === firstElement) {
      event.preventDefault()
      lastElement?.focus()
    }
  } else {
    if (document.activeElement === lastElement) {
      event.preventDefault()
      firstElement?.focus()
    }
  }
}

async function openDrawer() {
  if (visible.value) return

  previousFocusedElement = document.activeElement
  emit('before-enter')
  emit('open')
  visible.value = true

  await nextTick()

  if (drawerRef.value) {
    drawerRef.value.focus()
    document.addEventListener('keydown', trapFocus)
  }

  scrollLockCleanup = useScrollLock()
  emit('enter')
  emit('after-enter')
  emit('opened')
}

async function closeDrawer(trigger = 'user') {
  if (!visible.value) return

  if (props.beforeClose) {
    const shouldClose = await props.beforeClose()
    if (!shouldClose) return
  }

  emit('before-leave', trigger)
  emit('close', trigger)
  visible.value = false

  await nextTick()

  document.removeEventListener('keydown', trapFocus)

  if (scrollLockCleanup) {
    scrollLockCleanup()
    scrollLockCleanup = null
  }

  if (previousFocusedElement) {
    previousFocusedElement.focus()
    previousFocusedElement = null
  }

  emit('leave', trigger)
  emit('after-leave', trigger)
  emit('closed', trigger)
}

watch(() => props.modelValue, (newVal) => {
  if (newVal) {
    openDrawer()
  } else {
    closeDrawer()
  }
}, { immediate: true })

watch(visible, (newVal) => {
  emit('update:modelValue', newVal)
  emit('visible-change', newVal)
})

onMounted(() => {
  if (props.modelValue) {
    openDrawer()
  }
})

onUnmounted(() => {
  document.removeEventListener('keydown', trapFocus)
  if (scrollLockCleanup) {
    scrollLockCleanup()
  }
})
</script>

<style scoped>
/* ===========================================================================
 * Drawer - OpsMesh (Linear design system)
 * =========================================================================== */

/* Mask / Backdrop */
.drawer-mask {
  position: fixed;
  inset: 0;
  display: flex;
  pointer-events: none;
  opacity: 0;
  transition: opacity 200ms ease;
  z-index: var(--z-modal-backdrop, 999);
}

.drawer-mask--open {
  opacity: 1;
  pointer-events: auto;
}

.drawer-mask::before {
  content: '';
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.6);
  backdrop-filter: blur(4px);
  -webkit-backdrop-filter: blur(4px);
  transition: opacity 200ms ease;
}

/* Drawer Panel */
.drawer {
  position: relative;
  display: flex;
  flex-direction: column;
  background: var(--color-surface-1, #0f1011);
  border: 1px solid var(--color-hairline, #23252a);
  border-radius: 0;
  box-shadow: var(--shadow-xl, 0 16px 48px rgba(0, 0, 0, 0.45));
  overflow: hidden;
  pointer-events: auto;
  flex-shrink: 0;
  outline: none;
  color: var(--color-ink, #f7f8f8);
}

.drawer:focus-visible {
  box-shadow: var(--shadow-xl, 0 16px 48px rgba(0, 0, 0, 0.45)), 0 0 0 3px color-mix(in srgb, var(--color-primary, #5e6ad2) 40%, transparent);
}

/* Directions */
.drawer--right {
  margin-left: auto;
  transform: translateX(100%);
  border-left: 1px solid var(--color-hairline, #23252a);
}

.drawer--left {
  margin-right: auto;
  transform: translateX(-100%);
  border-right: 1px solid var(--color-hairline, #23252a);
}

.drawer--top {
  margin-bottom: auto;
  transform: translateY(-100%);
}

.drawer--bottom {
  margin-top: auto;
  transform: translateY(100%);
}

/* Fullscreen variant */
.drawer--fullscreen {
  width: 100vw !important;
  height: 100vh !important;
  max-width: 100vw !important;
  max-height: 100vh !important;
  border-radius: 0 !important;
  border: none !important;
}

.drawer-mask--fullscreen .drawer {
  border-radius: 0;
}

/* Animation - Slide in/out */
.drawer-mask--open .drawer--right {
  animation: drawer-slide-in-right 200ms ease-out forwards;
}
.drawer-mask:not(.drawer-mask--open) .drawer--right {
  animation: drawer-slide-out-right 200ms ease-in forwards;
}

.drawer-mask--open .drawer--left {
  animation: drawer-slide-in-left 200ms ease-out forwards;
}
.drawer-mask:not(.drawer-mask--open) .drawer--left {
  animation: drawer-slide-out-left 200ms ease-in forwards;
}

.drawer-mask--open .drawer--top {
  animation: drawer-slide-in-top 200ms ease-out forwards;
}
.drawer-mask:not(.drawer-mask--open) .drawer--top {
  animation: drawer-slide-out-top 200ms ease-in forwards;
}

.drawer-mask--open .drawer--bottom {
  animation: drawer-slide-in-bottom 200ms ease-out forwards;
}
.drawer-mask:not(.drawer-mask--open) .drawer--bottom {
  animation: drawer-slide-out-bottom 200ms ease-in forwards;
}

@keyframes drawer-slide-in-right {
  from { transform: translateX(100%); }
  to { transform: translateX(0); }
}
@keyframes drawer-slide-out-right {
  from { transform: translateX(0); }
  to { transform: translateX(100%); }
}

@keyframes drawer-slide-in-left {
  from { transform: translateX(-100%); }
  to { transform: translateX(0); }
}
@keyframes drawer-slide-out-left {
  from { transform: translateX(0); }
  to { transform: translateX(-100%); }
}

@keyframes drawer-slide-in-top {
  from { transform: translateY(-100%); }
  to { transform: translateY(0); }
}
@keyframes drawer-slide-out-top {
  from { transform: translateY(0); }
  to { transform: translateY(-100%); }
}

@keyframes drawer-slide-in-bottom {
  from { transform: translateY(100%); }
  to { transform: translateY(0); }
}
@keyframes drawer-slide-out-bottom {
  from { transform: translateY(0); }
  to { transform: translateY(100%); }
}

/* Fade animation for mask */
.drawer-fade-enter-active,
.drawer-fade-leave-active {
  transition: opacity 200ms ease;
}
.drawer-fade-enter-from {
  opacity: 0;
}
.drawer-fade-leave-to {
  opacity: 0;
}

/* Header */
.drawer__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3, 12px);
  padding: 24px;
  border-bottom: 1px solid var(--color-hairline, #23252a);
  background: var(--color-surface-1, #0f1011);
  flex-shrink: 0;
  min-height: 56px;
}

.drawer__title {
  margin: 0;
  font-size: var(--font-size-lg, 16px);
  font-weight: 600;
  color: var(--color-ink, #f7f8f8);
  line-height: 1.25;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.drawer__close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  padding: 0;
  border: none;
  border-radius: var(--radius-sm, 6px);
  background: transparent;
  color: var(--color-ink-muted, #d0d6e0);
  cursor: pointer;
  transition: background-color 150ms ease, color 150ms ease;
  flex-shrink: 0;
  margin-left: var(--space-2, 8px);
}

.drawer__close:hover {
  background: var(--color-surface-2, #141516);
  color: var(--color-ink, #f7f8f8);
}

.drawer__close:focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-primary, #5e6ad2) 40%, transparent);
}

.drawer__close-icon {
  width: 18px;
  height: 18px;
  flex-shrink: 0;
}

/* Body */
.drawer__body {
  flex: 1 1 auto;
  overflow: auto;
  padding: 24px;
  min-height: 0;
  -webkit-overflow-scrolling: touch;
  color: var(--color-ink, #f7f8f8);
}

/* Footer */
.drawer__footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: var(--space-3, 12px);
  padding: 20px 24px;
  border-top: 1px solid var(--color-hairline, #23252a);
  background: var(--color-surface-1, #0f1011);
  flex-shrink: 0;
  flex-wrap: wrap;
}

.drawer__footer-actions {
  display: flex;
  align-items: center;
  gap: var(--space-3, 12px);
  width: 100%;
  justify-content: flex-end;
}

/* Buttons */
.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0 16px;
  border: 1px solid transparent;
  border-radius: 9999px;
  font-family: var(--font-family-sans, var(--font));
  font-weight: 500;
  line-height: 1;
  color: var(--color-ink, #f7f8f8);
  background: transparent;
  cursor: pointer;
  transition: background-color 150ms ease, border-color 150ms ease, box-shadow 150ms ease, opacity 150ms ease;
  white-space: nowrap;
  outline: none;
}

.btn--default {
  height: 36px;
  padding: 0 16px;
  font-size: 14px;
}

.btn--primary {
  color: #ffffff;
  background: var(--color-primary, #5e6ad2);
  box-shadow: var(--shadow-sm, 0 1px 2px rgba(0, 0, 0, 0.3));
}

.btn--primary:hover:not(:disabled) {
  background: var(--color-primary-hover, #828fff);
}

.btn--primary:active:not(:disabled) {
  background: var(--color-primary-press, #4a5bc4);
}

.btn--ghost {
  color: var(--color-ink, #f7f8f8);
  background: transparent;
  box-shadow: none;
  border-color: transparent;
}

.btn--ghost:hover:not(:disabled) {
  background: var(--color-surface-2, #141516);
}

.btn--ghost:active:not(:disabled) {
  background: var(--color-surface-3, #18191a);
}

.btn:focus-visible {
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-primary, #5e6ad2) 40%, transparent);
}

.btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
  pointer-events: none;
  box-shadow: none;
}

.btn__spinner {
  display: inline-flex;
  margin-right: 6px;
}

.btn__spinner-svg {
  width: 14px;
  height: 14px;
  animation: btn-spin 0.8s linear infinite;
}

.btn__spinner-circle {
  stroke-dasharray: 50;
  stroke-dashoffset: 25;
}

@keyframes btn-spin {
  to { transform: rotate(360deg); }
}

/* Responsive */
@media (max-width: 640px) {
  .drawer--right,
  .drawer--left {
    width: 100vw !important;
    max-width: 100vw !important;
    border-radius: 0 !important;
  }

  .drawer--top,
  .drawer--bottom {
    height: 100vh !important;
    max-height: 100vh !important;
    border-radius: 0 !important;
  }

  .drawer__header {
    padding: 16px;
  }

  .drawer__body {
    padding: 16px;
  }

  .drawer__footer {
    padding: 12px 16px;
  }

  .drawer__footer-actions {
    width: 100%;
  }

  .drawer__footer-actions .btn {
    flex: 1;
    justify-content: center;
  }
}

/* Reduced motion */
@media (prefers-reduced-motion: reduce) {
  .drawer-mask,
  .drawer,
  .drawer__close,
  .btn,
  .btn__spinner-svg {
    transition: none !important;
    animation: none !important;
  }

  .drawer-mask--open .drawer--right,
  .drawer-mask--open .drawer--left,
  .drawer-mask--open .drawer--top,
  .drawer-mask--open .drawer--bottom {
    transform: translate(0);
  }
}

/* Light mode (Inverse) */
[data-theme="light"] .drawer {
  background: var(--color-inverse-surface-1, #f5f6f6);
  border-color: var(--color-hairline-inverse, #e3e8ee);
  color: var(--color-inverse-ink, #000000);
}

[data-theme="light"] .drawer__header {
  background: var(--color-inverse-surface-1, #f5f6f6);
  border-bottom-color: var(--color-hairline-inverse, #e3e8ee);
}

[data-theme="light"] .drawer__title {
  color: var(--color-inverse-ink, #000000);
}

[data-theme="light"] .drawer__close {
  color: var(--color-inverse-ink-muted, #64748d);
}

[data-theme="light"] .drawer__close:hover {
  background: var(--color-inverse-surface-2, #f6f7f7);
  color: var(--color-inverse-ink, #000000);
}

[data-theme="light"] .drawer__body {
  color: var(--color-inverse-ink, #000000);
}

[data-theme="light"] .drawer__footer {
  background: var(--color-inverse-surface-1, #f5f6f6);
  border-top-color: var(--color-hairline-inverse, #e3e8ee);
}

[data-theme="light"] .btn--ghost:hover:not(:disabled) {
  background: var(--color-inverse-surface-2, #f6f7f7);
}

[data-theme="light"] .btn--ghost:active:not(:disabled) {
  background: var(--color-inverse-surface-2, #f6f7f7);
}
</style>
