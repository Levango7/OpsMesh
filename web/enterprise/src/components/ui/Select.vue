<template>
  <div class="select" :class="wrapperClasses">
    <label
      v-if="label"
      :for="inputId"
      class="select__label"
      :class="{ 'select__label--required': required }"
    >
      {{ label }}
      <span v-if="required" class="select__label-required" aria-hidden="true">*</span>
    </label>

    <div
      class="select__trigger"
      :class="triggerClasses"
      @click="toggleDropdown"
      @keydown.enter.prevent="toggleDropdown"
      @keydown.space.prevent="toggleDropdown"
      @keydown.esc="closeDropdown"
      @keydown.down="navigateOptions(1)"
      @keydown.up="navigateOptions(-1)"
      :aria-expanded="isOpen ? 'true' : 'false'"
      :aria-disabled="isDisabled ? 'true' : 'false'"
      :aria-required="ariaRequired ? 'true' : 'false'"
    >
      <span v-if="leftIcon" class="select__icon select__icon--left">
        <slot name="left-icon">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1-2-2h12a2 2 0 0 1 2 2z" />
          </svg>
        </slot>
      </span>

      <div class="select__value" :class="{ 'select__value--empty': !displayValue }">
        <span v-if="multiple && selectedTags.length" class="select__tags">
          <span
            v-for="(tag, idx) in visibleTags"
            :key="idx"
            class="select__tag"
          >
            {{ tag.label }}
            <button
              type="button"
              class="select__tag-remove"
              @click.stop="removeTag(tag.value)"
              :aria-label="`Remove ${tag.label}`"
            >
              ×
            </button>
          </span>
          <span v-if="hiddenCount > 0" class="select__tag-more">
            +{{ hiddenCount }}
          </span>
        </span>
        <span v-else-if="displayValue" class="select__placeholder">{{ displayValue }}</span>
        <span v-else class="select__placeholder">{{ placeholder }}</span>
      </div>

      <button
        v-if="clearable && !isDisabled && (displayValue || (multiple && selectedTags.length))"
        type="button"
        class="select__clear"
        @click.stop="clearValue"
        :aria-label="t('ui.select.clear', 'Clear')"
      >
        ×
      </button>

      <svg class="select__arrow" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" :class="{ 'select__arrow--open': isOpen }">
        <polyline points="6 9 12 15 18 9" />
      </svg>
    </div>

    <transition name="select-fade">
      <div
        v-if="isOpen"
        class="select__dropdown"
        :class="{ 'select__dropdown--loading': loading }"
      >
        <div v-if="filterable" class="select__search">
          <input
            class="select__search-input"
            type="text"
            :placeholder="t('ui.select.search', 'Search')"
            v-model="searchQuery"
            @input="handleSearch"
            @keydown.esc.stop
            @keydown.down.stop="navigateOptions(1)"
            @keydown.up.stop="navigateOptions(-1)"
            @keydown.enter.stop="selectHighlighted"
            :aria-label="t('ui.select.search', 'Search')"
          />
        </div>

        <div class="select__options" ref="optionsRef">
          <div
            v-if="filteredOptions.length === 0 && !loading"
            class="select__option select__option--empty"
          >
            {{ t('ui.select.noOptions', 'No options') }}
          </div>
          <div
            v-else-if="loading"
            class="select__option select__option--empty"
          >
            {{ t('ui.select.loading', 'Loading') }}
          </div>
          <button
            v-for="(opt, idx) in filteredOptions"
            :key="opt.value + '-' + idx"
            type="button"
            class="select__option"
            :class="{
              'select__option--active': highlightedIndex === idx,
              'select__option--disabled': opt.disabled,
              'select__option--selected': isSelected(opt)
            }"
            @click="() => selectOption(opt)"
            @mouseenter="highlightedIndex = idx"
            :disabled="opt.disabled"
            :aria-selected="isSelected(opt) ? 'true' : 'false'"
          >
            <span class="select__option-label">{{ opt.label }}</span>
            <span v-if="isSelected(opt)" class="select__option-check">✓</span>
          </button>
        </div>
      </div>
    </transition>

    <div v-if="helperText" class="select__helper">{{ helperText }}</div>
    <div v-if="error" class="select__error">{{ error }}</div>
  </div>
</template>

<script setup>
import { computed, ref, watch, nextTick } from "vue"
import { t } from "@/i18n"

const props = defineProps({
  modelValue: { type: [String, Number, Boolean, Array], default: () => [] },
  options: { type: Array, default: () => [] },
  placeholder: { type: String, default: "Select" },
  disabled: { type: Boolean, default: false },
  clearable: { type: Boolean, default: false },
  filterable: { type: Boolean, default: false },
  size: { type: String, default: "md" },
  error: { type: [String, Boolean], default: "" },
  label: { type: String, default: "" },
  helperText: { type: String, default: "" },
  multiple: { type: Boolean, default: false },
  maxTags: { type: Number, default: 3 },
  loading: { type: Boolean, default: false },
  required: { type: Boolean, default: false },
  leftIcon: { type: String, default: "" }
})

const emit = defineEmits(["update:modelValue", "change", "focus", "blur", "clear", "visible-change", "remove-tag"])

const isOpen = ref(false)
const searchQuery = ref("")
const highlightedIndex = ref(-1)
const optionsRef = ref(null)
const inputId = `select-${Math.random().toString(36).slice(2)}`

const isDisabled = computed(() => props.disabled || props.loading)
const ariaRequired = computed(() => props.required ? "true" : "false")

const valueArray = computed(() => {
  if (!props.modelValue) return []
  return Array.isArray(props.modelValue) ? props.modelValue : [props.modelValue]
})

const selectedTags = computed(() => {
  return props.options.filter(opt => valueArray.value.includes(opt.value))
})

const visibleTags = computed(() => selectedTags.value.slice(0, props.maxTags))
const hiddenCount = computed(() => Math.max(0, selectedTags.value.length - props.maxTags))

const displayValue = computed(() => {
  if (props.multiple) return ""
  const val = valueArray.value[0]
  if (val == null) return ""
  const opt = props.options.find(o => o.value === val)
  return opt ? opt.label : String(val)
})

const wrapperClasses = computed(() => [
  "select",
  `select--${props.size}`,
  {
    "select--error": !!props.error,
    "select--disabled": isDisabled.value,
    "select--open": isOpen.value
  }
])

const triggerClasses = computed(() => [
  "select__trigger",
  { "select__trigger--focus": isOpen.value }
])

const filteredOptions = computed(() => {
  if (!props.filterable) return props.options
  const q = searchQuery.value.toLowerCase().trim()
  if (!q) return props.options
  return props.options.filter(opt => String(opt.label).toLowerCase().includes(q))
})

function toggleDropdown() {
  if (isDisabled.value) return
  isOpen.value = !isOpen.value
  if (isOpen.value) {
    highlightedIndex.value = -1
    searchQuery.value = ""
    emit("visible-change", true)
  } else {
    emit("visible-change", false)
  }
}

function closeDropdown() {
  if (!isOpen.value) return
  isOpen.value = false
  searchQuery.value = ""
  highlightedIndex.value = -1
  emit("visible-change", false)
}

function navigateOptions(delta) {
  if (!isOpen.value) return
  const len = filteredOptions.value.length
  if (len === 0) return
  highlightedIndex.value += delta
  if (highlightedIndex.value < 0) highlightedIndex.value = len - 1
  if (highlightedIndex.value >= len) highlightedIndex.value = 0
  scrollToHighlighted()
}

function scrollToHighlighted() {
  nextTick(() => {
    const active = optionsRef.value?.querySelector(".select__option--active")
    if (active) active.scrollIntoView({ block: "nearest" })
  })
}

function selectHighlighted() {
  if (highlightedIndex.value >= 0 && highlightedIndex.value < filteredOptions.value.length) {
    selectOption(filteredOptions.value[highlightedIndex.value])
  }
}

function selectOption(opt) {
  if (opt.disabled || !isOpen.value) return
  if (props.multiple) {
    const vals = [...valueArray.value]
    const idx = vals.indexOf(opt.value)
    if (idx >= 0) vals.splice(idx, 1)
    else vals.push(opt.value)
    emit("update:modelValue", vals)
  } else {
    emit("update:modelValue", opt.value)
    closeDropdown()
  }
  emit("change", props.modelValue)
}

function isSelected(opt) {
  return valueArray.value.includes(opt.value)
}

function removeTag(val) {
  const vals = [...valueArray.value]
  const idx = vals.indexOf(val)
  if (idx >= 0) vals.splice(idx, 1)
  emit("update:modelValue", vals)
  emit("remove-tag", val)
  emit("change", vals)
}

function clearValue() {
  emit("update:modelValue", props.multiple ? [] : null)
  emit("clear")
  emit("change", props.multiple ? [] : null)
  closeDropdown()
}

function handleSearch() {
  highlightedIndex.value = -1
}

watch(() => props.modelValue, () => {
  if (isOpen.value && !props.multiple) closeDropdown()
})
</script>

<style scoped>
.select {
  display: inline-flex;
  flex-direction: column;
  width: 100%;
  position: relative;
  font-family: var(--font-family-sans, var(--font));
}

/* ===== Label ===== */
.select__label {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1, 4px);
  margin-bottom: var(--space-1, 4px);
  font-size: var(--font-size-sm, 12px);
  font-weight: 500;
  color: var(--color-ink-muted, #d0d6e0);
  line-height: 1.4;
}
.select__label--required {
  color: var(--color-error, #ef4444);
}
.select__label-required {
  font-size: var(--font-size-xs, 10px);
  color: var(--color-error, #ef4444);
  line-height: 1;
}

/* ===== Trigger ===== */
.select__trigger {
  display: inline-flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  padding: 0 var(--space-3, 12px);
  border: 1px solid var(--color-hairline, #23252a);
  border-radius: var(--radius-sm, 6px);
  background: var(--color-surface-1, #0f1011);
  color: var(--color-ink, #f7f8f8);
  font-size: 13px;
  font-weight: 400;
  line-height: 1.5;
  cursor: pointer;
  outline: none;
  transition: border-color 150ms ease, box-shadow 150ms ease, background-color 150ms ease, color 150ms ease;
  flex-shrink: 0;
}

/* Sizes */
.select--sm .select__trigger {
  height: 32px;
  padding: 0 var(--space-2, 8px);
  font-size: 12px;
}

.select--md .select__trigger {
  height: 40px;
  padding: 0 var(--space-3, 12px);
  font-size: 13px;
}

.select--lg .select__trigger {
  height: 48px;
  padding: 0 var(--space-3, 12px);
  font-size: 14px;
}

/* Focus */
.select__trigger--focus {
  border-color: var(--color-primary, #5e6ad2);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-primary, #5e6ad2) 40%, transparent);
}

.select__trigger:hover:not(.select__trigger--disabled):not(.select__trigger--focus) {
  border-color: var(--color-hairline-strong, #34343a);
}

.select--disabled .select__trigger,
.select__trigger[aria-disabled="true"] {
  background: var(--color-surface-3, #18191a);
  border-color: var(--color-hairline, #23252a);
  color: var(--color-ink-tertiary, #62666d);
  cursor: not-allowed;
}

/* ===== Value / Tags ===== */
.select__value {
  display: flex;
  align-items: center;
  flex: 1;
  min-width: 0;
}

.select__value--empty {
  opacity: 0.6;
}

.select__placeholder {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--color-ink-tertiary, #62666d);
  font-size: inherit;
}

.select__tags {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-1, 4px);
  align-items: center;
  min-width: 0;
}

.select__tag {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 2px 8px;
  border-radius: var(--radius-pill-sm, 64px);
  background: color-mix(in srgb, var(--color-primary, #5e6ad2) 15%, transparent);
  color: var(--color-primary, #5e6ad2);
  font-size: 11px;
  font-weight: 500;
  white-space: nowrap;
}

.select__tag-remove {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 16px;
  border: none;
  background: transparent;
  color: currentColor;
  opacity: 0.7;
  cursor: pointer;
  border-radius: 50%;
  line-height: 1;
  padding: 0;
}
.select__tag-remove:hover {
  opacity: 1;
  background: rgba(0, 0, 0, 0.08);
}

.select__tag-more {
  padding: 2px 8px;
  font-size: 11px;
  color: var(--color-ink-tertiary, #62666d);
  background: var(--color-surface-2, #141516);
  border-radius: var(--radius-pill-sm, 64px);
}

/* ===== Icon / Clear / Arrow ===== */
.select__icon {
  display: inline-flex;
  align-items: center;
  flex-shrink: 0;
  color: var(--color-ink-tertiary, #62666d);
}
.select__icon-svg {
  width: 16px;
  height: 16px;
}
.select__icon--left {
  margin-right: var(--space-2, 8px);
}

.select__clear {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border: none;
  border-radius: 50%;
  background: transparent;
  color: var(--color-ink-tertiary, #62666d);
  font-size: 16px;
  line-height: 1;
  cursor: pointer;
  opacity: 0.7;
  flex-shrink: 0;
  margin-left: var(--space-1, 4px);
}
.select__clear:hover {
  background: rgba(0, 0, 0, 0.08);
  opacity: 1;
}

.select__arrow {
  width: 16px;
  height: 16px;
  flex-shrink: 0;
  color: var(--color-ink-tertiary, #62666d);
  transition: transform 150ms ease;
}
.select__arrow--open {
  transform: rotate(180deg);
}

/* ===== Dropdown ===== */
.select__dropdown {
  position: absolute;
  top: 100%;
  left: 0;
  right: 0;
  margin-top: 4px;
  border: 1px solid var(--color-hairline, #23252a);
  border-radius: var(--radius-md, 8px);
  background: var(--color-surface-1, #0f1011);
  box-shadow: var(--shadow-xl, 0 16px 48px rgba(0, 0, 0, 0.6));
  z-index: 1000;
  max-height: 260px;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.select__search {
  padding: var(--space-1, 4px) var(--space-2, 8px);
  border-bottom: 1px solid var(--color-hairline, #23252a);
}

.select__search-input {
  width: 100%;
  height: 28px;
  padding: 0 10px;
  border: 1px solid var(--color-hairline, #23252a);
  border-radius: var(--radius-sm, 6px);
  background: var(--color-surface-1, #0f1011);
  color: var(--color-ink, #f7f8f8);
  font-size: 13px;
  font-family: inherit;
  outline: none;
}
.select__search-input:focus {
  border-color: var(--color-primary, #5e6ad2);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-primary, #5e6ad2) 40%, transparent);
}

.select__options {
  overflow-y: auto;
  max-height: calc(100% - 36px);
}

.select__option {
  display: flex;
  align-items: center;
  padding: 8px var(--space-3, 12px);
  color: var(--color-ink, #f7f8f8);
  font-size: 13px;
  cursor: pointer;
  text-align: left;
  background: transparent;
  border: none;
  outline: none;
  width: 100%;
  text-align: left;
}

.select__option:hover:not(.select__option--disabled),
.select__option--active:not(.select__option--disabled) {
  background: color-mix(in srgb, var(--color-primary, #5e6ad2) 15%, transparent);
  color: var(--color-primary, #5e6ad2);
}

.select__option--selected {
  color: var(--color-primary, #5e6ad2);
  font-weight: 500;
}

.select__option--selected .select__option-check {
  display: inline-block;
}

.select__option--disabled {
  color: var(--color-ink-tertiary, #62666d);
  cursor: not-allowed;
  opacity: 0.5;
}

.select__option--empty {
  padding: 8px var(--space-3, 12px);
  color: var(--color-ink-tertiary, #62666d);
  font-size: 13px;
  background: var(--color-surface-2, #141516);
}

.select__option-label {
  flex: 1;
  min-width: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.select__option-check {
  display: none;
  color: var(--color-primary, #5e6ad2);
  font-weight: bold;
}

/* ===== Footer / Helper / Error ===== */
.select__helper {
  margin-top: var(--space-1, 4px);
  font-size: 12px;
  color: var(--color-ink-muted, #d0d6e0);
  line-height: 1.4;
}
.select__error {
  margin-top: var(--space-1, 4px);
  font-size: 12px;
  color: var(--color-error, #ef4444);
  line-height: 1.4;
}

/* ===== Animations ===== */
.select-fade-enter-active, .select-fade-leave-active {
  transition: opacity 150ms ease, transform 150ms ease;
}
.select-fade-enter-from {
  opacity: 0;
  transform: translateY(-6px);
}
.select-fade-leave-to {
  opacity: 0;
  transform: translateY(-6px);
}

/* ===== Reduced Motion ===== */
@media (prefers-reduced-motion: reduce) {
  .select__dropdown,
  .select__trigger,
  .select__arrow,
  .select__control,
  .select__tag,
  .select__option {
    transition: none !important;
    animation: none !important;
  }
}

/* ===== Light Mode (Inverse) ===== */
[data-theme="light"] .select__trigger {
  background: var(--color-inverse-surface-1, #f5f6f6);
  border-color: var(--color-hairline-inverse, #e3e8ee);
  color: var(--color-inverse-ink, #000000);
}
[data-theme="light"] .select__trigger:hover:not(.select__trigger--disabled):not(.select__trigger--focus) {
  border-color: var(--color-hairline-inverse-strong, #cbd5e1);
}
[data-theme="light"] .select__trigger--focus {
  border-color: var(--color-primary, #5e6ad2);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-primary, #5e6ad2) 40%, transparent);
}
[data-theme="light"] .select__dropdown {
  background: var(--color-inverse-surface-1, #f5f6f6);
  border-color: var(--color-hairline-inverse, #e3e8ee);
  box-shadow: var(--shadow-xl, 0 16px 48px rgba(0, 0, 0, 0.1));
}
[data-theme="light"] .select__search-input {
  background: var(--color-inverse-surface-1, #f5f6f6);
  border-color: var(--color-hairline-inverse, #e3e8ee);
  color: var(--color-inverse-ink, #000000);
}
[data-theme="light"] .select__search-input:focus {
  border-color: var(--color-primary, #5e6ad2);
}
[data-theme="light"] .select__option {
  color: var(--color-inverse-ink, #000000);
}
[data-theme="light"] .select__option--empty {
  background: var(--color-inverse-surface-2, #f6f7f7);
}
[data-theme="light"] .select__tag {
  background: color-mix(in srgb, var(--color-primary, #5e6ad2) 15%, #ffffff);
  color: var(--color-primary, #5e6ad2);
}
</style>