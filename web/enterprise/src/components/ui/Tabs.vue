<template>
  <div class="tabs" :class="wrapClasses">
    <div class="tabs__nav" ref="navRef">
      <button
        v-for="tab in tabItems"
        :key="tab.name"
        type="button"
        class="tabs__tab"
        :class="[
          'tabs__tab--' + type,
          { 'tabs__tab--active': tab.name === activeName },
          { 'tabs__tab--disabled': tab.disabled }
        ]"
        :data-tab-name="tab.name"
        :aria-selected="tab.name === activeName ? 'true' : 'false'"
        :aria-disabled="tab.disabled ? 'true' : 'false'"
        @click="tab.disabled ? null : onTabClick(tab.name)"
        @keydown.enter="tab.disabled ? null : onTabClick(tab.name)"
        @keydown.space.prevent="tab.disabled ? null : onTabClick(tab.name)"
      >
        {{ tab.label }}
        <span v-if="tab.badge" class="tabs__tab-badge">{{ tab.badge }}</span>
      </button>
    </div>

    <span
      v-if="animated && activeTab"
      ref="inkRef"
      class="tabs__ink"
      :style="inkStyle"
      aria-hidden="true"
    />

    <div class="tabs__panels" ref="panelsRef">
      <transition name="tab-panel-fade">
        <div
          v-if="activePanel"
          class="tabs__panel"
          :class="{ 'tabs__panel--card': type === 'card' }"
          :aria-hidden="!activePanel"
        >
          <slot :name="activePanel" />
        </div>
      </transition>
    </div>
  </div>
</template>

<script setup>
import { computed, ref, onMounted, onUnmounted, nextTick, watch, provide } from "vue"
import { TabsSymbol } from "./tabs-symbol"

const props = defineProps({
  modelValue: { type: String, default: "" },
  type: { type: String, default: "line", validator: v => ["line", "card"].includes(v) },
  animated: { type: Boolean, default: true }
})

const emit = defineEmits(["update:modelValue", "tab-change"])

const navRef = ref(null)
const panelsRef = ref(null)
const inkRef = ref(null)

let uidSeq = 0
const tabItems = ref([])

const activeName = computed(() => props.modelValue)
const activeTab = computed(() => tabItems.value.find(tab => tab.name === activeName.value))
const activePanel = computed(() => activeTab.value ? activeTab.value.name : "")

const inkRect = ref({ left: 0, width: 0 })

const inkStyle = computed(() => {
  if (!props.animated || !activeTab.value) {
    return { display: "none" }
  }
  return { left: `${inkRect.value.left}px`, width: `${inkRect.value.width}px` }
})

function registerTab(entry) {
  entry.uid = ++uidSeq
  tabItems.value.push(entry)
}

function unregisterTab(entry) {
  const index = tabItems.value.indexOf(entry)
  if (index >= 0) {
    tabItems.value.splice(index, 1)
  }
}

function onTabClick(name) {
  emit("update:modelValue", name)
  emit("tab-change", name)
}

function updateInk() {
  if (!props.animated || !navRef.value || !activeTab.value) {
    inkRect.value = { left: 0, width: 0 }
    return
  }
  const btn = navRef.value.querySelector(`[data-tab-name="${activeTab.value.name}"]`)
  if (!btn) {
    inkRect.value = { left: 0, width: 0 }
    return
  }
  const nav = navRef.value
  inkRect.value = { left: btn.offsetLeft - nav.offsetLeft, width: btn.offsetWidth }
}

let resizeListener = null
function startResizeListener() {
  if (resizeListener) return
  resizeListener = () => updateInk()
  window.addEventListener("resize", resizeListener)
}

function stopResizeListener() {
  if (resizeListener) {
    window.removeEventListener("resize", resizeListener)
    resizeListener = null
  }
}

provide(TabsSymbol, {
  active: activeName,
  register: registerTab,
  unregister: unregisterTab,
  type: props.type,
  animated: props.animated
})

const wrapClasses = computed(() => [
  "tabs",
  `tabs--${props.type}`,
  { "tabs--animated": props.animated }
])

onMounted(() => {
  nextTick(updateInk)
  startResizeListener()
})

onUnmounted(() => {
  stopResizeListener()
})

watch(activeTab, () => {
  nextTick(updateInk)
})
</script>

<style scoped>
.tabs {
  display: flex;
  flex-direction: column;
  width: 100%;
  font-family: var(--font-family-sans, var(--font));
}

.tabs__nav {
  display: flex;
  align-items: center;
  gap: var(--space-1, 4px);
  overflow-x: auto;
  overflow-y: hidden;
  scrollbar-width: none;
  position: relative;
  flex-shrink: 0;
}
.tabs__nav::-webkit-scrollbar {
  display: none;
}

.tabs__tab {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 0;
  padding: var(--space-2, 8px) var(--space-4, 16px);
  border: none;
  border-radius: var(--radius-md, 8px);
  background: transparent;
  color: var(--color-ink-tertiary, #62666d);
  font-size: var(--font-size-sm, 13px);
  font-weight: var(--font-weight-medium, 500);
  line-height: 1;
  white-space: nowrap;
  cursor: pointer;
  transition: color 150ms ease, background-color 150ms ease, border-color 150ms ease, box-shadow 150ms ease;
  outline: none;
}

.tabs__tab:focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-primary, #5e6ad2) 40%, transparent);
  border-radius: var(--radius-sm, 6px);
}

.tabs__tab:hover:not(.tabs__tab--disabled) {
  color: var(--color-ink, #f7f8f8);
  background-color: var(--color-surface-2, #141516);
}

.tabs__tab:active:not(.tabs__tab--disabled) {
  transform: scale(0.96);
}

.tabs__tab--disabled {
  opacity: 0.6;
  cursor: not-allowed;
  pointer-events: none;
}

.tabs__tab-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 16px;
  height: 16px;
  padding: 0 4px;
  margin-left: var(--space-1, 4px);
  border-radius: var(--radius-pill-sm, 64px);
  background: var(--color-primary, #5e6ad2);
  color: #ffffff;
  font-size: 10px;
  font-weight: 700;
  line-height: 1;
}

.tabs__ink {
  position: absolute;
  bottom: 0;
  height: 2px;
  border-radius: var(--radius-full, 9999px);
  background: var(--color-primary, #5e6ad2);
  pointer-events: none;
  transition: left 150ms ease, width 150ms ease;
}

/* Line type */
.tabs--line .tabs__tab--active {
  color: var(--color-primary, #5e6ad2);
  box-shadow: inset 0 -2px 0 var(--color-primary, #5e6ad2);
}

/* Card type */
.tabs--card .tabs__nav {
  background: var(--color-surface-2, #141516);
  border-radius: var(--radius-lg, 12px);
  padding: var(--space-1, 4px);
}

.tabs--card .tabs__tab {
  border-radius: var(--radius-md, 8px);
}

.tabs--card .tabs__tab--active {
  background: var(--color-surface-1, #0f1011);
  color: var(--color-primary, #5e6ad2);
  box-shadow: var(--shadow-sm, 0 1px 2px rgba(0, 0, 0, 0.3));
  border: 1px solid var(--color-hairline, #23252a);
}

/* Panels */
.tabs__panels {
  position: relative;
}

.tabs__panel {
  width: 100%;
  overflow: auto;
  outline: none;
}

.tabs__panel--card {
  border: 1px solid var(--color-hairline, #23252a);
  border-radius: var(--radius-lg, 12px);
  background: var(--color-surface-1, #0f1011);
  padding: var(--space-6, 24px);
  margin-top: var(--space-4, 16px);
}

/* Animations */
@keyframes tab-panel-fade-in {
  from { opacity: 0; transform: translateY(4px); }
  to { opacity: 1; transform: translateY(0); }
}

@keyframes tab-panel-fade-out {
  from { opacity: 1; transform: translateY(0); }
  to { opacity: 0; transform: translateY(4px); }
}

.tab-panel-fade-enter-active { animation: tab-panel-fade-in var(--transition-normal, 0.15s) ease-out both; }
.tab-panel-fade-leave-active { animation: tab-panel-fade-out var(--transition-normal, 0.15s) ease-in both; }

/* Light mode */
[data-theme="light"] .tabs--card .tabs__nav {
  background: var(--color-inverse-surface-1, #f5f6f6);
}

[data-theme="light"] .tabs--card .tabs__tab--active {
  border-color: var(--color-hairline-inverse, #e3e8ee);
  box-shadow: var(--shadow-sm, 0 1px 2px rgba(0, 0, 0, 0.05));
}

[data-theme="light"] .tabs__tab {
  color: var(--color-inverse-ink-muted, #64748d);
}

[data-theme="light"] .tabs__tab:hover:not(.tabs__tab--disabled) {
  color: var(--color-inverse-ink, #000000);
  background-color: var(--color-inverse-surface-2, #f6f7f7);
}

/* Reduced motion */
@media (prefers-reduced-motion: reduce) {
  .tabs__tab,
  .tabs__ink,
  .tab-panel-fade-enter-active,
  .tab-panel-fade-leave-active {
    transition: none !important;
    animation: none !important;
  }
}
</style>