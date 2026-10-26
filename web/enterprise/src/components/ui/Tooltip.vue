<template>
  <div
    ref="triggerRef"
    class="tooltip-trigger"
    @mouseenter="handleMouseEnter"
    @mouseleave="handleMouseLeave"
    @focus="handleFocus"
    @blur="handleBlur"
    @click="toggle"
    @keydown.enter="toggle"
    @keydown.esc="close"
  >
    <slot name="trigger">
      <slot />
    </slot>
  </div>

  <transition name="tooltip-fade">
    <div
      v-if="visible"
      ref="tooltipRef"
      class="tooltip"
      :class="[
        'tooltip--' + placement,
        { 'tooltip--arrow': showArrow }
      ]"
      :style="tooltipStyle"
      role="tooltip"
      id="tooltip-id"
    >
      <span class="tooltip__content">{{ content }}</span>
      <span v-if="showArrow" class="tooltip__arrow"></span>
    </div>
  </transition>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from "vue"

const props = defineProps({
  content: { type: String, required: true },
  placement: { type: String, default: "top", validator: v => ["top", "bottom", "left", "right"].includes(v) },
  disabled: { type: Boolean, default: false },
  trigger: { type: String, default: "hover", validator: v => ["hover", "focus", "click"].includes(v) },
  persistent: { type: Boolean, default: false },
  showArrow: { type: Boolean, default: true },
  delay: { type: [Number, Object], default: 0 }
})

const emit = defineEmits(["update:modelValue", "visible-change"])

const triggerRef = ref(null)
const tooltipRef = ref(null)

const visible = ref(false)
let hideTimer = null
let showTimer = null

const isTriggerHover = ref(false)

const tooltipStyle = computed(() => {
  if (!tooltipRef.value) return {}
  const triggerRect = triggerRef.value?.getBoundingClientRect()
  const tooltipRect = tooltipRef.value.getBoundingClientRect()
  const scrollTop = window.pageYOffset || document.documentElement.scrollTop
  const scrollLeft = window.pageXOffset || document.documentElement.scrollLeft

  let top = 0
  let left = 0

  switch (props.placement) {
    case "top":
      top = triggerRect.top + scrollTop - tooltipRect.height - 8
      left = triggerRect.left + scrollLeft + (triggerRect.width - tooltipRect.width) / 2
      break
    case "bottom":
      top = triggerRect.top + scrollTop + triggerRect.height + 8
      left = triggerRect.left + scrollLeft + (triggerRect.width - tooltipRect.width) / 2
      break
    case "left":
      top = triggerRect.top + scrollTop + (triggerRect.height - tooltipRect.height) / 2
      left = triggerRect.left + scrollLeft - tooltipRect.width - 8
      break
    case "right":
      top = triggerRect.top + scrollTop + (triggerRect.height - tooltipRect.height) / 2
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
  }, typeof props.delay === "number" ? props.delay : props.delay.show || 0)
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
  }, typeof props.delay === "number" ? props.delay : props.delay.hide || 0)
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
  if (props.trigger === "hover" || props.trigger === "click") {
    isTriggerHover.value = true
    show()
  }
}

function handleMouseLeave() {
  if (props.trigger === "hover") {
    isTriggerHover.value = false
    hide()
  }
}

function handleFocus() {
  if (props.trigger === "focus" || props.trigger === "click") {
    show()
  }
}

function handleBlur() {
  if (props.trigger === "focus") {
    hide()
  }
}

onMounted(() => {
  if (!triggerRef.value) return
  triggerRef.value.setAttribute("aria-describedby", "tooltip-id")
})

onUnmounted(() => {
  if (showTimer) clearTimeout(showTimer)
  if (hideTimer) clearTimeout(hideTimer)
})
</script>

<style scoped>
.tooltip-trigger {
  display: inline-flex;
  position: relative;
  cursor: pointer;
}

.tooltip {
  position: absolute;
  z-index: 1100;
  max-width: 200px;
  padding: var(--space-2, 8px) var(--space-3, 12px);
  border-radius: var(--radius-sm, 6px);
  background: var(--color-surface-4, #191a1b);
  color: var(--color-ink, #f7f8f8);
  font-size: var(--font-size-xs, 11px);
  font-weight: var(--font-weight-medium, 500);
  line-height: 1.4;
  white-space: nowrap;
  box-shadow: var(--shadow-lg, 0 8px 24px rgba(0, 0, 0, 0.5));
  animation: tooltip-fade-in 150ms ease;
}

.tooltip__content {
  display: inline-block;
}

.tooltip__arrow {
  position: absolute;
  width: 0;
  height: 0;
  border-color: transparent;
  border-style: solid;
}

.tooltip--top .tooltip__arrow {
  bottom: -4px;
  left: 50%;
  transform: translateX(-50%);
  border-width: 4px 4px 0;
  border-top-color: var(--color-surface-4, #191a1b);
}

.tooltip--bottom .tooltip__arrow {
  top: -4px;
  left: 50%;
  transform: translateX(-50%);
  border-width: 0 4px 4px;
  border-bottom-color: var(--color-surface-4, #191a1b);
}

.tooltip--left .tooltip__arrow {
  top: 50%;
  right: -4px;
  transform: translateY(-50%);
  border-width: 4px 0 4px 4px;
  border-left-color: var(--color-surface-4, #191a1b);
}

.tooltip--right .tooltip__arrow {
  top: 50%;
  left: -4px;
  transform: translateY(-50%);
  border-width: 4px 4px 0 0;
  border-right-color: var(--color-surface-4, #191a1b);
}

@keyframes tooltip-fade-in {
  from { opacity: 0; transform: translateY(-4px); }
  to { opacity: 1; transform: translateY(0); }
}

.tooltip-fade-enter-active { animation: tooltip-fade-in 150ms ease; }
.tooltip-fade-leave-active { animation: tooltip-fade-in 150ms ease reverse; }

/* Reduced motion */
@media (prefers-reduced-motion: reduce) {
  .tooltip,
  .tooltip-fade-enter-active,
  .tooltip-fade-leave-active {
    transition: none !important;
    animation: none !important;
  }
}

/* Light mode */
[data-theme="light"] .tooltip {
  background: var(--color-inverse-surface-4, #374151);
  color: var(--color-inverse-ink, #000000);
}

[data-theme="light"] .tooltip--top .tooltip__arrow {
  border-top-color: var(--color-inverse-surface-4, #374151);
}

[data-theme="light"] .tooltip--bottom .tooltip__arrow {
  border-bottom-color: var(--color-inverse-surface-4, #374151);
}

[data-theme="light"] .tooltip--left .tooltip__arrow {
  border-left-color: var(--color-inverse-surface-4, #374151);
}

[data-theme="light"] .tooltip--right .tooltip__arrow {
  border-right-color: var(--color-inverse-surface-4, #374151);
}
</style>