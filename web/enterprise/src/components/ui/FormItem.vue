<template>
  <div class="form-item" :class="itemClasses" :style="itemStyle">
    <div v-if="showLabel" class="form-item__label-group">
      <label
        :for="inputId"
        class="form-item__label"
        :class="{
          'form-item__label--required': required,
          'form-item__label--error': hasError
        }"
      >
        {{ label }}
        <span v-if="required" class="form-item__label-required" aria-hidden="true">*</span>
      </label>
    </div>

    <div class="form-item__control" :class="{ 'form-item__control--error': hasError }">
      <div class="form-item__control-inner">
        <slot />
      </div>

      <div v-if="showErrorMessage" class="form-item__error-message" role="alert">
        <svg class="form-item__error-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <circle cx="12" cy="12" r="10" />
          <line x1="12" y1="8" x2="12" y2="12" />
          <line x1="12" y1="16" x2="12.01" y2="16" />
        </svg>
        <span class="form-item__error-text">{{ errorMessage }}</span>
      </div>

      <div v-if="!hasError && helperText" class="form-item__helper">
        {{ helperText }}
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, ref, watch } from "vue"

const props = defineProps({
  prop: { type: String, default: "" },
  label: { type: String, default: "" },
  required: { type: Boolean, default: false },
  rules: { type: [Array, Function], default: () => [] },
  showLabel: { type: Boolean, default: true },
  showErrorMessage: { type: Boolean, default: true },
  helperText: { type: String, default: "" },
  error: { type: [Boolean, String], default: false },
  size: { type: String, default: "md" },
  labelWidth: { type: [String, Number], default: 100 },
  labelAlign: { type: String, default: "right" },
  disabled: { type: Boolean, default: false }
})

const emit = defineEmits(["update:modelValue", "change", "focus", "blur"])

const model = {
  value: ref("")
}

const inputId = `form-item-${Math.random().toString(36).slice(2)}`

const errorMessage = computed(() => {
  if (typeof props.error === "string") return props.error
  return ""
})

const hasError = computed(() => {
  if (props.error === true) return true
  if (typeof props.error === "string") return props.error.length > 0
  return !!errorMessage.value
})

const itemClasses = computed(() => [
  "form-item",
  `form-item--${props.size}`,
  {
    "form-item--error": hasError.value,
    "form-item--disabled": props.disabled,
    "form-item--required": props.required
  }
])

const itemStyle = computed(() => {
  const style = {}
  if (props.labelWidth) {
    style.setProperty("--form-item-label-width", typeof props.labelWidth === "number" ? `${props.labelWidth}px` : props.labelWidth)
  }
  return style
})

const showLabel = computed(() => props.showLabel && (props.label || props.required))

watch(() => props.model, (val) => {
  model.value = val
  emit("update:modelValue", val)
}, { immediate: true })
</script>

<style scoped>
.form-item {
  display: flex;
  flex-direction: column;
  width: 100%;
  position: relative;
  --form-item-label-width: 100px;
}

.form-item__label-group {
  display: flex;
  align-items: flex-start;
  gap: var(--space-1, 4px);
  margin-bottom: var(--space-1, 4px);
}

.form-item__label {
  font-size: var(--font-size-sm, 12px);
  font-weight: 500;
  color: var(--color-ink-muted, #d0d6e0);
  white-space: nowrap;
  flex-shrink: 0;
  width: var(--form-item-label-width);
  padding-top: 2px;
  line-height: 1.4;
}

.form-item__label--required {
  color: var(--color-ink, #f7f8f8);
}

.form-item__label--error {
  color: var(--color-error, #ef4444);
}

.form-item__label-required {
  font-size: var(--font-size-xs, 10px);
  color: var(--color-error, #ef4444);
  line-height: 1;
  margin-left: 4px;
}

.form-item__control {
  display: flex;
  flex-direction: column;
  gap: var(--space-1, 4px);
}

.form-item__control-inner {
  position: relative;
}

.form-item__control--error .form-item__control-inner {
  position: relative;
}

.form-item__error-message {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1, 4px);
  color: var(--color-error, #ef4444);
  font-size: var(--font-size-xs, 11px);
  line-height: 1.4;
  animation: form-item-fade-in 150ms ease;
}

.form-item__error-icon {
  width: 16px;
  height: 16px;
  flex-shrink: 0;
}

.form-item__error-text {
  flex: 1;
  min-width: 0;
}

.form-item__helper {
  color: var(--color-ink-muted, #d0d6e0);
  font-size: var(--font-size-xs, 11px);
  line-height: 1.4;
}

@keyframes form-item-fade-in {
  from { opacity: 0; transform: translateY(-4px); }
  to { opacity: 1; transform: translateY(0); }
}

/* Label alignment variants */
.form-item__label-group {
  width: 100%;
}
</style>