<template>
  <button
    :class="classList"
    :type="nativeType"
    :disabled="isDisabled"
    :aria-disabled="isDisabled"
    :aria-busy="loading"
    @click="handleClick"
  >
    <span class="btn__inner">
      <span v-if="loading" class="btn__spinner" aria-hidden="true">
        <svg class="btn__spinner-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" :stroke-width="spinnerStrokeWidth" stroke-linecap="round" stroke-linejoin="round">
          <circle class="btn__spinner-circle" cx="12" cy="12" r="10" />
        </svg>
      </span>
      <span v-if="!loading && icon" class="btn__icon" :class="iconPositionClass">
        <slot name="icon">
          <Icon :name="icon" :size="iconSize" aria-hidden="true" />
        </slot>
      </span>
      <span class="btn__content"><slot /></span>
    </span>
  </button>
</template>

<script setup>
import { computed } from "vue"
import Icon from "@/components/Icon.vue"

const props = defineProps({
  variant: { type: String, default: "primary" },     // primary | secondary | ghost | danger
  size: { type: String, default: "md" },              // sm | md | lg
  disabled: { type: Boolean, default: false },
  loading: { type: Boolean, default: false },
  block: { type: Boolean, default: false },
  icon: { type: String, default: "" },
  iconPosition: { type: String, default: "left" },    // left | right
  type: { type: String, default: "button" },
  nativeType: { type: String, default: "button" },
  modelValue: { type: Boolean, default: false }
})

const emit = defineEmits(["click", "update:modelValue"])

const isDisabled = computed(() => props.disabled || props.loading)

const iconSize = computed(() => {
  switch (props.size) {
    case "sm": return 14
    case "lg": return 20
    default: return 16
  }
})

const spinnerStrokeWidth = computed(() => {
  switch (props.size) {
    case "sm": return 2.5
    case "lg": return 3
    default: return 2.5
  }
})

const variantClasses = computed(() => {
  const v = props.variant
  const base = ["btn", `btn--${v}`]
  if (v === "primary") base.push("btn--primary")
  else if (v === "secondary") base.push("btn--secondary")
  else if (v === "ghost") base.push("btn--ghost")
  else if (v === "danger") base.push("btn--danger")
  return base
})

const sizeClasses = computed(() => [`btn--${props.size}`])

const classList = computed(() => [
  "btn",
  ...variantClasses.value,
  ...sizeClasses.value,
  { "btn--block": props.block, "btn--disabled": isDisabled.value }
])

const iconPositionClass = computed(() => ({
  "btn__icon--left": props.iconPosition === "left",
  "btn__icon--right": props.iconPosition === "right"
}))

function handleClick(evt) {
  if (isDisabled.value) return
  emit("click", evt)
  emit("update:modelValue", !props.modelValue)
}
</script>

<style scoped>
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
  color: var(--color-white);
  background: var(--color-primary, #5e6ad2);
  box-shadow: var(--shadow-sm, 0 1px 2px rgba(0,0,0,0.3));
  cursor: pointer;
  transition: background-color 150ms ease, border-color 150ms ease, box-shadow 150ms ease, opacity 150ms ease, transform 100ms ease;
  white-space: nowrap;
  position: relative;
  outline: none;
  text-decoration: none;
  flex-shrink: 0;
}

.btn__inner {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
}

/* ===== Sizes ===== */
.btn--sm {
  height: 32px;
  padding: 0 12px;
  font-size: 12px;
}
.btn--sm .btn__inner { gap: 6px; }

.btn--md {
  height: 40px;
  padding: 0 16px;
  font-size: 14px;
}
.btn--md .btn__inner { gap: 8px; }

.btn--lg {
  height: 48px;
  padding: 0 20px;
  font-size: 16px;
}
.btn--lg .btn__inner { gap: 10px; }

.btn--block { width: 100%; }

/* ===== Variants ===== */
/* --- Primary (Linear Lavender) --- */
.btn--primary {
  color: #ffffff;
  background: var(--color-primary, #5e6ad2);
  box-shadow: var(--shadow-sm, 0 1px 2px rgba(0,0,0,0.3));
}
.btn--primary:hover:not(.btn--disabled) {
  background: var(--color-primary-hover, #828fff);
}
.btn--primary:active:not(.btn--disabled) {
  background: var(--color-primary-press, #4a5bc4);
}

/* --- Secondary (Surface + Hairline) --- */
.btn--secondary {
  color: var(--color-ink, #f7f8f8);
  background: var(--color-surface-1, #0f1011);
  border-color: var(--color-hairline, #23252a);
  box-shadow: none;
}
.btn--secondary:hover:not(.btn--disabled) {
  background: var(--color-surface-2, #141516);
}
.btn--secondary:active:not(.btn--disabled) {
  background: var(--color-surface-3, #18191a);
}

/* --- Ghost (Transparent) --- */
.btn--ghost {
  color: var(--color-ink, #f7f8f8);
  background: transparent;
  box-shadow: none;
  border-color: transparent;
}
.btn--ghost:hover:not(.btn--disabled) {
  background: var(--color-surface-1, #0f1011);
}
.btn--ghost:active:not(.btn--disabled) {
  background: var(--color-surface-2, #141516);
}

/* --- Danger (Error Red) --- */
.btn--danger {
  color: #ffffff;
  background: var(--color-error, #ef4444);
  box-shadow: var(--shadow-sm, 0 1px 2px rgba(0,0,0,0.3));
}
.btn--danger:hover:not(.btn--disabled) {
  background: #dc2626;
}
.btn--danger:active:not(.btn--disabled) {
  background: #b91c1c;
}

/* ===== States ===== */
.btn:focus-visible {
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-primary, #5e6ad2) 40%, transparent);
  border-radius: 9999px;
}

.btn:active:not(.btn--disabled) {
  transform: scale(0.96);
}

.btn--disabled,
.btn[disabled] {
  opacity: 0.4;
  cursor: not-allowed;
  pointer-events: none;
  box-shadow: none;
  transform: none;
}

.btn--block { width: 100%; }

/* ===== Inner ===== */
.btn__inner {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 100%;
}

.btn__icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.btn__icon--left { margin-right: 0; }
.btn__icon--right { margin-left: 0; }

.btn__content {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: 1;
}

/* ===== Loading Spinner ===== */
.btn__spinner {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.btn--sm .btn__spinner { width: 14px; height: 14px; }
.btn--md .btn__spinner { width: 16px; height: 16px; }
.btn--lg .btn__spinner { width: 20px; height: 20px; }

.btn__spinner-svg {
  width: 100%;
  height: 100%;
  animation: spin 1s linear infinite;
}
.btn__spinner-circle {
  stroke: currentColor;
  stroke-width: 2.5;
  stroke-linecap: round;
  fill: none;
  stroke-dasharray: 64 48;
  stroke-dashoffset: 0;
  animation: btn-dash 1.4s ease-in-out infinite;
}
@keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }
@keyframes btn-dash {
  0% { stroke-dasharray: 1 64; stroke-dashoffset: 0; }
  50% { stroke-dasharray: 64 1; stroke-dashoffset: -32; }
  100% { stroke-dasharray: 64 1; stroke-dashoffset: -64; }
}

/* ===== Disabled ===== */
.btn--disabled,
.btn[disabled] {
  opacity: 0.4;
  cursor: not-allowed;
  pointer-events: none;
  box-shadow: none;
  transform: none;
}

/* ===== Reduced Motion ===== */
@media (prefers-reduced-motion: reduce) {
  .btn,
  .btn__spinner-svg,
  .btn__spinner-circle {
    transition: none !important;
    animation: none !important;
  }
}

/* ===== Dark mode (default) + Light mode ===== */
[data-theme="light"] .btn--secondary {
  background: var(--color-inverse-surface-1, #f5f6f6);
  border-color: var(--color-hairline-inverse, #e3e8ee);
  color: var(--color-inverse-ink, #000000);
}
[data-theme="light"] .btn--secondary:hover:not(.btn--disabled) {
  background: var(--color-inverse-surface-2, #f6f7f7);
}
[data-theme="light"] .btn--ghost:hover:not(.btn--disabled) {
  background: var(--color-inverse-surface-1, #f5f6f6);
}
</style>