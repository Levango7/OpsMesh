<template>
  <nav
    ref="sidebarRef"
    class="sidebar"
    :class="sidebarClass"
    role="navigation"
    aria-orientation="vertical"
    :aria-label="ariaLabel"
    @keydown="handleKeydown"
  >
    <div class="sidebar__scroll" ref="scrollRef">
      <ul class="sidebar__list" role="list">
        <SidebarGroup
          v-for="(group, groupIndex) in items"
          :key="group.label + '-' + groupIndex"
          :group="group"
          :group-index="groupIndex"
          :collapsed="collapsed"
          :accordion="accordion"
          :active-path="activePath"
          :router="router"
          @select="handleGroupSelect"
        />
      </ul>
    </div>

    <button
      v-if="collapsible"
      type="button"
      class="sidebar__toggle"
      :class="{ 'sidebar__toggle--collapsed': collapsed }"
      @click="toggleCollapse"
      :aria-expanded="!collapsed"
      :aria-label="collapsed ? 'Expand sidebar' : 'Collapse sidebar'"
      title=""
    >
      <svg class="sidebar__toggle-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <polyline :points="collapsed ? '9 18 15 12 9 6' : '15 18 9 12 15 6'" />
      </svg>
    </button>
  </nav>

  <Teleport to="body">
    <Transition name="sidebar-tooltip">
      <div
        v-if="collapsed && tooltipVisible && tooltipContent"
        class="sidebar__tooltip"
        :style="tooltipStyle"
        role="tooltip"
        aria-hidden="true"
      >
        {{ tooltipContent }}
      </div>
    </Transition>
  </Teleport>
</template>

<script setup>
import { computed, ref, watch, onMounted, provide, inject } from 'vue'
import { useRouter, useRoute } from 'vue-router'

// SidebarGroup component for nested rendering
const SidebarGroup = {
  name: 'SidebarGroup',
  components: { SidebarItem },
  props: {
    group: { type: Object, required: true },
    groupIndex: { type: Number, required: true },
    collapsed: { type: Boolean, default: false },
    accordion: { type: Boolean, default: true },
    activePath: { type: String, default: '' },
    router: { type: Object, default: null }
  },
  emits: ['select'],
  setup(props, { emit }) {
    const expandedGroups = ref(new Set())
    const focusedIndex = ref(-1)
    const tooltipTimer = ref(null)
    const parentSidebar = inject('sidebar-context', null)

    const isGroupExpanded = computed(() => expandedGroups.value.has(props.group.label))

    function toggleGroup(groupLabel) {
      if (!props.group.children || props.group.children.length === 0) return
      if (props.accordion) {
        expandedGroups.value.clear()
      }
      if (isGroupExpanded.value) {
        expandedGroups.value.delete(groupLabel)
      } else {
        expandedGroups.value.add(groupLabel)
      }
    }

    function isItemActive(item) {
      if (!props.activePath) return false
      if (item.route && props.activePath.startsWith(item.route)) return true
      if (item.children) {
        return item.children.some(child => isItemActive(child))
      }
      return false
    }

    function isGroupActive() {
      if (!props.group.children) return false
      return props.group.children.some(item => isItemActive(item))
    }

    function handleItemClick(item, _event) {
      if (item.disabled) return
      if (item.children && item.children.length > 0) {
        toggleGroup(props.group.label)
        return
      }
      emit('select', item)
      if (props.router && item.route) {
        props.router.push(item.route).catch(() => {})
      }
    }

    function handleKeyDown(_event, item, itemIndex, isChild) {
      const items = isChild ? props.group.children : [props.group].flatMap(g => g.children || [])
      const maxIndex = items.length - 1

      switch (event.key) {
        case 'ArrowDown':
          event.preventDefault()
          focusedIndex.value = Math.min(focusedIndex.value + 1, maxIndex)
          break
        case 'ArrowUp':
          event.preventDefault()
          focusedIndex.value = Math.max(focusedIndex.value - 1, 0)
          break
        case 'ArrowRight':
          event.preventDefault()
          if (item.children && item.children.length > 0 && !isGroupExpanded.value) {
            toggleGroup(props.group.label)
          }
          break
        case 'ArrowLeft':
          event.preventDefault()
          if (item.children && item.children.length > 0 && isGroupExpanded.value) {
            toggleGroup(props.group.label)
          }
          break
        case 'Enter':
        case ' ':
          event.preventDefault()
          handleItemClick(item, event)
          break
        case 'Escape':
          if (isGroupExpanded.value) {
            toggleGroup(props.group.label)
          } else if (parentSidebar) {
            parentSidebar.blur()
          }
          break
        case 'Home':
          event.preventDefault()
          focusedIndex.value = 0
          break
        case 'End':
          event.preventDefault()
          focusedIndex.value = maxIndex
          break
      }
    }

    function showTooltip(item, event) {
      if (!props.collapsed) return
      tooltipTimer.value = setTimeout(() => {
        if (parentSidebar) {
          parentSidebar.showTooltip(item.label, event.target.getBoundingClientRect())
        }
      }, 300)
    }

    function hideTooltip() {
      if (tooltipTimer.value) {
        clearTimeout(tooltipTimer.value)
        tooltipTimer.value = null
      }
      if (parentSidebar) {
        parentSidebar.hideTooltip()
      }
    }

    return {
      isGroupExpanded,
      isItemActive,
      isGroupActive,
      toggleGroup,
      handleItemClick,
      handleKeyDown,
      showTooltip,
      hideTooltip,
      focusedIndex
    }
  },
  template: `
    <li class="sidebar__group" :class="{ 'sidebar__group--active': isGroupActive() }">
      <template v-if="group.label">
        <div
          class="sidebar__group-header"
          :class="{ 'sidebar__group-header--collapsed': collapsed }"
          @click="toggleGroup(group.label)"
          @keydown.enter.prevent="toggleGroup(group.label)"
          @keydown.space.prevent="toggleGroup(group.label)"
          tabindex="0"
          role="button"
          :aria-expanded="isGroupExpanded"
          :aria-controls="group.label + '-children'"
        >
          <span class="sidebar__group-icon" aria-hidden="true">
            <svg v-if="group.icon" class="sidebar__icon-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path :d="iconPath(group.icon)" />
            </svg>
          </span>
          <span v-if="!collapsed" class="sidebar__group-label">{{ group.label }}</span>
          <span v-if="group.badge != null && !collapsed" class="sidebar__badge" :class="{ 'sidebar__badge--dot': group.badge === 'dot' }">{{ group.badge === 'dot' ? '' : group.badge }}</span>
          <svg v-if="group.children && group.children.length > 0 && !collapsed" class="sidebar__chevron" :class="{ 'sidebar__chevron--open': isGroupExpanded }" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <polyline points="6 9 12 15 18 9" />
          </svg>
        </div>
      </template>

      <template v-else>
        <SidebarItem
          v-for="(item, itemIndex) in group.children"
          :key="item.route + '-' + itemIndex"
          :item="item"
          :item-index="itemIndex"
          :level="1"
          :collapsed="collapsed"
          :active-path="activePath"
          :router="router"
          :focused-index="focusedIndex"
          @select="$emit('select', $event)"
          @keydown="(e) => handleKeyDown(e, item, itemIndex, true)"
          @mouseenter="showTooltip(item, $event)"
          @mouseleave="hideTooltip"
        />
      </template>

      <Transition name="sidebar-collapse">
        <ul
          v-if="group.children && group.children.length > 0 && isGroupExpanded && !collapsed"
          :id="group.label + '-children'"
          class="sidebar__children"
          role="group"
          :aria-label="group.label"
        >
          <SidebarItem
            v-for="(item, itemIndex) in group.children"
            :key="item.route + '-' + itemIndex"
            :item="item"
            :item-index="itemIndex"
            :level="1"
            :collapsed="collapsed"
            :active-path="activePath"
            :router="router"
            :focused-index="focusedIndex"
            @select="$emit('select', $event)"
            @keydown="(e) => handleKeyDown(e, item, itemIndex, true)"
            @mouseenter="showTooltip(item, $event)"
            @mouseleave="hideTooltip"
          />
        </ul>
      </Transition>
    </li>
  `,
  methods: {
    iconPath(name) {
      const paths = {
        dashboard: 'M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z',
        server: 'M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z',
        network: 'M4 7V4h16v3M9 20h6M12 4v16',
        database: 'M4 7v11a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7M4 7l8 4 8-4M4 15l8 4 8-4',
        security: 'M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z',
        settings: 'M19.14 12.94a1.5 1.5 0 0 0-.03-1.47l-1.38-1.38a1.5 1.5 0 0 0-2.12 0l-.56.56a4.5 4.5 0 0 1-6.36 0l-.56-.56a1.5 1.5 0 0 0-2.12 0l-1.38 1.38a1.5 1.5 0 0 0 0 2.12l1.38 1.38a1.5 1.5 0 0 0 2.12 0l.56-.56a4.5 4.5 0 0 1 6.36 0l.56.56a1.5 1.5 0 0 0 2.12 0l1.38-1.38a1.5 1.5 0 0 0 .03-1.47z',
        user: 'M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2',
        monitor: 'M22 12s-4 0-4-4 4-4 4-4-4 0-4 4',
        folder: 'M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z',
        file: 'M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z',
        log: 'M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z',
        chart: 'M18 20V10M12 20V4M6 20v-6',
        terminal: 'M4 17l4-4-4-4M12 5v14M20 9l-4 4 4 4',
        cloud: 'M18 10h-1.26A8 8 0 1 0 9 20h9a5 5 0 0 0 0-10z',
        lock: 'M18 8h-1a4 4 0 0 0-7.36 2.5l-1.14 1.14a2.5 2.5 0 0 0-2.88 3.88A7 7 0 0 1 18 8z',
        key: 'M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3',
        users: 'M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2',
        bell: 'M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9',
        gear: 'M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z',
        home: 'M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z',
        menu: 'M3 12h18M3 6h18M3 18h18',
        chevronRight: 'M9 18l6-6-6-6',
        chevronLeft: 'M15 18l-6-6 6-6',
        chevronDown: 'M6 9l6 6 6-6',
        chevronUp: 'M18 15l-6-6-6 6'
      }
      return paths[name] || paths.menu
    }
  }
}

// SidebarItem component
const SidebarItem = {
  name: 'SidebarItem',
  props: {
    item: { type: Object, required: true },
    itemIndex: { type: Number, required: true },
    level: { type: Number, default: 1 },
    collapsed: { type: Boolean, default: false },
    activePath: { type: String, default: '' },
    router: { type: Object, default: null },
    focusedIndex: { type: Number, default: -1 }
  },
  emits: ['select', 'keydown', 'mouseenter', 'mouseleave'],
  setup(props, { emit }) {
    const expandedGroups = ref(new Set())

    const isActive = computed(() => {
      if (!props.activePath) return false
      if (props.item.route && props.activePath.startsWith(props.item.route)) return true
      if (props.item.children) {
        return props.item.children.some(child => props.activePath.startsWith(child.route))
      }
      return false
    })

    const isFocused = computed(() => props.focusedIndex === props.itemIndex)

    const itemClass = computed(() => {
      const classes = [
        'sidebar__item',
        { 'sidebar__item--active': isActive.value },
        { 'sidebar__item--disabled': props.item.disabled },
        { 'sidebar__item--focused': isFocused.value }
      ]
      if (props.level > 0) {
        classes.push('sidebar__item--level-' + props.level)
      }
      return classes
    })

    function handleClick(_event) {
      if (props.item.disabled) return
      emit('select', props.item)
      if (props.router && props.item.route) {
        props.router.push(props.item.route).catch(() => {})
      }
    }

    function handleKeyDown(_event) {
      emit('keydown', event)
    }

    return {
      expandedGroups,
      isActive,
      isFocused,
      itemClass,
      handleClick,
      handleKeyDown
    }
  },
  template: `
    <li :class="itemClass">
      <button
        type="button"
        class="sidebar__item-button"
        :class="{ 'sidebar__item-button--collapsed': collapsed }"
        :disabled="item.disabled"
        :aria-disabled="item.disabled ? 'true' : 'false'"
        :aria-current="isActive ? 'page' : undefined"
        :aria-expanded="item.children && item.children.length > 0 ? (expandedGroups.has(item.label) ? 'true' : 'false') : undefined"
        @click="handleClick"
        @keydown="handleKeyDown"
        @mouseenter="$emit('mouseenter', item, $event)"
        @mouseleave="$emit('mouseleave', $event)"
      >
        <span class="sidebar__item-icon" aria-hidden="true">
          <svg v-if="item.icon" class="sidebar__icon-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path :d="iconPath(item.icon)" />
          </svg>
        </span>
        <span v-if="!collapsed" class="sidebar__item-label">{{ item.label }}</span>
        <span v-if="item.badge != null && !collapsed" class="sidebar__badge" :class="{ 'sidebar__badge--dot': item.badge === 'dot' }">{{ item.badge === 'dot' ? '' : item.badge }}</span>
        <svg v-if="item.children && item.children.length > 0 && !collapsed" class="sidebar__item-chevron" :class="{ 'sidebar__item-chevron--open': expandedGroups.has(item.label) }" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <polyline points="6 9 12 15 18 9" />
        </svg>
      </button>

      <Transition name="sidebar-collapse">
        <ul v-if="item.children && item.children.length > 0 && expandedGroups.has(item.label) && !collapsed" class="sidebar__children" role="group" :aria-label="item.label">
          <SidebarItem
            v-for="(child, childIndex) in item.children"
            :key="child.route + '-' + childIndex"
            :item="child"
            :item-index="childIndex"
            :level="level + 1"
            :collapsed="collapsed"
            :active-path="activePath"
            :router="router"
            :focused-index="-1"
            @select="$emit('select', $event)"
            @keydown="$emit('keydown', $event)"
            @mouseenter="$emit('mouseenter', $event)"
            @mouseleave="$emit('mouseleave', $event)"
          />
        </ul>
      </Transition>
    </li>
  `,
  methods: {
    iconPath(name) {
      const paths = {
        dashboard: 'M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z',
        server: 'M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z',
        network: 'M4 7V4h16v3M9 20h6M12 4v16',
        database: 'M4 7v11a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7M4 7l8 4 8-4M4 15l8 4 8-4',
        security: 'M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z',
        settings: 'M19.14 12.94a1.5 1.5 0 0 0-.03-1.47l-1.38-1.38a1.5 1.5 0 0 0-2.12 0l-.56.56a4.5 4.5 0 0 1-6.36 0l-.56-.56a1.5 1.5 0 0 0-2.12 0l-1.38 1.38a1.5 1.5 0 0 0 0 2.12l1.38 1.38a1.5 1.5 0 0 0 2.12 0l.56-.56a4.5 4.5 0 0 1 6.36 0l.56.56a1.5 1.5 0 0 0 2.12 0l1.38-1.38a1.5 1.5 0 0 0 .03-1.47z',
        user: 'M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2',
        monitor: 'M22 12s-4 0-4-4 4-4 4-4-4 0-4 4',
        folder: 'M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z',
        file: 'M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z',
        log: 'M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z',
        chart: 'M18 20V10M12 20V4M6 20v-6',
        terminal: 'M4 17l4-4-4-4M12 5v14M20 9l-4 4 4 4',
        cloud: 'M18 10h-1.26A8 8 0 1 0 9 20h9a5 5 0 0 0 0-10z',
        lock: 'M18 8h-1a4 4 0 0 0-7.36 2.5l-1.14 1.14a2.5 2.5 0 0 0-2.88 3.88A7 7 0 0 1 18 8z',
        key: 'M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3',
        users: 'M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2',
        bell: 'M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9',
        gear: 'M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z',
        home: 'M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z',
        menu: 'M3 12h18M3 6h18M3 18h18',
        chevronRight: 'M9 18l6-6-6-6',
        chevronLeft: 'M15 18l-6-6 6-6',
        chevronDown: 'M6 9l6 6 6-6',
        chevronUp: 'M18 15l-6-6-6 6'
      }
      return paths[name] || paths.menu
    }
  }
}

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  items: {
    type: Array,
    default: () => []
  },
  accordion: { type: Boolean, default: true },
  collapsible: { type: Boolean, default: true },
  ariaLabel: { type: String, default: 'Main navigation' },
  width: { type: [String, Number], default: 260 },
  collapsedWidth: { type: [String, Number], default: 64 }
})

const emit = defineEmits(['update:modelValue', 'select'])

const collapsed = ref(props.modelValue)
const activePath = ref('/')
const sidebarRef = ref(null)
const scrollRef = ref(null)
const tooltipVisible = ref(false)
const tooltipContent = ref('')
const tooltipStyle = ref({})

const router = useRouter()
const route = useRoute()

watch(() => props.modelValue, (val) => {
  collapsed.value = val
})

watch(collapsed, (val) => {
  emit('update:modelValue', val)
})

watch(() => route.path, (newPath) => {
  activePath.value = newPath
}, { immediate: true })

function toggleCollapse() {
  collapsed.value = !collapsed.value
}

function handleGroupSelect(item) {
  emit('select', item)
}

function handleKeydown(event) {
  if (event.key === 'Tab' && collapsed.value) {
    event.preventDefault()
    const focusable = sidebarRef.value?.querySelectorAll('button:not([disabled]), [href], [tabindex]:not([tabindex="-1"])')
    if (focusable?.length) {
      focusable[0].focus()
    }
  }
}

function showTooltip(label, rect) {
  tooltipContent.value = label
  tooltipVisible.value = true
  tooltipStyle.value = {
    left: rect.right + 8 + 'px',
    top: rect.top + rect.height / 2 + 'px',
    transform: 'translateY(-50%)'
  }
}

function hideTooltip() {
  tooltipVisible.value = false
  tooltipContent.value = ''
}

function blur() {
  sidebarRef.value?.blur()
}

provide('sidebar-context', {
  showTooltip,
  hideTooltip,
  blur
})

onMounted(() => {
  activePath.value = route.path
})
</script>

<style scoped>
.sidebar {
  position: relative;
  display: flex;
  flex-direction: column;
  width: var(--sidebar-width, 260px);
  min-width: var(--sidebar-width, 260px);
  max-width: var(--sidebar-width, 260px);
  height: 100%;
  background: var(--color-surface-1, #0f1011);
  border-right: 1px solid var(--color-hairline, #23252a);
  transition: width 150ms ease, min-width 150ms ease, max-width 150ms ease;
  overflow: hidden;
  z-index: var(--z-sidebar, 200);
}

.sidebar--collapsed {
  width: var(--sidebar-collapsed-width, 64px);
  min-width: var(--sidebar-collapsed-width, 64px);
  max-width: var(--sidebar-collapsed-width, 64px);
}

.sidebar__scroll {
  flex: 1 1 auto;
  overflow-y: auto;
  overflow-x: hidden;
  padding: 16px 0;
  scrollbar-width: thin;
  scrollbar-color: var(--color-hairline, #23252a) transparent;
}

.sidebar__scroll::-webkit-scrollbar { width: 6px; }
.sidebar__scroll::-webkit-scrollbar-track { background: transparent; }
.sidebar__scroll::-webkit-scrollbar-thumb {
  background: var(--color-hairline, #23252a);
  border-radius: 3px;
}
.sidebar__scroll::-webkit-scrollbar-thumb:hover {
  background: var(--color-hairline-strong, #34343a);
}

.sidebar__list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.sidebar__group { display: flex; flex-direction: column; }

.sidebar__group-header {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 12px;
  border-radius: 8px;
  color: var(--color-ink-tertiary, #62666d);
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.4px;
  cursor: pointer;
  transition: color 75ms ease, background-color 75ms ease;
  white-space: nowrap;
  user-select: none;
}

.sidebar__group-header:hover {
  color: var(--color-ink, #f7f8f8);
  background: var(--color-surface-2, #141516);
}

.sidebar__group-header:focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-primary, #5e6ad2) 40%, transparent);
  border-radius: 4px;
}

.sidebar__group-header--collapsed {
  justify-content: center;
  padding: 8px;
}

.sidebar__group-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  flex-shrink: 0;
  color: var(--color-ink-tertiary, #62666d);
}

.sidebar__group-icon .sidebar__icon-svg {
  width: 16px;
  height: 16px;
}

.sidebar__group-label {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sidebar__chevron {
  width: 16px;
  height: 16px;
  flex-shrink: 0;
  color: var(--color-ink-tertiary, #62666d);
  transition: transform 75ms ease;
}

.sidebar__chevron--open { transform: rotate(180deg); }

.sidebar__children {
  list-style: none;
  margin: 0;
  padding: 0;
  padding-left: 12px;
  display: flex;
  flex-direction: column;
  gap: 4px;
  border-left: 1px solid var(--color-hairline, #23252a);
  background: var(--color-surface-2, #141516);
  border-radius: 8px;
  animation: sidebar-collapse-in 150ms ease forwards;
}

@keyframes sidebar-collapse-in {
  from { opacity: 0; transform: translateY(-4px); }
  to { opacity: 1; transform: translateY(0); }
}

.sidebar__item { position: relative; }

.sidebar__item--level-1 { padding-left: 0; }
.sidebar__item--level-2 { padding-left: 12px; }
.sidebar__item--level-3 { padding-left: 24px; }

.sidebar__item-button {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  padding: 8px 12px;
  border: none;
  border-left: 2px solid transparent;
  border-radius: 8px;
  background: transparent;
  color: var(--color-ink-muted, #d0d6e0);
  font-size: 13px;
  font-weight: 500;
  font-family: inherit;
  cursor: pointer;
  text-align: left;
  white-space: nowrap;
  overflow: hidden;
  transition: color 75ms ease, background-color 75ms ease, border-color 75ms ease;
}

.sidebar__item-button:hover:not(:disabled) {
  color: var(--color-ink, #f7f8f8);
  background: var(--color-surface-2, #141516);
}

.sidebar__item-button:focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-primary, #5e6ad2) 40%, transparent);
  border-radius: 4px;
}

.sidebar__item-button:active:not(:disabled) {
  background: var(--color-surface-3, #18191a);
}

.sidebar__item-button--collapsed {
  justify-content: center;
  padding: 8px;
}

.sidebar__item--active > .sidebar__item-button {
  color: var(--color-primary, #5e6ad2);
  background: color-mix(in srgb, var(--color-primary, #5e6ad2) 12%, transparent);
  border-left-color: var(--color-primary, #5e6ad2);
}

.sidebar__item--active > .sidebar__item-button:hover:not(:disabled) {
  background: color-mix(in srgb, var(--color-primary, #5e6ad2) 16%, transparent);
  color: var(--color-primary, #5e6ad2);
}

.sidebar__item--disabled .sidebar__item-button {
  opacity: 0.5;
  cursor: not-allowed;
  pointer-events: none;
}

.sidebar__item-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  flex-shrink: 0;
  color: var(--color-ink-tertiary, #62666d);
  transition: color 75ms ease;
}

.sidebar__item--active > .sidebar__item-button .sidebar__item-icon {
  color: var(--color-primary, #5e6ad2);
}

.sidebar__icon-svg {
  width: 18px;
  height: 18px;
}

.sidebar__item-label {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sidebar__item-chevron {
  width: 16px;
  height: 16px;
  flex-shrink: 0;
  color: var(--color-ink-tertiary, #62666d);
  margin-left: auto;
  transition: transform 75ms ease;
}

.sidebar__item-chevron--open { transform: rotate(180deg); }

.sidebar__badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 18px;
  height: 18px;
  padding: 2px 8px;
  border-radius: 9999px;
  background: var(--color-surface-2, #141516);
  color: var(--color-ink-muted, #d0d6e0);
  font-size: 11px;
  font-weight: 600;
  line-height: 1;
  flex-shrink: 0;
}

.sidebar__badge--dot {
  width: 8px;
  height: 8px;
  min-width: 8px;
  padding: 0;
  border-radius: 50%;
}

.sidebar__toggle {
  position: absolute;
  bottom: 16px;
  left: 50%;
  transform: translateX(-50%);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border: 1px solid var(--color-hairline, #23252a);
  border-radius: 9999px;
  background: var(--color-surface-1, #0f1011);
  color: var(--color-ink-muted, #d0d6e0);
  cursor: pointer;
  transition: all 75ms ease;
  z-index: 10;
  box-shadow: 0 1px 2px rgba(0,0,0,0.05);
}

.sidebar__toggle:hover {
  border-color: var(--color-primary, #5e6ad2);
  color: var(--color-primary, #5e6ad2);
  background: color-mix(in srgb, var(--color-primary, #5e6ad2) 12%, transparent);
}

.sidebar__toggle:focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-primary, #5e6ad2) 40%, transparent);
}

.sidebar__toggle:active {
  transform: translateX(-50%) scale(0.95);
}

.sidebar__toggle--collapsed {
  transform: translateX(-50%) rotate(180deg);
}

.sidebar__toggle-icon {
  width: 16px;
  height: 16px;
  flex-shrink: 0;
  transition: transform 75ms ease;
}

.sidebar__tooltip {
  position: fixed;
  z-index: var(--z-tooltip, 1000);
  padding: 4px 8px;
  border-radius: 4px;
  background: var(--color-surface-3, #18191a);
  color: var(--color-ink, #f7f8f8);
  font-size: 12px;
  font-weight: 500;
  white-space: nowrap;
  pointer-events: none;
  box-shadow: 0 10px 25px rgba(0,0,0,0.15);
  animation: sidebar-tooltip-in 75ms ease forwards;
}

@keyframes sidebar-tooltip-in {
  from { opacity: 0; transform: translateY(-50%) translateX(-4px); }
  to { opacity: 1; transform: translateY(-50%) translateX(0); }
}

.sidebar-tooltip-enter-active,
.sidebar-tooltip-leave-active {
  transition: opacity 75ms ease, transform 75ms ease;
}
.sidebar-tooltip-enter-from {
  opacity: 0;
  transform: translateY(-50%) translateX(-4px);
}
.sidebar-tooltip-leave-to {
  opacity: 0;
  transform: translateY(-50%) translateX(-4px);
}

.sidebar-collapse-enter-active,
.sidebar-collapse-leave-active {
  transition: max-height 150ms ease, opacity 150ms ease, padding 150ms ease;
  overflow: hidden;
}
.sidebar-collapse-enter-from {
  max-height: 0;
  opacity: 0;
  padding-top: 0;
  padding-bottom: 0;
}
.sidebar-collapse-leave-to {
  max-height: 0;
  opacity: 0;
  padding-top: 0;
  padding-bottom: 0;
}

[data-theme="light"] .sidebar {
  background: var(--color-inverse-surface-1, #f5f6f6);
  border-right-color: var(--color-hairline-inverse, #e3e8ee);
}
[data-theme="light"] .sidebar__group-header { color: var(--color-inverse-ink-muted, #64748d); }
[data-theme="light"] .sidebar__group-header:hover {
  color: var(--color-inverse-ink, #000000);
  background: var(--color-inverse-surface-2, #f6f7f7);
}
[data-theme="light"] .sidebar__item-button { color: var(--color-inverse-ink-muted, #64748d); }
[data-theme="light"] .sidebar__item-button:hover:not(:disabled) {
  color: var(--color-inverse-ink, #000000);
  background: var(--color-inverse-surface-2, #f6f7f7);
}
[data-theme="light"] .sidebar__item--active > .sidebar__item-button {
  color: var(--color-primary, #5e6ad2);
  background: color-mix(in srgb, var(--color-primary, #5e6ad2) 12%, transparent);
}
[data-theme="light"] .sidebar__children {
  background: var(--color-inverse-surface-2, #f6f7f7);
  border-left-color: var(--color-hairline-inverse, #e3e8ee);
}
[data-theme="light"] .sidebar__badge {
  background: var(--color-inverse-surface-2, #f6f7f7);
  color: var(--color-inverse-ink-muted, #64748d);
}
[data-theme="light"] .sidebar__toggle {
  background: var(--color-inverse-surface-1, #f5f6f6);
  border-color: var(--color-hairline-inverse, #e3e8ee);
  color: var(--color-inverse-ink-muted, #64748d);
}
[data-theme="light"] .sidebar__scroll { scrollbar-color: var(--color-hairline-inverse, #e3e8ee) transparent; }

@media (prefers-reduced-motion: reduce) {
  .sidebar, .sidebar__item-button, .sidebar__group-header,
  .sidebar__chevron, .sidebar__item-chevron, .sidebar__toggle,
  .sidebar__toggle-icon, .sidebar__tooltip, .sidebar__children {
    transition: none !important;
    animation: none !important;
  }
  .sidebar-collapse-enter-active, .sidebar-collapse-leave-active {
    transition: none !important;
  }
  .sidebar-collapse-enter-from, .sidebar-collapse-leave-to {
    max-height: none;
    opacity: 1;
    padding-top: 0;
    padding-bottom: 0;
  }
}

@media (max-width: 1024px) {
  .sidebar:not(.sidebar--collapsed) {
    position: fixed;
    top: 0; left: 0; bottom: 0;
    z-index: var(--z-drawer, 300);
    box-shadow: -8px 0 30px rgba(28, 35, 64, 0.12);
  }
}
</style>
