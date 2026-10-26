<template>
  <div class="layout" :class="layoutClass">
    <!-- Sidebar -->
    <aside
      v-if="!sidebarHidden"
      ref="sidebarRef"
      class="layout__sidebar"
      :class="sidebarClass"
      :style="sidebarStyle"
      role="navigation"
      aria-label="Main navigation"
      @transitionend="onSidebarTransitionEnd"
    >
      <div class="layout__sidebar-inner" :class="{ 'layout__sidebar-inner--collapsed': sidebarCollapsed }">
        <slot name="sidebar">
          <nav class="layout__nav" aria-label="Sidebar navigation">
            <slot name="nav" />
          </nav>
        </slot>
      </div>
      <!-- Collapse/Expand Toggle -->
      <button
        v-if="showCollapseToggle"
        type="button"
        class="layout__sidebar-toggle"
        :class="{ 'layout__sidebar-toggle--collapsed': sidebarCollapsed }"
        @click="toggleSidebar"
        :aria-label="sidebarCollapsed ? 'Expand sidebar' : 'Collapse sidebar'"
        :aria-expanded="!sidebarCollapsed"
        aria-controls="sidebar-content"
      >
        <svg class="layout__sidebar-toggle-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M15 18l-6-6 6-6" v-if="!sidebarCollapsed" />
          <path d="M9 18l6-6-6-6" v-else />
        </svg>
      </button>
    </aside>

    <!-- Mobile Sidebar Overlay -->
    <div
      v-if="isMobile && mobileSidebarOpen"
      class="layout__sidebar-overlay"
      @click="closeMobileSidebar"
      aria-hidden="true"
    />

    <!-- Main Content Area -->
    <div class="layout__main" :class="mainClass" :style="mainStyle">
      <!-- Header -->
      <header
        v-if="!headerHidden"
        ref="headerRef"
        class="layout__header"
        :class="headerClass"
        :style="headerStyle"
        role="banner"
      >
        <div class="layout__header-inner">
          <!-- Mobile Menu Button -->
          <button
            v-if="isMobile"
            type="button"
            class="layout__mobile-menu-btn"
            @click="openMobileSidebar"
            aria-label="Open navigation menu"
            :aria-expanded="mobileSidebarOpen"
            aria-controls="sidebar-content"
          >
            <svg class="layout__mobile-menu-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <line x1="3" y1="12" x2="21" y2="12" />
              <line x1="3" y1="6" x2="21" y2="6" />
              <line x1="3" y1="18" x2="21" y2="18" />
            </svg>
          </button>

          <!-- Header Slot -->
          <slot name="header">
            <div class="layout__header-default">
              <div class="layout__header-start">
                <slot name="header-start" />
              </div>
              <div class="layout__header-center">
                <slot name="header-center" />
              </div>
              <div class="layout__header-end">
                <slot name="header-end" />
              </div>
            </div>
          </slot>
        </div>
      </header>

      <!-- Main Content -->
      <main
        ref="mainRef"
        class="layout__content"
        :class="contentClass"
        :style="contentStyle"
        role="main"
        aria-label="Main content"
      >
        <slot />
      </main>

      <!-- Footer -->
      <footer
        v-if="!footerHidden"
        ref="footerRef"
        class="layout__footer"
        :class="footerClass"
        :style="footerStyle"
        role="contentinfo"
      >
        <div class="layout__footer-inner">
          <slot name="footer">
            <div class="layout__footer-default">
              <slot name="footer-start" />
              <slot name="footer-center" />
              <slot name="footer-end" />
            </div>
          </slot>
        </div>
      </footer>
    </div>
  </div>
</template>

<script setup>
import { computed, ref, watch, onMounted, onUnmounted } from 'vue'

const props = defineProps({
  sidebarCollapsed: { type: Boolean, default: false },
  sidebarWidth: { type: [String, Number], default: 260 },
  sidebarCollapsedWidth: { type: [String, Number], default: 64 },
  headerHeight: { type: [String, Number], default: 56 },
  footerHeight: { type: [String, Number], default: 48 },
  fixedHeader: { type: Boolean, default: true },
  fixedSidebar: { type: Boolean, default: true },
  fixedFooter: { type: Boolean, default: false },
  sidebarHidden: { type: Boolean, default: false },
  headerHidden: { type: Boolean, default: false },
  footerHidden: { type: Boolean, default: false },
  mobileBreakpoint: { type: Number, default: 1024 },
  showCollapseToggle: { type: Boolean, default: true },
  mobileSidebarOpen: { type: Boolean, default: false },
  transitions: { type: Boolean, default: true },
  headerZIndex: { type: Number, default: 200 },
  sidebarZIndex: { type: Number, default: 150 },
})

const emit = defineEmits([
  'update:sidebarCollapsed',
  'update:mobileSidebarOpen',
  'sidebar-toggle',
  'mobile-sidebar-open',
  'mobile-sidebar-close',
  'resize',
])

const sidebarRef = ref(null)
const headerRef = ref(null)
const mainRef = ref(null)
const footerRef = ref(null)

const isMobile = ref(false)
const mobileSidebarOpen = ref(props.mobileSidebarOpen)
const windowWidth = ref(typeof window !== 'undefined' ? window.innerWidth : 1920)
const sidebarTransitioning = ref(false)

const layoutClass = computed(() => [
  'layout',
  { 'layout--has-sidebar': !props.sidebarHidden },
  { 'layout--has-header': !props.headerHidden },
  { 'layout--has-footer': !props.footerHidden },
  { 'layout--sidebar-collapsed': props.sidebarCollapsed && !props.sidebarHidden },
  { 'layout--mobile': isMobile.value },
  { 'layout--mobile-sidebar-open': mobileSidebarOpen.value },
  { 'layout--fixed-header': props.fixedHeader },
  { 'layout--fixed-sidebar': props.fixedSidebar },
  { 'layout--fixed-footer': props.fixedFooter },
  { 'layout--transitions': props.transitions },
])

const sidebarClass = computed(() => [
  'layout__sidebar',
  `layout__sidebar--${props.fixedSidebar ? 'fixed' : 'absolute'}`,
  { 'layout__sidebar--collapsed': props.sidebarCollapsed },
  { 'layout__sidebar--mobile': isMobile.value },
  { 'layout__sidebar--open': mobileSidebarOpen.value },
])

const sidebarStyle = computed(() => {
  const style = {}
  if (!props.sidebarHidden) {
    style.width = props.sidebarCollapsed
      ? (typeof props.sidebarCollapsedWidth === 'number' ? `${props.sidebarCollapsedWidth}px` : props.sidebarCollapsedWidth)
      : (typeof props.sidebarWidth === 'number' ? `${props.sidebarWidth}px` : props.sidebarWidth)
  }
  if (props.fixedSidebar) {
    style.height = '100vh'
    style.top = props.headerHidden ? '0' : (typeof props.headerHeight === 'number' ? `${props.headerHeight}px` : props.headerHeight)
    if (!props.headerHidden && props.fixedHeader) {
      style.top = (typeof props.headerHeight === 'number' ? `${props.headerHeight}px` : props.headerHeight)
    }
  }
  style.zIndex = props.sidebarZIndex
  return style
})

const mainClass = computed(() => [
  'layout__main',
  { 'layout__main--with-sidebar': !props.sidebarHidden },
  { 'layout__main--sidebar-collapsed': props.sidebarCollapsed && !props.sidebarHidden },
  { 'layout__main--mobile': isMobile.value },
])

const mainStyle = computed(() => {
  const style = {}
  if (!props.sidebarHidden && !isMobile.value) {
    const sidebarWidth = props.sidebarCollapsed
      ? (typeof props.sidebarCollapsedWidth === 'number' ? `${props.sidebarCollapsedWidth}px` : props.sidebarCollapsedWidth)
      : (typeof props.sidebarWidth === 'number' ? `${props.sidebarWidth}px` : props.sidebarWidth)
    style.marginLeft = sidebarWidth
  }
  if (props.fixedHeader && !props.headerHidden) {
    style.paddingTop = (typeof props.headerHeight === 'number' ? `${props.headerHeight}px` : props.headerHeight)
  }
  if (props.fixedFooter && !props.footerHidden) {
    style.paddingBottom = (typeof props.footerHeight === 'number' ? `${props.footerHeight}px` : props.footerHeight)
  }
  return style
})

const headerClass = computed(() => [
  'layout__header',
  `layout__header--${props.fixedHeader ? 'fixed' : 'absolute'}`,
  { 'layout__header--with-sidebar': !props.sidebarHidden },
])

const headerStyle = computed(() => {
  const style = {
    height: typeof props.headerHeight === 'number' ? `${props.headerHeight}px` : props.headerHeight,
    zIndex: props.headerZIndex,
  }
  if (!props.sidebarHidden && !isMobile.value) {
    const sidebarWidth = props.sidebarCollapsed
      ? (typeof props.sidebarCollapsedWidth === 'number' ? `${props.sidebarCollapsedWidth}px` : props.sidebarCollapsedWidth)
      : (typeof props.sidebarWidth === 'number' ? `${props.sidebarWidth}px` : props.sidebarWidth)
    if (props.fixedHeader) {
      style.left = sidebarWidth
    }
  }
  return style
})

const contentClass = computed(() => [
  'layout__content',
  { 'layout__content--scrollable': true },
])

const contentStyle = computed(() => {
  const style = {}
  if (props.fixedHeader && !props.headerHidden && props.fixedFooter && !props.footerHidden) {
    style.height = `calc(100vh - ${typeof props.headerHeight === 'number' ? props.headerHeight + 'px' : props.headerHeight} - ${typeof props.footerHeight === 'number' ? props.footerHeight + 'px' : props.footerHeight})`
    style.overflow = 'auto'
  } else if (props.fixedHeader && !props.headerHidden) {
    style.height = `calc(100vh - ${typeof props.headerHeight === 'number' ? props.headerHeight + 'px' : props.headerHeight})`
    style.overflow = 'auto'
  } else if (props.fixedFooter && !props.footerHidden) {
    style.height = `calc(100vh - ${typeof props.footerHeight === 'number' ? props.footerHeight + 'px' : props.footerHeight})`
    style.overflow = 'auto'
  } else {
    style.minHeight = '100vh'
  }
  return style
})

const footerClass = computed(() => [
  'layout__footer',
  `layout__footer--${props.fixedFooter ? 'fixed' : 'absolute'}`,
  { 'layout__footer--with-sidebar': !props.sidebarHidden },
])

const footerStyle = computed(() => {
  const style = {
    height: typeof props.footerHeight === 'number' ? `${props.footerHeight}px` : props.footerHeight,
  }
  if (!props.sidebarHidden && !isMobile.value && props.fixedFooter) {
    const sidebarWidth = props.sidebarCollapsed
      ? (typeof props.sidebarCollapsedWidth === 'number' ? `${props.sidebarCollapsedWidth}px` : props.sidebarCollapsedWidth)
      : (typeof props.sidebarWidth === 'number' ? `${props.sidebarWidth}px` : props.sidebarWidth)
    style.left = sidebarWidth
  }
  return style
})

function toggleSidebar() {
  const newValue = !props.sidebarCollapsed
  emit('update:sidebarCollapsed', newValue)
  emit('sidebar-toggle', newValue)
}

function openMobileSidebar() {
  mobileSidebarOpen.value = true
  emit('update:mobileSidebarOpen', true)
  emit('mobile-sidebar-open')
  lockBodyScroll()
}

function closeMobileSidebar() {
  mobileSidebarOpen.value = false
  emit('update:mobileSidebarOpen', false)
  emit('mobile-sidebar-close')
  unlockBodyScroll()
}

function onSidebarTransitionEnd() {
  sidebarTransitioning.value = false
  emit('resize')
}

function handleResize() {
  const width = window.innerWidth
  windowWidth.value = width
  const wasMobile = isMobile.value
  isMobile.value = width < props.mobileBreakpoint

  if (wasMobile && !isMobile.value) {
    mobileSidebarOpen.value = false
    emit('update:mobileSidebarOpen', false)
    unlockBodyScroll()
  }

  emit('resize', { width, isMobile: isMobile.value })
}

function lockBodyScroll() {
  const scrollY = window.scrollY
  document.body.style.position = 'fixed'
  document.body.style.top = `-${scrollY}px`
  document.body.style.width = '100%'
  document.body.style.overflow = 'hidden'
}

function unlockBodyScroll() {
  const scrollY = Math.abs(parseInt(document.body.style.top || '0', 10))
  document.body.style.position = ''
  document.body.style.top = ''
  document.body.style.width = ''
  document.body.style.overflow = ''
  window.scrollTo(0, scrollY)
}

function handleKeydown(e) {
  if (e.key === 'Escape' && mobileSidebarOpen.value) {
    closeMobileSidebar()
  }
}

watch(() => props.mobileSidebarOpen, (val) => {
  mobileSidebarOpen.value = val
})

watch(() => props.sidebarCollapsed, () => {
  sidebarTransitioning.value = true
})

onMounted(() => {
  handleResize()
  window.addEventListener('resize', handleResize)
  window.addEventListener('keydown', handleKeydown)
})

onUnmounted(() => {
  window.removeEventListener('resize', handleResize)
  window.removeEventListener('keydown', handleKeydown)
  unlockBodyScroll()
})

defineExpose({
  toggleSidebar,
  openMobileSidebar,
  closeMobileSidebar,
  sidebarRef,
  headerRef,
  mainRef,
  footerRef,
})
</script>

<style scoped>

.layout {
  --layout-sidebar-width: var(--sidebar-width, 260px);
  --layout-sidebar-collapsed-width: var(--sidebar-collapsed-width, 64px);
  --layout-header-height: var(--header-height, 56px);
  --layout-footer-height: var(--footer-height, 48px);
  --layout-sidebar-z: var(--sidebar-z-index, 150);
  --layout-header-z: var(--header-z-index, 200);
  --layout-transition-duration: var(--transition-normal, 150ms);
  --layout-transition-easing: var(--transition-easing, ease);
  display: flex;
  flex-direction: column;
  height: 100vh;
  min-height: 100vh;
  width: 100%;
  background: var(--color-canvas, #010102);
  color: var(--color-ink, #f7f8f8);
  transition: background-color var(--layout-transition-duration) var(--layout-transition-easing);
}

.layout__sidebar {
  position: fixed;
  top: 0;
  left: 0;
  height: 100vh;
  background: var(--color-surface-1, #0f1011);
  border-right: 1px solid var(--color-hairline, #23252a);
  box-shadow: var(--drawer-shadow, -8px 0 30px rgba(28, 35, 64, 0.12));
  display: flex;
  flex-direction: column;
  overflow: hidden;
  transition:
    width var(--layout-transition-duration) var(--layout-transition-easing),
    transform var(--layout-transition-duration) var(--layout-transition-easing),
    box-shadow var(--layout-transition-duration) var(--layout-transition-easing);
}

.layout__sidebar--fixed {
  position: fixed;
}

.layout__sidebar--absolute {
  position: absolute;
}

.layout__sidebar--collapsed {
  width: var(--layout-sidebar-collapsed-width);
}

.layout__sidebar:not(.layout__sidebar--collapsed) {
  width: var(--layout-sidebar-width);
}

.layout__sidebar-inner {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  height: 100%;
  transition: opacity var(--layout-transition-duration) var(--layout-transition-easing);
}

.layout__sidebar-inner--collapsed {
}

.layout__nav {
  flex: 1 1 auto;
  overflow-y: auto;
  padding: var(--space-4);
}

.layout__sidebar-toggle {
  position: absolute;
  right: calc(var(--layout-sidebar-collapsed-width) * -0.5);
  top: 50%;
  transform: translateY(-50%);
  width: 24px;
  height: 24px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  border-radius: var(--radius-full);
  background: var(--color-surface-1, #0f1011);
  color: var(--color-ink-subtle, #8a8f98);
  box-shadow: var(--shadow-md, 0 4px 6px -1px rgb(0 0 0 / 0.1), 0 2px 4px -2px rgb(0 0 0 / 0.1));
  cursor: pointer;
  transition:
    background-color var(--transition-fast),
    color var(--transition-fast),
    box-shadow var(--transition-fast),
    transform var(--transition-fast);
  z-index: 10;
}

.layout__sidebar-toggle:hover {
  background: var(--color-surface-2, #141516);
  color: var(--color-ink, #f7f8f8);
  box-shadow: var(--shadow-lg, 0 10px 15px -3px rgb(0 0 0 / 0.1), 0 4px 6px -4px rgb(0 0 0 / 0.1));
}

.layout__sidebar-toggle:active {
  transform: translateY(-50%) scale(0.95);
}

.layout__sidebar-toggle:focus-visible {
  outline: none;
  box-shadow:
    var(--shadow-md, 0 4px 6px -1px rgb(0 0 0 / 0.1), 0 2px 4px -2px rgb(0 0 0 / 0.1)),
    var(--focus-ring, 0 0 0 3px var(--color-primary-100));
}

[data-theme="dark"] .layout__sidebar-toggle:focus-visible {
  box-shadow:
    var(--shadow-md, 0 4px 6px -1px rgb(0 0 0 / 0.2), 0 2px 4px -2px rgb(0 0 0 / 0.2)),
    var(--focus-ring, 0 0 0 3px var(--color-primary-900));
}

.layout__sidebar-toggle-icon {
  width: 14px;
  height: 14px;
  flex-shrink: 0;
  transition: transform var(--layout-transition-duration) var(--layout-transition-easing);
}

.layout__sidebar-toggle--collapsed .layout__sidebar-toggle-icon {
  transform: rotate(180deg);
}

.layout__sidebar-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.6);
  backdrop-filter: blur(2px);
  -webkit-backdrop-filter: blur(2px);
  z-index: calc(var(--layout-sidebar-z) - 1);
  animation: layout-overlay-fade-in var(--layout-transition-duration) var(--layout-transition-easing);
}

@keyframes layout-overlay-fade-in {
  from { opacity: 0; }
  to { opacity: 1; }
}

@media (max-width: 1024px) {
  .layout__sidebar {
    transform: translateX(-100%);
    z-index: var(--layout-sidebar-z);
  }

  .layout__sidebar--mobile.layout__sidebar--open {
    transform: translateX(0);
    box-shadow: var(--drawer-shadow, -8px 0 30px rgba(28, 35, 64, 0.12));
  }

  .layout__sidebar--mobile .layout__sidebar-toggle {
    display: none;
  }
}

.layout__main {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  min-width: 0;
  transition: margin-left var(--layout-transition-duration) var(--layout-transition-easing);
}

.layout__main--with-sidebar {
}

.layout__header {
  position: sticky;
  top: 0;
  left: 0;
  right: 0;
  height: var(--layout-header-height);
  background: var(--color-surface-1, #0f1011);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  border-bottom: 1px solid var(--color-hairline, #23252a);
  display: flex;
  align-items: center;
  z-index: var(--layout-header-z);
  transition:
    left var(--layout-transition-duration) var(--layout-transition-easing),
    width var(--layout-transition-duration) var(--layout-transition-easing),
    box-shadow var(--layout-transition-duration) var(--layout-transition-easing);
}

.layout__header--fixed {
  position: fixed;
}

.layout__header--absolute {
  position: absolute;
}

.layout__header-inner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  height: 100%;
  padding: 0 var(--space-6);
  gap: var(--space-4);
}

.layout__mobile-menu-btn {
  display: none;
  align-items: center;
  justify-content: center;
  width: 40px;
  height: 40px;
  border: none;
  border-radius: var(--radius-md);
  background: transparent;
  color: var(--color-ink-muted, #d0d6e0);
  cursor: pointer;
  transition: background-color var(--transition-fast), color var(--transition-fast);
}

.layout__mobile-menu-btn:hover {
  background: var(--color-surface-2, #141516);
  color: var(--color-ink, #f7f8f8);
}

.layout__mobile-menu-btn:focus-visible {
  outline: none;
  box-shadow: var(--focus-ring, 0 0 0 3px var(--color-primary-100));
  border-radius: var(--radius-sm);
}

[data-theme="dark"] .layout__mobile-menu-btn:focus-visible {
  box-shadow: var(--focus-ring, 0 0 0 3px var(--color-primary-900));
}

.layout__mobile-menu-icon {
  width: 20px;
  height: 20px;
  flex-shrink: 0;
}

@media (max-width: 1024px) {
  .layout__mobile-menu-btn {
    display: inline-flex;
  }
}

.layout__header-default {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  gap: var(--space-4);
}

.layout__header-start,
.layout__header-center,
.layout__header-end {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.layout__header-start {
  flex: 0 0 auto;
}

.layout__header-center {
  flex: 1 1 auto;
  justify-content: center;
}

.layout__header-end {
  flex: 0 0 auto;
  justify-content: flex-end;
}

.layout__content {
  flex: 1 1 auto;
  width: 100%;
  padding: var(--space-6);
  overflow: auto;
  background: var(--color-canvas, #010102);
}

.layout__content--scrollable {
}

.layout__footer {
  position: sticky;
  bottom: 0;
  left: 0;
  right: 0;
  height: var(--layout-footer-height);
  background: var(--color-surface-1, #0f1011);
  border-top: 1px solid var(--color-hairline, #23252a);
  display: flex;
  align-items: center;
  z-index: 100;
  transition:
    left var(--layout-transition-duration) var(--layout-transition-easing),
    width var(--layout-transition-duration) var(--layout-transition-easing);
}

.layout__footer--fixed {
  position: fixed;
}

.layout__footer--absolute {
  position: absolute;
}

.layout__footer-inner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  height: 100%;
  padding: 0 var(--space-6);
  gap: var(--space-4);
}

.layout__footer-default {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  gap: var(--space-4);
  color: var(--color-ink-subtle, #8a8f98);
  font-size: var(--font-size-sm, 0.875rem);
}

@media (prefers-reduced-motion: reduce) {
  .layout,
  .layout__sidebar,
  .layout__header,
  .layout__footer,
  .layout__main,
  .layout__sidebar-toggle,
  .layout__mobile-menu-btn,
  .layout__sidebar-overlay {
    transition: none !important;
    animation: none !important;
  }

  .layout__sidebar--mobile.layout__sidebar--open {
    transform: translateX(0);
  }
}

@media (prefers-contrast: more) {
  .layout__sidebar {
    border-right-width: 2px;
  }
  .layout__header {
    border-bottom-width: 2px;
  }
  .layout__footer {
    border-top-width: 2px;
  }
  .layout__sidebar-toggle {
    border: 1px solid currentColor;
  }
}

@media print {
  .layout__sidebar,
  .layout__header,
  .layout__footer,
  .layout__mobile-menu-btn,
  .layout__sidebar-toggle,
  .layout__sidebar-overlay {
    display: none !important;
  }

  .layout__main {
    margin-left: 0 !important;
    padding-top: 0 !important;
    padding-bottom: 0 !important;
  }

  .layout__content {
    min-height: auto;
    padding: 0;
  }
}


/* ===== Light Mode (Inverse Canvas) ===== */
[data-theme="light"] .layout {
  background: var(--color-inverse-canvas, #ffffff);
  color: var(--color-inverse-ink, #000000);
}
[data-theme="light"] .layout__sidebar {
  background: var(--color-inverse-surface-1, #f5f6f6);
  border-right-color: var(--color-hairline-inverse, #e3e8ee);
}
[data-theme="light"] .layout__header {
  background: var(--color-inverse-surface-1, #f5f6f6);
  border-bottom-color: var(--color-hairline-inverse, #e3e8ee);
}
[data-theme="light"] .layout__content {
  background: var(--color-inverse-canvas, #ffffff);
}
[data-theme="light"] .layout__footer {
  background: var(--color-inverse-surface-1, #f5f6f6);
  border-top-color: var(--color-hairline-inverse, #e3e8ee);
}
[data-theme="light"] .layout__sidebar-toggle {
  background: var(--color-inverse-surface-1, #f5f6f6);
  color: var(--color-inverse-ink-muted, #64748d);
}
[data-theme="light"] .layout__mobile-menu-btn {
  color: var(--color-inverse-ink-muted, #64748d);
}
[data-theme="light"] .layout__sidebar-overlay {
  background: rgba(0, 0, 0, 0.4);
}

</style>
