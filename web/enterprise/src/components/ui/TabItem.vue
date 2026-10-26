<template>
  <span style="display:none" aria-hidden="true" />
</template>

<script setup>
import { inject, watch as tabWatch, onUnmounted as tabUnmount } from "vue"
import { TabsSymbol } from "./tabs-symbol"

const props = defineProps({
  name: { type: String, required: true },
  label: { type: String, default: "" },
  disabled: { type: Boolean, default: false },
  badge: { type: [String, Number], default: "" }
})

const tabs = inject(TabsSymbol)

const entry = {
  name: props.name,
  label: props.label,
  disabled: props.disabled,
  badge: props.badge
}

tabs.register(entry)

tabWatch(() => props.label, (value) => { entry.label = value })
tabWatch(() => props.disabled, (value) => { entry.disabled = value })
tabWatch(() => props.badge, (value) => { entry.badge = value })

tabUnmount(() => tabs.unregister(entry))
</script>