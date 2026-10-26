<template>
  <nav class="breadcrumb" role="navigation" aria-label="Breadcrumb" :class="wrapClass">
    <ol class="breadcrumb__list" aria-label="Breadcrumb">
      <li v-if="showHome" class="breadcrumb__item breadcrumb__item--home">
        <a
          class="breadcrumb__link breadcrumb__link--home"
          :href="homeRoute"
          @click="handleHomeClick"
          :aria-label="t('ui.breadcrumb.home', 'Home')"
        >
          <Icon name="home" :size="iconSize" aria-hidden="true" />
        </a>
        <span v-if="separator && !isLastItem(0)" class="breadcrumb__separator" aria-hidden="true">{{ separator }}</span>
      </li>

      <template v-for="(segment, index) in segments" :key="segment.key">
        <li
          class="breadcrumb__item"
          :class="{
            'breadcrumb__item--current': segment.isCurrent,
            'breadcrumb__item--has-dropdown': segment.siblings && segment.siblings.length > 0
          }"
        >
          <template v-if="segment.isCurrent">
            <span
              class="breadcrumb__current"
              aria-current="page"
              :title="segment.label"
            >
              {{ segment.label }}
            </span>
          </template>
          <template v-else>
            <a
              class="breadcrumb__link"
              :href="segment.path"
              @click="handleSegmentClick(segment, $event)"
              :title="segment.label"
            >
              {{ segment.label }}
            </a>
          </template>

          <div
            v-if="segment.siblings && segment.siblings.length > 0 && !segment.isCurrent"
            class="breadcrumb__dropdown"
          >
            <button
              type="button"
              class="breadcrumb__dropdown-trigger"
              @click.stop="toggleDropdown(index)"
              @keydown.enter.prevent="toggleDropdown(index)"
              @keydown.space.prevent="toggleDropdown(index)"
              @keydown.esc="closeDropdown(index)"
              @keydown.down="navigateDropdown(index, 1)"
              @keydown.up="navigateDropdown(index, -1)"
              :aria-expanded="openDropdownIndex === index ? 'true' : 'false'"
              :aria-haspopup="'listbox'"
              :aria-label="t('ui.breadcrumb.siblings', 'Sibling routes')"
            >
              <Icon name="arrow-right" :size="12" aria-hidden="true" />
            </button>

            <transition name="breadcrumb-fade">
              <div
                v-if="openDropdownIndex === index"
                class="breadcrumb__dropdown-panel"
                role="listbox"
              >
                <button
                  v-for="(sibling, sIdx) in segment.siblings"
                  :key="sibling.path"
                  type="button"
                  class="breadcrumb__dropdown-option"
                  :class="{ 'breadcrumb__dropdown-option--highlighted': dropdownHighlightedIndex[index] === sIdx }"
                  @click="selectSibling(segment, sibling)"
                  @mouseenter="dropdownHighlightedIndex[index] = sIdx"
                  role="option"
                  :aria-selected="false"
                >
                  {{ sibling.label }}
                </button>
              </div>
            </transition>
          </div>

          <span v-if="separator && !isLastItem(index + (showHome ? 1 : 0))" class="breadcrumb__separator" aria-hidden="true">{{ separator }}</span>
        </li>
      </template>
    </ol>
  </nav>
</template>

<script setup>
import { computed, ref, watch, nextTick } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { t } from '@/i18n'
import Icon from '@/components/Icon.vue'

const props = defineProps({
  routes: {
    type: Array,
    default: () => []
  },
  separator: {
    type: String,
    default: '/'
  },
  showHome: {
    type: Boolean,
    default: true
  },
  homeRoute: {
    type: String,
    default: '/'
  },
  maxItems: {
    type: Number,
    default: 7
  }
})

const emit = defineEmits(['navigate', 'home-click'])

const router = useRouter()
const route = useRoute()

const openDropdownIndex = ref(-1)
const dropdownHighlightedIndex = ref({})
const iconSize = 16

function isLastItem(index) {
  const total = (props.showHome ? 1 : 0) + segments.value.length
  return index === total - 1
}

const segments = computed(() => {
  if (props.routes && props.routes.length > 0) {
    return props.routes.map((r, i) => ({
      key: r.path || `route-${i}`,
      label: r.label || r.name || r.path,
      path: r.path,
      isCurrent: r.isCurrent || false,
      siblings: r.siblings || []
    }))
  }

  const matched = route.matched.filter(m => m.path && m.path !== '/')
  return matched.map((m, i) => {
    const isLast = i === matched.length - 1
    const path = matched.slice(0, i + 1).map(r => r.path).join('')
    const siblings = getSiblingRoutes(m)
    return {
      key: m.path,
      label: getRouteLabel(m),
      path,
      isCurrent: isLast,
      siblings
    }
  })
})

function getRouteLabel(matchedRoute) {
  if (matchedRoute.meta?.breadcrumb) return matchedRoute.meta.breadcrumb
  if (matchedRoute.meta?.title) return matchedRoute.meta.title
  if (matchedRoute.name) return String(matchedRoute.name).replace(/[-_]/g, ' ').replace(/\b\w/g, c => c.toUpperCase())
  return matchedRoute.path.replace(/^\//, '').replace(/[-_]/g, ' ') || 'Home'
}

function getSiblingRoutes(matchedRoute) {
  const parent = route.matched.find(m => m.children?.some(c => c.path === matchedRoute.path))
  if (!parent?.children) return []

  return parent.children
    .filter(c => c.path && c.path !== matchedRoute.path && c.meta?.breadcrumb)
    .map(c => ({
      label: c.meta.breadcrumb,
      path: route.matched.slice(0, route.matched.indexOf(parent) + 1).map(r => r.path).join('') + c.path
    }))
}

const wrapClass = computed(() => [
  'breadcrumb',
  { 'breadcrumb--collapsed': segments.value.length > props.maxItems }
])

function handleHomeClick(event) {
  if (props.homeRoute !== route.path) {
    emit('home-click', event)
    router.push(props.homeRoute)
  }
}

function handleSegmentClick(segment, event) {
  if (!segment.isCurrent) {
    emit('navigate', segment)
    router.push(segment.path)
  }
  event.preventDefault()
}

function toggleDropdown(index) {
  openDropdownIndex.value = openDropdownIndex.value === index ? -1 : index
  if (openDropdownIndex.value === index) {
    dropdownHighlightedIndex.value[index] = 0
    nextTick(() => {
      const panel = document.querySelector('.breadcrumb__dropdown-panel')
      if (panel) panel.scrollTop = 0
    })
  } else {
    delete dropdownHighlightedIndex.value[index]
  }
}

function closeDropdown(index) {
  if (openDropdownIndex.value === index) {
    openDropdownIndex.value = -1
    delete dropdownHighlightedIndex.value[index]
  }
}

function navigateDropdown(breadcrumbIndex, delta) {
  const segment = segments.value[breadcrumbIndex]
  if (!segment?.siblings?.length) return

  const current = dropdownHighlightedIndex.value[breadcrumbIndex] ?? -1
  let next = current + delta
  if (next < 0) next = segment.siblings.length - 1
  if (next >= segment.siblings.length) next = 0
  dropdownHighlightedIndex.value = { ...dropdownHighlightedIndex.value, [breadcrumbIndex]: next }
}

function selectSibling(segment, sibling) {
  emit('navigate', { ...segment, ...sibling })
  router.push(sibling.path)
  closeDropdown(segments.value.indexOf(segment))
}

watch(() => route.fullPath, () => {
  openDropdownIndex.value = -1
  dropdownHighlightedIndex.value = {}
})

document.addEventListener('click', () => {
  openDropdownIndex.value = -1
  dropdownHighlightedIndex.value = {}
}, true)
</script>

<style scoped>
.breadcrumb {
  display: flex;
  align-items: center;
  flex-wrap: nowrap;
  overflow: hidden;
  font-size: var(--font-size-sm, 13px);
  font-family: var(--font-family-sans, inherit);
  color: var(--text-secondary, var(--color-text-secondary));
  background: transparent;
  padding: 0;
  margin: 0;
}

.breadcrumb__list {
  display: inline-flex;
  align-items: center;
  flex-wrap: nowrap;
  list-style: none;
  margin: 0;
  padding: 0;
  gap: 0;
  overflow: hidden;
}

.breadcrumb__item {
  display: inline-flex;
  align-items: center;
  flex-shrink: 0;
  white-space: nowrap;
  position: relative;
}

.breadcrumb__item--home {
  border-radius: var(--radius-sm, 4px) 0 0 var(--radius-sm, 4px);
}

.breadcrumb__link {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1, 4px);
  padding: var(--space-1, 4px) var(--space-2, 8px);
  color: var(--text-secondary, var(--color-text-secondary));
  text-decoration: none;
  border-radius: var(--radius-sm, 4px);
  transition: color var(--transition-fast, 150ms), background-color var(--transition-fast, 150ms);
  white-space: nowrap;
  outline: none;
}

.breadcrumb__link:hover {
  color: var(--color-primary, var(--accent));
  background: var(--color-bg-hover-primary, var(--color-primary-lighter));
  text-decoration: none;
}

.breadcrumb__link:focus-visible {
  outline: none;
  box-shadow: var(--focus-ring, 0 0 0 3px var(--color-primary-100));
  border-radius: var(--radius-sm, 4px);
}

.breadcrumb__link--home {
  padding: var(--space-1, 4px) var(--space-2, 8px) var(--space-1, 4px) var(--space-3, 12px);
  color: var(--text-tertiary, var(--color-text-tertiary));
}

.breadcrumb__link--home:hover {
  color: var(--color-primary, var(--accent));
  background: var(--color-bg-hover-primary, var(--color-primary-lighter));
}

.breadcrumb__current {
  display: inline-flex;
  align-items: center;
  padding: var(--space-1, 4px) var(--space-2, 8px);
  color: var(--text-primary, var(--color-text-primary));
  font-weight: var(--font-weight-medium, 500);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 200px;
}

.breadcrumb__separator {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--text-quaternary, var(--color-text-quaternary));
  padding: 0 var(--space-1, 4px);
  font-size: var(--font-size-sm, 13px);
  user-select: none;
  flex-shrink: 0;
}

.breadcrumb__dropdown {
  position: relative;
  display: inline-flex;
  align-items: center;
}

.breadcrumb__dropdown-trigger {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  padding: 0;
  border: none;
  border-radius: var(--radius-sm, 4px);
  background: transparent;
  color: var(--text-tertiary, var(--color-text-tertiary));
  cursor: pointer;
  transition: color var(--transition-fast, 150ms), background-color var(--transition-fast, 150ms);
  flex-shrink: 0;
  margin-left: var(--space-1, 4px);
}

.breadcrumb__dropdown-trigger:hover {
  color: var(--color-primary, var(--accent));
  background: var(--color-bg-hover-primary, var(--color-primary-lighter));
}

.breadcrumb__dropdown-trigger:focus-visible {
  outline: none;
  box-shadow: var(--focus-ring, 0 0 0 3px var(--color-primary-100));
  border-radius: var(--radius-sm, 4px);
}

.breadcrumb__dropdown-panel {
  position: absolute;
  top: calc(100% + 4px);
  left: 0;
  min-width: 180px;
  max-width: 280px;
  padding: var(--space-1, 4px);
  border: 1px solid var(--border-primary, var(--border));
  border-radius: var(--radius-md, 6px);
  background: var(--surface-1, var(--color-bg-primary));
  box-shadow: var(--shadow-lg, 0 10px 15px -3px rgb(0 0 0 / 0.1), 0 4px 6px -4px rgb(0 0 0 / 0.1));
  z-index: var(--z-dropdown, 300);
  overflow: hidden;
  animation: breadcrumb-fade-in 150ms ease-out;
}

@keyframes breadcrumb-fade-in {
  from {
    opacity: 0;
    transform: translateY(-4px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.breadcrumb__dropdown-option {
  display: flex;
  align-items: center;
  width: 100%;
  padding: var(--space-2, 8px) var(--space-3, 12px);
  border: none;
  border-radius: var(--radius-sm, 4px);
  background: transparent;
  color: var(--text-primary, var(--color-text-primary));
  font-size: var(--font-size-sm, 13px);
  text-align: left;
  cursor: pointer;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  transition: background-color var(--transition-fast, 150ms), color var(--transition-fast, 150ms);
}

.breadcrumb__dropdown-option:hover,
.breadcrumb__dropdown-option--highlighted {
  background: var(--color-bg-hover, var(--color-bg-tertiary));
  color: var(--color-primary, var(--accent));
}

.breadcrumb__dropdown-option:focus-visible {
  outline: none;
  box-shadow: var(--focus-ring, 0 0 0 3px var(--color-primary-100));
  border-radius: var(--radius-sm, 4px);
}

.breadcrumb-fade-enter-active,
.breadcrumb-fade-leave-active {
  transition: opacity 150ms ease, transform 150ms ease;
}

.breadcrumb-fade-enter-from {
  opacity: 0;
  transform: translateY(-4px);
}

.breadcrumb-fade-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

@media (max-width: 768px) {
  .breadcrumb__current {
    max-width: 120px;
  }

  .breadcrumb__dropdown-panel {
    left: auto;
    right: 0;
    min-width: 160px;
  }
}

[data-theme="dark"] .breadcrumb__dropdown-panel {
  box-shadow: 0 10px 15px -3px rgb(0 0 0 / 0.3), 0 4px 6px -4px rgb(0 0 0 / 0.3);
}

@media (prefers-reduced-motion: reduce) {
  .breadcrumb__link,
  .breadcrumb__dropdown-trigger,
  .breadcrumb__dropdown-option,
  .breadcrumb-fade-enter-active,
  .breadcrumb-fade-leave-active {
    transition-duration: 0.01ms !important;
    animation-duration: 0.01ms !important;
  }
}
</style>