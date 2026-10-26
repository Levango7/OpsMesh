<template>
  <div class="form" :class="wrapClasses">
    <slot />
  </div>
</template>

<script setup>
import { computed } from "vue"

const props = defineProps({
  labelWidth: { type: [String, Number], default: 100 },
  labelAlign: { type: String, default: "right" },
  model: { type: Object, default: () => ({}) },
  rules: { type: Object, default: () => ({}) },
  size: { type: String, default: "md" },
  showLabel: { type: Boolean, default: true },
  disabled: { type: Boolean, default: false }
})

const wrapClasses = computed(() => [
  "form",
  `form--${props.size}`,
  {
    "form--label-right": props.labelAlign === "right",
    "form--label-left": props.labelAlign === "left",
    "form--label-top": props.labelAlign === "top",
    "form--disabled": props.disabled
  }
])
</script>

<style scoped>
.form {
  display: flex;
  flex-direction: column;
  gap: var(--space-4, 16px);
  width: 100%;
}

.form--label-right .form-item__label {
  text-align: right;
}

.form--label-left .form-item__label {
  text-align: left;
}

.form--label-top .form-item__label {
  text-align: left;
  margin-bottom: var(--space-1, 4px);
}
</style>