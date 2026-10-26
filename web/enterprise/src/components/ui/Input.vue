<template>
  <div class="input" :class="wrapperClasses">
    <label
      v-if="label"
      :for="inputId"
      class="input__label"
      :class="{ 'input__label--required': required }"
    >
      {{ label }}
      <span v-if="required" class="input__label-required" aria-hidden="true">*</span>
    </label>

    <div class="input__field">
      <span
        v-if="leftIcon || $slots['left-icon']"
        class="input__icon input__icon--left"
        aria-hidden="true"
      >
        <slot name="left-icon">
          <svg v-if="leftIcon" class="input__icon-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path :d="iconPath(leftIcon)" />
          </svg>
        </slot>
      </span>

      <input
        ref="inputRef"
        :id="inputId"
        class="input__native"
        :type="nativeType"
        :value="textValue"
        :placeholder="placeholder"
        :disabled="disabled"
        :readonly="readonly"
        :aria-invalid="isError ? 'true' : 'false'"
        :aria-disabled="disabled ? 'true' : 'false'"
        :aria-required="required ? 'true' : 'false'"
        :aria-describedby="helperId"
        @input="handleInput"
        @change="handleChange"
        @focus="handleFocus"
        @blur="handleBlur"
      />

      <button
        v-if="showClear"
        type="button"
        class="input__control input__control--clear"
        :disabled="disabled || readonly"
        :aria-label="clearTitle"
        @click.prevent="handleClear"
        @mousedown.prevent
      >
        <svg class="input__icon-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M18 6L6 18M6 6l12 12" />
        </svg>
      </button>

      <button
        v-if="showToggle"
        type="button"
        class="input__control input__control--toggle"
        :aria-pressed="passwordVisible ? 'true' : 'false'"
        :aria-label="passwordVisible ? hideTitle : showTitle"
        @click.prevent="togglePassword"
      >
        <svg class="input__icon-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M22 12c0 4.42-4.48 8-10 8S2 16.42 2 12 6.48 4 11 4 22 7.58 22 12z" />
          <path fill="currentColor" d="M12 12a3 3 0 1 0 0-6 3 3 0 0 0 0 6z" />
        </svg>
      </button>

      <span
        v-if="rightIcon || $slots['right-icon']"
        class="input__icon input__icon--right"
        aria-hidden="true"
      >
        <slot name="right-icon">
          <svg v-if="rightIcon" class="input__icon-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path :d="iconPath(rightIcon)" />
          </svg>
        </slot>
      </span>
    </div>

    <div v-if="showFooter" class="input__footer">
      <span
        v-if="error"
        class="input__helper input__helper--error"
        :id="helperId"
        aria-live="polite"
      >
        {{ error }}
      </span>
      <span v-else-if="helperText" class="input__helper" :id="helperId">
        {{ helperText }}
      </span>
    </div>
  </div>
</template>

<script setup>
import { computed, nextTick, ref } from "vue"

const props = defineProps({
  modelValue: { type: [String, Number], default: "" },
  type: { type: String, default: "text" },
  placeholder: { type: String, default: "" },
  disabled: { type: Boolean, default: false },
  readonly: { type: Boolean, default: false },
  clearable: { type: Boolean, default: false },
  showPassword: { type: Boolean, default: false },
  leftIcon: { type: String, default: "" },
  rightIcon: { type: String, default: "" },
  size: { type: String, default: "md" },          // sm | md | lg
  error: { type: Boolean, default: false },
  label: { type: String, default: "" },
  helperText: { type: String, default: "" },
  required: { type: Boolean, default: false }
})

const emit = defineEmits(["update:modelValue", "change", "input", "focus", "blur", "clear", "toggle-password"])

let inputUid = 0
const inputRef = ref(null)
const inputId = ref(props.label ? `ops-input-${++inputUid}` : undefined)
const passwordVisible = ref(false)
const isFocused = ref(false)

const iconPaths = {
  user: "M19 21V5a7 7 0 0 0-7-7H4a7 7 0 0 0-7 7v16",
  search: "M21 21l-6-6m2-5a7 7 0 1 1-14 0 7 7 0 0 1 14 0z",
  mail: "M4 4v12c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V4",
  phone: "M22 16.92v3a2 2 0 0 1-2.18 2 19.79 19.79 0 0 1-8.63-3.07 19.5 19.5 0 0 1-6-6 19.79 19.5 0 0 1-3.07-8.63A2 2 0 0 1 4.11 2h3a2 2 0 0 1 2 1.72 12.84 12.84 0 0 0 .7 2.81 2 2 0 0 1-.45 2.11L8.09 9.91a16 16 0 0 0 6 6l1.27-1.27a2 2 0 0 1 2.11-.45 12.84 12.84 0 0 0 2.81.7A2 2 0 0 1 22 16.92z",
  calendar: "M3 3v18h18V3H3zm18 1h-7.5a2 2 0 0 1-2-2V5h-7V3a2 2 0 0 1-2-2H3a2 2 0 0 0-2 2v18a2 2 0 0 0 2 2h18a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2z",
  tag: "M20.59 13.41l-7.17 7.17a2 2 0 0 1-2.83 0L2 17.17l-1.58 1.59 4.16-4.16a2 2 0 0 1 0-2.83l7.17-7.17a2 2 0 0 1 2.83 0l1.41 1.41a2 2 0 0 1 0 2.83zm-6.24-1.27a2 2 0 0 0-2.83 0L5 10.59l1.41-1.41a2 2 0 0 0-2.83 0L6.24 3.41a2 2 0 0 0-2.83 0L2 6.59l1.41 1.41a2 2 0 0 0 2.83 0l2.12-2.12a2 2 0 0 1 2.83 0L13.35 7.17l1.41 1.41a2 2 0 0 1 0 2.83l-1.41 1.41z"
}

function iconPath(name) {
  return iconPaths[name] || iconPaths.tag
}

const textValue = computed(() => props.modelValue == null ? "" : String(props.modelValue))
const nativeType = computed(() => props.showPassword && props.type === "password" && !passwordVisible.value ? "password" : props.type)
const isError = computed(() => props.error)
const helperId = computed(() => (props.helperText || props.error) && inputId.value ? inputId.value + "-helper" : undefined)
const showClear = computed(() => props.clearable && !props.disabled && !props.readonly && textValue.value.length > 0)
const showToggle = computed(() => props.showPassword && props.type === "password")
const showFooter = computed(() => !!(props.helperText || props.error))
const clearTitle = "Clear"
const showTitle = "Show password"
const hideTitle = "Hide password"

const wrapperClasses = computed(() => [
  "input",
  `input--${props.size}`,
  {
    "input--error": isError.value,
    "input--disabled": props.disabled,
    "input--focused": isFocused.value,
    "input--readonly": props.readonly
  }
])

function handleInput(evt) {
  const value = evt.target.value
  emit("input", value)
  emit("update:modelValue", value)
}

function handleChange(evt) {
  emit("change", evt.target.value)
}

function handleFocus(evt) {
  isFocused.value = true
  emit("focus", evt)
}

function handleBlur(evt) {
  isFocused.value = false
  emit("blur", evt)
}

async function handleClear() {
  emit("input", "")
  emit("update:modelValue", "")
  emit("clear")
  await nextTick()
  inputRef.value?.focus()
}

function togglePassword() {
  passwordVisible.value = !passwordVisible.value
  emit("toggle-password", passwordVisible.value)
}
</script>

<style scoped>
.input {
  display: flex;
  flex-direction: column;
  width: 100%;
  position: relative;
}

/* ===== Sizes ===== */
.input--sm { --input-h: 32px; --input-px: 10px; --font-size: 12px; --icon-size: 14px; }
.input--md  { --input-h: 40px; --input-px: 12px; --font-size: 13px; --icon-size: 16px; }
.input--lg  { --input-h: 48px; --input-px: 16px; --font-size: 14px; --icon-size: 20px; }

/* ===== Label ===== */
.input__label {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1, 4px);
  margin-bottom: var(--space-1, 4px);
  font-size: var(--font-size-sm, 12px);
  font-weight: 500;
  color: var(--color-ink-muted, #d0d6e0);
  line-height: var(--line-height-normal, 1.4);
}
.input__label--required {
  color: var(--color-error, #ef4444);
}
.input__label-required {
  font-size: var(--font-size-xs, 10px);
  color: var(--color-error, #ef4444);
  line-height: 1;
}

/* ===== Field ===== */
.input__field {
  display: flex;
  align-items: center;
  gap: var(--space-2, 8px);
  width: 100%;
  position: relative;
}

.input__icon {
  display: inline-flex;
  align-items: center;
  height: var(--input-h);
  flex-shrink: 0;
  color: var(--color-ink-tertiary, #62666d);
}
.input__icon-svg {
  width: var(--icon-size);
  height: var(--icon-size);
}

/* ===== Native Input ===== */
.input__native {
  flex: 1 1 0%;
  min-width: 0;
  height: var(--input-h);
  padding: 0 var(--input-px);
  border: 1px solid var(--color-hairline, #23252a);
  border-radius: var(--radius-sm, 6px);
  background: var(--color-surface-1, #0f1011);
  font-family: var(--font-family-sans, var(--font));
  font-size: var(--font-size);
  font-weight: 400;
  line-height: var(--line-height-normal, 1.5);
  color: var(--color-ink, #f7f8f8);
  transition: border-color 150ms ease, box-shadow 150ms ease, background-color 150ms ease, color 150ms ease;
  appearance: none;
  outline: none;
}

.input__native::placeholder {
  color: var(--color-ink-tertiary, #62666d);
}

.input__native:hover:not(:disabled):not([readonly]) {
  border-color: var(--color-hairline-strong, #34343a);
}

.input__native:focus {
  outline: none;
  border-color: var(--color-primary, #5e6ad2);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-primary, #5e6ad2) 40%, transparent);
}

.input__native:disabled,
.input__native[readonly],
.input--disabled .input__native,
.input--readonly .input__native {
  background: var(--color-surface-3, #18191a);
  border-color: var(--color-hairline, #23252a);
  color: var(--color-ink-tertiary, #62666d);
  cursor: not-allowed;
}
.input__native[readonly] { cursor: default; }

.input--error .input__native {
  border-color: var(--color-error, #ef4444);
}
.input--error .input__native:hover:not(:disabled):not([readonly]) {
  border-color: var(--color-error, #ef4444);
}
.input--error .input__native:focus {
  border-color: var(--color-error, #ef4444);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-error, #ef4444) 20%, transparent);
}

.input__native::placeholder {
  color: var(--color-ink-tertiary, #62666d);
}

/* ===== Controls (Clear / Toggle) ===== */
.input__control {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  height: var(--input-h);
  padding: 0 var(--space-2, 8px);
  border: 1px solid transparent;
  border-radius: var(--radius-sm, 6px);
  background: var(--color-surface-1, #0f1011);
  color: var(--color-ink-tertiary, #62666d);
  cursor: pointer;
  transition: background-color 150ms ease, color 150ms ease, box-shadow 150ms ease, transform 100ms ease;
  line-height: 1;
}
.input__control:hover:not(:disabled) {
  background: var(--color-surface-2, #141516);
  color: var(--color-ink, #f7f8f8);
}
.input__control:active:not(:disabled) {
  transform: scale(0.96);
}
.input__control:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}
.input__control:focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-primary, #5e6ad2) 40%, transparent);
}
.input__control .input__icon-svg {
  width: 16px;
  height: 16px;
  color: inherit;
}

/* ===== Icons ===== */
.input__icon {
  display: inline-flex;
  align-items: center;
  height: var(--input-h);
  flex-shrink: 0;
}
.input__icon-svg {
  width: var(--icon-size);
  height: var(--icon-size);
  color: var(--color-ink-tertiary, #62666d);
}

/* ===== Footer / Helper / Error ===== */
.input__footer {
  margin-top: var(--space-1, 4px);
  display: flex;
  align-items: flex-start;
  gap: var(--space-1, 4px);
  font-size: 12px;
  line-height: 1.4;
}
.input__helper {
  color: var(--color-ink-muted, #d0d6e0);
  font-size: 12px;
  line-height: 1.4;
}
.input__helper--error {
  color: var(--color-error, #ef4444);
}

/* ===== States ===== */
.input--focused .input__native:not(:disabled):not([readonly]) {
  border-color: var(--color-primary, #5e6ad2);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-primary, #5e6ad2) 40%, transparent);
}

.input--disabled .input__native,
.input--readonly .input__native {
  background: var(--color-surface-3, #18191a);
  border-color: var(--color-hairline, #23252a);
  color: var(--color-ink-tertiary, #62666d);
  cursor: not-allowed;
}
.input--readonly .input__native { cursor: default; }

/* ===== Reduced Motion ===== */
@media (prefers-reduced-motion: reduce) {
  .input__native,
  .input__control,
  .input__icon-svg {
    transition: none !important;
    animation: none !important;
  }
}

/* ===== Light Mode (Inverse) ===== */
[data-theme="light"] .input__native {
  background: var(--color-inverse-surface-1, #f5f6f6);
  border-color: var(--color-hairline-inverse, #e3e8ee);
  color: var(--color-inverse-ink, #000000);
}
[data-theme="light"] .input__native:hover:not(:disabled):not([readonly]) {
  border-color: var(--color-hairline-inverse, #e3e8ee);
}
[data-theme="light"] .input__native:focus {
  border-color: var(--color-primary, #5e6ad2);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-primary, #5e6ad2) 40%, transparent);
}
[data-theme="light"] .input--error .input__native {
  border-color: var(--color-error, #ef4444);
}
[data-theme="light"] .input--error .input__native:focus {
  border-color: var(--color-error, #ef4444);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-error, #ef4444) 20%, transparent);
}
[data-theme="light"] .input__native:disabled,
[data-theme="light"] .input--disabled .input__native {
  background: var(--color-inverse-surface-2, #f6f7f7);
  border-color: var(--color-hairline-inverse, #e3e8ee);
  color: var(--color-inverse-ink-muted, #64748d);
}
[data-theme="light"] .input__control {
  background: var(--color-inverse-surface-1, #f5f6f6);
  color: var(--color-inverse-ink-muted, #64748d);
}
[data-theme="light"] .input__control:hover:not(:disabled) {
  background: var(--color-inverse-surface-2, #f6f7f7);
  color: var(--color-inverse-ink, #000000);
}
</style>