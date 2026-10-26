<template>
  <Transition name="modal-fade" @before-enter="onBeforeEnter" @after-leave="onAfterLeave">
    <div
      v-if="modelValue"
      class="modal"
      :class="[
        'modal--' + size,
        { 'modal--centered': centered },
        { 'modal--fullscreen': fullscreen },
        { 'modal--destroyed': destroyOnClose && !modelValue }
      ]"
      role="dialog"
      aria-modal="true"
      :aria-labelledby="titleId"
      :aria-describedby="bodyId"
      tabindex="-1"
      @keydown="handleKeydown"
      ref="modalRef"
    >
      <!-- Mask/Backdrop -->
      <div
        v-if="!fullscreen"
        class="modal__mask"
        :class="{ 'modal__mask--closable': maskClosable }"
        @click="handleMaskClick"
        aria-hidden="true"
      />

      <!-- Modal Container -->
      <div
        class="modal__container"
        :style="containerStyle"
        @click.stop
      >
        <!-- Header -->
        <header v-if="hasHeader" class="modal__header">
          <slot name="header">
            <h2 class="modal__title" :id="titleId">{{ title }}</h2>
          </slot>
          <button
            v-if="closable"
            type="button"
            class="modal__close"
            @click="handleClose"
            :aria-label="closeLabel"
          >
            <svg class="modal__close-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <line x1="18" y1="6" x2="6" y2="18" />
              <line x1="6" y1="6" x2="18" y2="18" />
            </svg>
          </button>
        </header>

        <!-- Body -->
        <div class="modal__body" :id="bodyId" ref="bodyRef">
          <slot />
        </div>

        <!-- Footer -->
        <footer v-if="hasFooter" class="modal__footer">
          <slot name="footer">
            <div class="modal__footer-actions">
              <button
                v-if="showCancel"
                type="button"
                class="btn btn--secondary btn--default"
                :disabled="confirmLoading"
                @click="handleCancel"
              >
                {{ cancelText }}
              </button>
              <button
                type="button"
                class="btn btn--primary btn--default"
                :disabled="confirmLoading"
                :loading="confirmLoading"
                @click="handleConfirm"
              >
                {{ confirmLoading ? confirmLoadingText : confirmText }}
              </button>
            </div>
          </slot>
        </footer>
      </div>
    </div>
  </Transition>
</template>

<script setup>
import { computed, ref, watch, onMounted, onUnmounted, nextTick, useSlots } from "vue"

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  title: { type: String, default: "" },
  width: { type: [String, Number], default: 520 },
  size: { type: String, default: "default", validator: v => ["small", "default", "large", "full"].includes(v) },
  closable: { type: Boolean, default: true },
  closeOnClickMask: { type: Boolean, default: true },
  closeOnPressEsc: { type: Boolean, default: true },
  confirmLoading: { type: Boolean, default: false },
  cancelText: { type: String, default: "Cancel" },
  confirmText: { type: String, default: "Confirm" },
  confirmLoadingText: { type: String, default: "Confirming..." },
  destroyOnClose: { type: Boolean, default: false },
  centered: { type: Boolean, default: false },
  maskClosable: { type: Boolean, default: true },
  fullscreen: { type: Boolean, default: false },
  closeLabel: { type: String, default: "Close" }
})

const emit = defineEmits(["update:modelValue", "cancel", "confirm"])

const slots = useSlots()
const modalRef = ref(null)
const bodyRef = ref(null)
let lastFocusedElement = null
let scrollBarWidth = 0

const hasHeader = computed(() => !!props.title || !!slots.header)
const hasFooter = computed(() => !!slots.footer || !props.confirmLoading)
const showCancel = computed(() => !props.confirmLoading)

const titleId = `modal-title-${Math.random().toString(36).slice(2)}`
const bodyId = `modal-body-${Math.random().toString(36).slice(2)}`

const containerStyle = computed(() => {
  if (props.fullscreen) return {}
  const w = typeof props.width === "number" ? `${props.width}px` : props.width
  return { width: w, maxWidth: "90vw" }
})

// Scroll lock utilities
function getScrollBarWidth() {
  if (scrollBarWidth) return scrollBarWidth
  const outer = document.createElement("div")
  outer.style.visibility = "hidden"
  outer.style.overflow = "scroll"
  document.body.appendChild(outer)
  const inner = document.createElement("div")
  outer.appendChild(inner)
  scrollBarWidth = outer.offsetWidth - inner.offsetWidth
  outer.remove()
  return scrollBarWidth
}

function lockScroll() {
  const width = getScrollBarWidth()
  document.body.style.overflow = "hidden"
  document.body.style.paddingRight = `${width}px`
}

function unlockScroll() {
  document.body.style.overflow = ""
  document.body.style.paddingRight = ""
}

// Focus management
function saveLastFocus() {
  lastFocusedElement = document.activeElement
}

function restoreFocus() {
  if (lastFocusedElement && lastFocusedElement instanceof HTMLElement) {
    lastFocusedElement.focus()
    lastFocusedElement = null
  }
}

function trapFocus(e) {
  if (!modalRef.value) return
  const focusableElements = modalRef.value.querySelectorAll(
    "button, [href], input, select, textarea, [tabindex]:not([tabindex=\"-1\"])"
  )
  const firstElement = focusableElements[0]
  const lastElement = focusableElements[focusableElements.length - 1]

  if (e.key === "Tab") {
    if (e.shiftKey) {
      if (document.activeElement === firstElement) {
        e.preventDefault()
        lastElement?.focus()
      }
    } else {
      if (document.activeElement === lastElement) {
        e.preventDefault()
        firstElement?.focus()
      }
    }
  }
}

function focusFirstElement() {
  nextTick(() => {
    if (!modalRef.value) return
    const focusableElements = modalRef.value.querySelectorAll(
      "button, [href], input, select, textarea, [tabindex]:not([tabindex=\"-1\"])"
    )
    const firstElement = focusableElements[0]
    if (firstElement instanceof HTMLElement) {
      firstElement.focus()
    } else {
      modalRef.value.focus()
    }
  })
}

// Event handlers
function handleClose() {
  emit("update:modelValue", false)
  emit("cancel")
}

function handleMaskClick() {
  if (props.closeOnClickMask && props.maskClosable) {
    handleClose()
  }
}

function handleKeydown(e) {
  if (e.key === "Escape" && props.closeOnPressEsc) {
    handleClose()
    return
  }
  if (e.key === "Enter" && !e.shiftKey && !props.confirmLoading) {
    const target = e.target
    const isInTextArea = target.tagName === "TEXTAREA"
    const isInInput = target.tagName === "INPUT" && target.type !== "button" && target.type !== "submit"
    if (!isInTextArea && !isInInput) {
      handleConfirm()
    }
  }
  if (e.key === "Tab") {
    trapFocus(e)
  }
}

function handleCancel() {
  emit("cancel")
  emit("update:modelValue", false)
}

function handleConfirm() {
  emit("confirm")
  emit("update:modelValue", false)
}

function onBeforeEnter() {
  saveLastFocus()
  lockScroll()
}

function onAfterLeave() {
  unlockScroll()
  restoreFocus()
  if (props.destroyOnClose) {
    // Component will be destroyed by parent via v-if
  }
}

// Lifecycle
onMounted(() => {
  if (props.modelValue) {
    saveLastFocus()
    lockScroll()
    nextTick(focusFirstElement)
  }
})

onUnmounted(() => {
  unlockScroll()
})

watch(() => props.modelValue, (val) => {
  if (val) {
    saveLastFocus()
    lockScroll()
    nextTick(focusFirstElement)
  } else {
    unlockScroll()
    restoreFocus()
  }
})

// Expose methods for parent component
defineExpose({
  focus: focusFirstElement,
  close: handleClose
})
</script>

<style scoped>
/* ===========================================================================
 * Modal - OpsMesh DESIGN.md (Linear/Vercel-inspired)
 * =========================================================================== */

.modal {
  position: fixed;
  inset: 0;
  z-index: 1000;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: var(--space-4, 16px);
  pointer-events: none;
  overflow: hidden;
}

/* Mask/Backdrop */
.modal__mask {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.6);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  animation: modal-mask-fade-in 150ms ease;
  pointer-events: auto;
}

.modal__mask--closable {
  cursor: pointer;
}

@keyframes modal-mask-fade-in {
  from { opacity: 0; }
  to { opacity: 1; }
}

/* Container */
.modal__container {
  position: relative;
  z-index: 1;
  background: var(--color-surface-1, #0f1011);
  border: 1px solid var(--color-hairline, #23252a);
  border-radius: var(--radius-xl, 16px);
  box-shadow: var(--shadow-xl, 0 16px 48px rgba(0, 0, 0, 0.4));
  pointer-events: auto;
  display: flex;
  flex-direction: column;
  max-height: calc(100vh - 32px);
  overflow: hidden;
  animation: modal-scale-in 150ms ease;
  transform-origin: center;
}

@keyframes modal-scale-in {
  from {
    opacity: 0;
    transform: scale(0.95);
  }
  to {
    opacity: 1;
    transform: scale(1);
  }
}

/* Sizes */
.modal--small .modal__container {
  width: 100%;
  max-width: 360px;
}

.modal--default .modal__container {
  width: 100%;
  max-width: 520px;
}

.modal--large .modal__container {
  width: 100%;
  max-width: 720px;
}

.modal--full .modal__container {
  width: 100%;
  max-width: 960px;
}

.modal--centered .modal__container {
  align-self: center;
}

/* Fullscreen variant */
.modal--fullscreen {
  padding: 0;
  align-items: stretch;
  justify-content: stretch;
}

.modal--fullscreen .modal__container {
  border-radius: 0;
  max-height: 100vh;
  height: 100vh;
  width: 100vw;
  max-width: 100vw;
  margin: 0;
  animation: modal-scale-in 150ms ease;
}

/* Header */
.modal__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 24px;
  border-bottom: 1px solid var(--color-hairline, #23252a);
  flex-shrink: 0;
}

.modal__title {
  margin: 0;
  font-size: var(--font-size-lg, 18px);
  font-weight: var(--font-weight-semibold, 600);
  color: var(--color-ink, #f7f8f8);
  line-height: var(--line-height-tight, 1.25);
  word-break: break-word;
}

.modal__close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border: none;
  border-radius: var(--radius-md, 8px);
  background: transparent;
  color: var(--color-ink-muted, #d0d6e0);
  cursor: pointer;
  transition: background-color 150ms ease, color 150ms ease;
  flex-shrink: 0;
  margin-left: 12px;
}

.modal__close:hover {
  color: var(--color-ink, #f7f8f8);
}

.modal__close:focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-primary, #5e6ad2) 40%, transparent);
  border-radius: var(--radius-sm, 6px);
}

.modal__close-icon {
  width: 20px;
  height: 20px;
  flex-shrink: 0;
}

/* Body */
.modal__body {
  flex: 1 1 auto;
  overflow: auto;
  padding: 24px;
  min-height: 0;
  color: var(--color-ink-muted, #d0d6e0);
  line-height: var(--line-height-normal, 1.5);
  font-size: var(--font-size-base, 16px);
}

.modal__body:empty {
  padding: 0;
}

/* Footer */
.modal__footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 12px;
  padding: 20px 24px;
  border-top: 1px solid var(--color-hairline, #23252a);
  flex-shrink: 0;
}

.modal__footer-actions {
  display: flex;
  align-items: center;
  gap: 12px;
}

/* Buttons (pill) */
.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  height: 36px;
  padding: 0 16px;
  border: 1px solid transparent;
  border-radius: 9999px;
  font-family: var(--font-family-sans, inherit);
  font-weight: 500;
  font-size: 14px;
  line-height: 1;
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

.btn--secondary {
  color: var(--color-ink, #f7f8f8);
  background: var(--color-surface-1, #0f1011);
  border-color: var(--color-hairline, #23252a);
  box-shadow: none;
}

.btn--secondary:hover:not(:disabled) {
  background: var(--color-surface-2, #141516);
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

/* Responsive */
@media (max-width: 640px) {
  .modal {
    padding: 0;
    align-items: flex-end;
    justify-content: flex-start;
  }

  .modal__container {
    border-radius: var(--radius-xl, 16px) var(--radius-xl, 16px) 0 0;
    max-height: 85vh;
    height: auto;
    animation: modal-scale-in 150ms ease;
  }

  .modal__header {
    padding: 16px;
  }

  .modal__body {
    padding: 16px;
  }

  .modal__footer {
    padding: 12px 16px;
    flex-direction: column-reverse;
    align-items: stretch;
    gap: 8px;
  }

  .modal__footer-actions {
    width: 100%;
    justify-content: flex-end;
  }

  .modal__footer-actions .btn {
    flex: 1;
    justify-content: center;
  }
}

/* Light mode (inverse) */
[data-theme="light"] .modal__container {
  background: var(--color-inverse-surface-1, #f5f6f6);
  border-color: var(--color-hairline-inverse, #e3e8ee);
  box-shadow: var(--shadow-xl, 0 16px 48px rgba(0, 0, 0, 0.12));
}

[data-theme="light"] .modal__header {
  border-color: var(--color-hairline-inverse, #e3e8ee);
}

[data-theme="light"] .modal__title {
  color: var(--color-inverse-ink, #000000);
}

[data-theme="light"] .modal__close {
  color: var(--color-inverse-ink-muted, #64748d);
}

[data-theme="light"] .modal__close:hover {
  color: var(--color-inverse-ink, #000000);
}

[data-theme="light"] .modal__body {
  color: var(--color-inverse-ink-muted, #64748d);
}

[data-theme="light"] .modal__footer {
  border-color: var(--color-hairline-inverse, #e3e8ee);
}

[data-theme="light"] .btn--secondary {
  color: var(--color-inverse-ink, #000000);
  background: var(--color-inverse-surface-1, #f5f6f6);
  border-color: var(--color-hairline-inverse, #e3e8ee);
}

[data-theme="light"] .btn--secondary:hover:not(:disabled) {
  background: var(--color-inverse-surface-2, #f6f7f7);
}

/* Reduced motion */
@media (prefers-reduced-motion: reduce) {
  .modal__mask,
  .modal__container {
    animation: none !important;
    transition: none !important;
  }
}
</style>
