<template>
  <div
    ref="triggerRef"
    class="popover-trigger"
    :class="{ 'popover-trigger--click': trigger === 'click' }"
    @click="toggle"
    @keydown.enter="toggle"
    @keydown.esc="close"
    @mouseenter="handleMouseEnter"
    @mouseleave="handleMouseLeave"
    @focus="show"
    @blur="hide"
  >
    <slot name="trigger">
      <slot />
    </slot>
  </div>

  <transition name="popover-fade">
    <div
      v-if="visible"
      ref="popoverRef"
      class="popover"
      :class="[`popover--${placement}`]"
      :style="popoverStyle"
      role="tooltip"
      id="popover-id"
    >
      <div class="popover__content">
        <slot />
      </div>
      <span v-if="showArrow" class="popover__arrow"></span>
    </div>
  </transition>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from "vue"

const props = defineProps({
  placement: { type: String, default: "bottom", validator: v => ["top", "bottom", "left", "right"].includes(v) },
  trigger: { type: String, default: "click", validator: v => ["click", "hover"].includes(v) },
  disabled: { type: Boolean, default: false },
  showArrow: { type: Boolean, default: true },
  persistent: { type: Boolean, default: false }
})

const emit = defineEmits(["update:modelValue", "visible-change"])

const triggerRef = ref(null)
const popoverRef = ref(null)

const visible = ref(false)
let hideTimer = null
let showTimer = null

const popoverStyle = computed(() => {
  if (!popoverRef.value) return {}
  const triggerRect = triggerRef.value?.getBoundingClientRect()
  const popoverRect = popoverRef.value.getBoundingClientRect()
  const scrollTop = window.pageYOffset || document.documentElement.scrollTop
  const scrollLeft = window.pageXOffset || document.documentElement.scrollLeft

  let top = 0
  let left = 0

  switch (props.placement) {
    case "top":
      top = triggerRect.top + scrollTop - popoverRect.height - 8
      left = triggerRect.left + scrollLeft + (triggerRect.width - popoverRect.width) / 2
      break
    case "bottom":
      top = triggerRect.top + scrollTop + triggerRect.height + 8
      left = triggerRect.left + scrollLeft + (popoverRect.width - triggerRect.width) / 2
      break
    case "left":
      top = triggerRect.top + scrollTop + (triggerRect.height - popoverRect.height) / 2
      left = triggerRect.left + scrollLeft - popoverRect.width - 8
      break
    case "right":
      top = triggerRect.top + scrollTop + (triggerRect.height - popoverRect.height) / 2
      left = triggerRect.left + scrollLeft + triggerRect.width + 8
      break
  }

  return { top: `${top}px`, left: `${left}px` }
})

function show() {
  if (props.disabled) return
  if (hideTimer) {
    clearTimeout(hideTimer)
    hideTimer = null
  }
  showTimer = setTimeout(() => {
    visible.value = true
    emit("update:modelValue", true)
    emit("visible-change", true)
  }, 0)
}

function hide() {
  if (showTimer) {
    clearTimeout(showTimer)
    showTimer = null
  }
  hideTimer = setTimeout(() => {
    visible.value = false
    emit("update:modelValue", false)
    emit("visible-change", false)
  }, 0)
}

function toggle() {
  if (props.disabled) return
  if (visible.value) hide()
  else show()
}

function close() {
  hide()
}

function handleMouseEnter() {
  if (props.trigger === "hover") show()
}

function handleMouseLeave() {
  if (props.trigger === "hover") hide()
}

onMounted(() => {
  if (!triggerRef.value) return
  triggerRef.value.setAttribute("aria-describedby", "popover-id")
})

onUnmounted(() => {
  if (showTimer) clearTimeout(showTimer)
  if (hideTimer) clearTimeout(hideTimer)
})
</script>

<style scoped>
.popover-trigger {
  display: inline-flex;
  position: relative;
  cursor: pointer;
}

.popover-trigger--click {
  cursor: pointer;
}

.popover {
  position: absolute;
  z-index: 1100;
  max-width: 320px;
  padding: var(--space-3, 12px);
  border: 1px solid var(--color-hairline, #23252a);
  border-radius: var(--radius-lg, 8px);
  background: var(--color-surface-1, #0f1011);
  box-shadow: var(--shadow-lg, 0 8px 24px rgba(0, 0, 0, 0.5));
  animation: popover-fade-in 150ms ease;
}

.popover__content {
  display: flex;
  flex-direction: column;
  gap: var(--space-2, 8px);
}

.popover__arrow {
  position: absolute;
  width: 0;
  height: 0;
  border-color: transparent;
  border-style: solid;
}

.popover--top .popover__arrow {
  bottom: -4px;
  left: 50%;
  transform: translateX(-50%);
  border-width: 4px 4px 0;
  border-top-color: var(--color-surface-1, #0f1011);
}

.popover--bottom .popover__arrow {
  top: -4px;
  left: 50%;
  transform: translateX(-50%);
  border-width: 0 4px 4px;
  border-bottom-color: var(--color-surface-1, #0f1011);
}

.popover--left .popover__arrow {
  top: 50%;
  right: -4px;
  transform: translateY(-50%);
  border-width: 4px 0 4px 4px;
  border-left-color: var(--color-surface-1, #0f1011);
}

.popover--right .popover__arrow {
  top: 50%;
  left: -4px;
  transform: translateY(-50%);
  border-width: 4px 4px 0 0;
  border-right-color: var(--color-surface-1, #0f1011);
}

@keyframes popover-fade-in {
  from { opacity: 0; transform: translateY(-4px); }
  to { opacity: 1; transform: translateY(0); }
}

.popover-fade-enter-active { animation: popover-fade-in 150ms ease; }
.popover-fade-leave-active { animation: popover-fade-in 150ms ease reverse; }

/* Reduced motion */
@media (prefers-reduced-motion: reduce) {
  .popover,
  .popover-fade-enter-active,
  .popover-fade-leave-active {
    transition: none !important;
    animation: none !important;
  }
}

/* Light mode */
[data-theme="light"] .popover {
  background: var(--color-inverse-surface-1, #f5f6f6);
  border-color: var(--color-hairline-inverse, #e3e8ee);
  box-shadow: var(--shadow-lg, 0 8px 24px rgba(0, 0, 0, 0.12));
}

[data-theme="light"] .popover--top .popover__arrow {
  border-top-color: var(--color-inverse-surface-1, #f5f6f6);
}

[data-theme="light"] .popover--bottom .popover__arrow {
  border-bottom-color: var(--color-inverse-surface-1, #f5f6f6);
}

[data-theme="light"] .popover--left .popover__arrow {
  border-left-color: var(--color-inverse-surface-1, #f5f6f6);
}

[data-theme="light"] .popover--right .popover__arrow {
  border-right-color: var(--color-inverse-surface-1, #f5f6f6);
}
</style>