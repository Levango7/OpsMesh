<template>
  <header
    ref="headerRef"
    class="header"
    :class="headerClass"
    role="banner"
    :aria-label="t('ui.header.label', 'Top navigation bar')"
  >
    <div class="header__left">
      <button
        v-if="showSidebarToggle"
        type="button"
        class="header__sidebar-toggle"
        :aria-label="sidebarCollapsed ? t('ui.sidebar.expand', 'Expand sidebar') : t('ui.sidebar.collapse', 'Collapse sidebar')"
        :aria-expanded="!sidebarCollapsed"
        @click="toggleSidebar"
      >
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <line x1="4" y1="6" x2="20" y2="6" />
          <line x1="4" y1="12" x2="20" y2="12" />
          <line x1="4" y1="18" x2="20" y2="18" />
        </svg>
      </button>

      <slot name="left">
        <div v-if="title" class="header__title">
          <slot name="logo">
            <svg class="header__logo" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <path d="M12 2L3 7l9 5 9-5-9-5z" />
              <path d="M3 7v10c0 5.55 3.14 9.24 8 11 4.86-1.76 8-5.45 8-11V7" />
              <path d="M12 12v9" />
            </svg>
          </slot>
          <span class="header__title-text">{{ title }}</span>
        </div>
      </slot>
    </div>

    <div class="header__center">
      <slot name="center" />
    </div>

    <div class="header__right">
      <slot name="right-start" />

      <button
        v-if="showSearch"
        type="button"
        class="header__action"
        :aria-label="t('ui.search.title', 'Search')"
        @click="openSearch"
      >
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <circle cx="11" cy="11" r="8" />
          <line x1="21" y1="21" x2="15.657" y2="15.657" />
        </svg>
        <kbd v-if="showSearchShortcut" class="header__kbd">{{ searchShortcut }}</kbd>
      </button>

      <div class="header__notifications" ref="notificationsRef">
        <button
          v-if="showNotifications"
          type="button"
          class="header__action"
          :class="{ 'header__action--active': hasUnreadNotifications }"
          :aria-label="t('ui.notifications.title', 'Notifications')"
          :aria-expanded="notificationsOpen ? 'true' : 'false'"
          :aria-haspopup="true"
          @click="toggleNotifications"
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9" />
            <path d="M13.73 21a2 2 0 0 1-3.46 0" />
          </svg>
          <span v-if="unreadCount > 0" class="header__badge header__badge--count" :aria-label="unreadCount + ' ' + t('ui.notifications.unread', 'unread')"></span>
        </button>

        <Transition name="header-fade">
          <div
            v-if="notificationsOpen"
            class="header__dropdown header__dropdown--notifications"
            role="menu"
            aria-orientation="vertical"
          >
            <div class="header__dropdown-header">
              <h3 class="header__dropdown-title">{{ t('ui.notifications.title', 'Notifications') }}</h3>
              <button
                v-if="hasUnreadNotifications"
                type="button"
                class="header__dropdown-action"
                @click="markAllAsRead"
                :aria-label="t('ui.notifications.markAllRead', 'Mark all as read')"
              >
                {{ t('ui.notifications.markAllRead', 'Mark all read') }}
              </button>
            </div>
            <div class="header__dropdown-list" ref="notificationsListRef">
              <button
                v-for="item in visibleNotifications"
                :key="item.id"
                type="button"
                class="header__notification"
                :class="{ 'header__notification--unread': item.unread }"
                @click="handleNotificationClick(item)"
              >
                <span class="header__notification-time">{{ formatTime(item.time) }}</span>
                <span class="header__notification-message">{{ item.message }}</span>
              </button>
              <div v-if="notifications.length === 0" class="header__notification-empty">
                {{ t('ui.notifications.empty', 'No notifications') }}
              </div>
            </div>
          </div>
        </Transition>
      </div>

      <div class="header__user" ref="userRef">
        <button
          v-if="showUserMenu && user"
          type="button"
          class="header__action header__action--user"
          :aria-label="t('ui.user.menu', 'User menu')"
          :aria-expanded="userMenuOpen ? 'true' : 'false'"
          :aria-haspopup="true"
          @click="toggleUserMenu"
        >
          <img
            v-if="user.avatar"
            :src="user.avatar"
            :alt="user.name"
            class="header__avatar"
            loading="lazy"
          />
          <svg v-else class="header__avatar-placeholder" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
            <circle cx="12" cy="8" r="5" />
            <path d="M20 21V12A4 4 0 0 0 4 12v9" />
          </svg>
          <span class="header__user-name" v-if="user.name">{{ user.name }}</span>
          <svg class="header__user-arrow" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <polyline points="6 9 12 15 18 9" />
          </svg>
        </button>

        <Transition name="header-fade">
          <div
            v-if="userMenuOpen"
            class="header__dropdown header__dropdown--user"
            role="menu"
            aria-orientation="vertical"
          >
            <a class="header__dropdown-item" href="#" role="menuitem" @click="handleUserAction('profile')">{{ t('ui.user.profile', 'Profile') }}</a>
            <a class="header__dropdown-item" href="#" role="menuitem" @click="handleUserAction('settings')">{{ t('ui.user.settings', 'Settings') }}</a>
            <div class="header__dropdown-divider" role="separator" />
            <a class="header__dropdown-item header__dropdown-item--danger" href="#" role="menuitem" @click="handleUserAction('logout')">{{ t('ui.user.logout', 'Logout') }}</a>
          </div>
        </Transition>
      </div>

      <button
        v-if="showThemeToggle"
        type="button"
        class="header__action"
        :aria-label="isDark ? t('ui.theme.light', 'Light mode') : t('ui.theme.dark', 'Dark mode')"
        @click="toggleTheme"
      >
        <svg v-if="isDark" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <circle cx="12" cy="12" r="5" />
          <line x1="12" y1="1" x2="12" y2="3" />
          <line x1="12" y1="21" x2="12" y2="23" />
          <line x1="4.22" y1="4.22" x2="5.64" y2="5.64" />
          <line x1="18.36" y1="18.36" x2="19.78" y2="19.78" />
          <line x1="1" y1="12" x2="3" y2="12" />
          <line x1="21" y1="12" x2="23" y2="12" />
          <line x1="4.22" y1="19.78" x2="5.64" y2="18.36" />
          <line x1="18.36" y1="5.64" x2="19.78" y2="4.22" />
        </svg>
        <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z" />
        </svg>
      </button>

      <slot name="right-end" />
    </div>
  </header>
</template>

<script setup>
import { computed, ref, onMounted, onUnmounted } from 'vue'
import { useThemeStore } from '@/stores/theme'
import { t } from '@/i18n'

const props = defineProps({
  title: { type: String, default: '' },
  showSidebarToggle: { type: Boolean, default: true },
  showSearch: { type: Boolean, default: true },
  showNotifications: { type: Boolean, default: true },
  showUserMenu: { type: Boolean, default: true },
  showThemeToggle: { type: Boolean, default: true },
  sidebarCollapsed: { type: Boolean, default: false },
  notifications: { type: Array, default: () => [] },
  user: { type: Object, default: null },
  maxNotifications: { type: Number, default: 8 },
  showSearchShortcut: { type: Boolean, default: true }
})

const emit = defineEmits([
  'sidebar-toggle',
  'open-search',
  'notification-click',
  'user-action',
  'theme-toggle'
])

const themeStore = useThemeStore()

const headerRef = ref(null)
const notificationsRef = ref(null)
const notificationsListRef = ref(null)
const userRef = ref(null)
const notificationsOpen = ref(false)
const userMenuOpen = ref(false)

const isDark = computed(() => themeStore.isDark)

const searchShortcut = computed(() => {
  if (navigator && navigator.platform && navigator.platform.includes('Mac')) {
    return 'Cmd + K'
  }
  return 'Ctrl + K'
})

const visibleNotifications = computed(() => {
  return props.notifications.slice(0, props.maxNotifications)
})

const headerClass = computed(() => [
  'header',
  {
    'header--collapsed': props.sidebarCollapsed,
    'header--has-notifications': hasUnreadNotifications.value
  }
])

const unreadCount = computed(() => {
  return props.notifications.filter(n => n.unread).length
})

const hasUnreadNotifications = computed(() => {
  return unreadCount.value > 0
})

function toggleSidebar() {
  emit('sidebar-toggle', !props.sidebarCollapsed)
}

function openSearch() {
  emit('open-search')
}

function toggleNotifications() {
  notificationsOpen.value = !notificationsOpen.value
  userMenuOpen.value = false
}

function toggleUserMenu() {
  userMenuOpen.value = !userMenuOpen.value
  notificationsOpen.value = false
}

function markAllAsRead() {
  notificationsOpen.value = false
}

function handleNotificationClick(item) {
  emit('notification-click', item)
  notificationsOpen.value = false
}

function handleUserAction(action) {
  emit('user-action', action)
  userMenuOpen.value = false
}

function toggleTheme() {
  themeStore.toggle()
  emit('theme-toggle', isDark.value)
}

function formatTime(time) {
  if (!time) return ''
  try {
    const date = new Date(time)
    const now = new Date()
    const diff = now.getTime() - date.getTime()
    const minutes = Math.floor(diff / 60000)
    if (minutes < 1) return t('ui.time.justNow', 'Just now')
    if (minutes < 60) return t('ui.time.minutesAgo', '{minutes} min ago', { minutes })
    const hours = Math.floor(minutes / 60)
    if (hours < 24) return t('ui.time.hoursAgo', '{hours} h ago', { hours })
    const days = Math.floor(hours / 24)
    return t('ui.time.daysAgo', '{days} d ago', { days })
  } catch {
    return String(time)
  }
}

function handleKeydown(evt) {
  if (evt.key === 'Escape') {
    notificationsOpen.value = false
    userMenuOpen.value = false
  }
}

function handleClickOutside(evt) {
  if (notificationsOpen.value && !notificationsRef.value?.contains(evt.target)) {
    notificationsOpen.value = false
  }
  if (userMenuOpen.value && !userRef.value?.contains(evt.target)) {
    userMenuOpen.value = false
  }
}

onMounted(() => {
  document.addEventListener('keydown', handleKeydown)
  document.addEventListener('click', handleClickOutside, true)
})

onUnmounted(() => {
  document.removeEventListener('keydown', handleKeydown)
  document.removeEventListener('click', handleClickOutside, true)
})
</script>

<style scoped>
.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 56px;
  gap: var(--space-3, 12px);
  padding: 0 var(--space-4, 16px);
  border-bottom: 1px solid var(--color-hairline, #23252a);
  background: var(--color-surface-1, #0f1011);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  position: sticky;
  top: 0;
  z-index: 50;
  transition: border-color 150ms ease, background-color 150ms ease;
}

.header__left {
  display: inline-flex;
  align-items: center;
  gap: var(--space-3, 12px);
  flex-shrink: 0;
}

.header__sidebar-toggle {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  border: none;
  border-radius: var(--radius-md, 8px);
  background: transparent;
  color: var(--color-ink-muted, #d0d6e0);
  cursor: pointer;
  transition: background-color 150ms ease, color 150ms ease, box-shadow 150ms ease;
  padding: 0;
  line-height: 1;
}

.header__sidebar-toggle:hover {
  background: var(--color-surface-2, #141516);
  color: var(--color-ink, #f7f8f8);
}

.header__sidebar-toggle:focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-primary, #5e6ad2) 40%, transparent);
}

.header__title {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2, 8px);
  font-weight: var(--font-weight-semibold, 600);
  color: var(--color-ink, #f7f8f8);
}

.header__logo {
  width: 24px;
  height: 24px;
  color: var(--color-primary, #5e6ad2);
}

.header__center {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}

.header__right {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1, 4px);
  flex-shrink: 0;
}

.header__action {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  border: none;
  border-radius: var(--radius-md, 8px);
  background: transparent;
  color: var(--color-ink-muted, #d0d6e0);
  cursor: pointer;
  transition: background-color 150ms ease, color 150ms ease, box-shadow 150ms ease;
  padding: 0;
  line-height: 1;
  position: relative;
}

.header__action:hover {
  background: var(--color-surface-2, #141516);
  color: var(--color-ink, #f7f8f8);
}

.header__action:focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-primary, #5e6ad2) 40%, transparent);
}

.header__action--active {
  color: var(--color-primary, #5e6ad2);
  background: color-mix(in srgb, var(--color-primary, #5e6ad2) 12%, transparent);
}

.header__action--user {
  gap: var(--space-2, 8px);
  padding: 0 var(--space-3, 12px);
  border-radius: var(--radius-md, 8px);
  width: auto;
  height: 36px;
}

.header__avatar {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  object-fit: cover;
}

.header__avatar-placeholder {
  width: 32px;
  height: 32px;
  color: var(--color-ink-tertiary, #62666d);
}

.header__user-name {
  font-size: var(--font-size-sm, 14px);
  font-weight: var(--font-weight-medium, 500);
  color: var(--color-ink, #f7f8f8);
  white-space: nowrap;
  max-width: 80px;
  overflow: hidden;
  text-overflow: ellipsis;
}

.header__user-arrow {
  width: 16px;
  height: 16px;
  color: var(--color-ink-tertiary, #62666d);
  transition: transform 150ms ease;
}

.header__kbd {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 2px 6px;
  margin-left: 4px;
  font-size: 11px;
  font-weight: var(--font-weight-medium, 500);
  color: var(--color-ink-muted, #d0d6e0);
  background: var(--color-surface-3, #18191a);
  border: 1px solid var(--color-hairline, #23252a);
  border-radius: var(--radius-sm, 6px);
}

.header__badge {
  position: absolute;
  top: 6px;
  right: 6px;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--color-error, #ef4444);
  pointer-events: none;
}

.header__dropdown {
  position: absolute;
  top: 100%;
  margin-top: 4px;
  min-width: 200px;
  border: 1px solid var(--color-hairline, #23252a);
  border-radius: 12px;
  background: var(--color-surface-1, #0f1011);
  box-shadow: 0 10px 15px -3px rgb(0 0 0 / 0.1), 0 4px 6px -4px rgb(0 0 0 / 0.1);
  z-index: 100;
  overflow: hidden;
}

.header__dropdown--notifications {
  right: 0;
  width: 320px;
  max-height: 400px;
  display: flex;
  flex-direction: column;
}

.header__dropdown--user {
  right: 0;
  min-width: 160px;
}

.header__dropdown-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--space-2, 8px) var(--space-3, 12px);
  border-bottom: 1px solid var(--color-hairline, #23252a);
}

.header__dropdown-title {
  font-size: var(--font-size-sm, 14px);
  font-weight: var(--font-weight-semibold, 600);
  color: var(--color-ink, #f7f8f8);
  margin: 0;
}

.header__dropdown-action {
  font-size: var(--font-size-xs, 12px);
  color: var(--color-ink-muted, #d0d6e0);
  background: transparent;
  border: none;
  cursor: pointer;
  padding: 4px 8px;
  border-radius: var(--radius-sm, 6px);
  transition: background-color 150ms ease;
}

.header__dropdown-action:hover {
  background: var(--color-surface-2, #141516);
}

.header__dropdown-list {
  overflow-y: auto;
  max-height: 320px;
}

.header__notification {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  width: 100%;
  gap: 4px;
  padding: var(--space-3, 12px);
  border: none;
  border-bottom: 1px solid var(--color-hairline, #23252a);
  background: transparent;
  text-align: left;
  cursor: pointer;
  transition: background-color 150ms ease;
}

.header__notification:hover {
  background: var(--color-surface-2, #141516);
}

.header__notification--unread {
  background: color-mix(in srgb, var(--color-primary, #5e6ad2) 12%, transparent);
}

.header__notification-time {
  font-size: var(--font-size-xs, 12px);
  color: var(--color-ink-tertiary, #62666d);
}

.header__notification-message {
  font-size: var(--font-size-sm, 14px);
  color: var(--color-ink, #f7f8f8);
}

.header__notification-empty {
  padding: var(--space-3, 12px);
  font-size: var(--font-size-sm, 14px);
  color: var(--color-ink-tertiary, #62666d);
  text-align: center;
}

.header__dropdown-divider {
  height: 1px;
  background: var(--color-hairline, #23252a);
  margin: 4px 0;
}

.header__dropdown-item {
  display: block;
  padding: var(--space-2, 8px) var(--space-3, 12px);
  font-size: var(--font-size-sm, 14px);
  color: var(--color-ink, #f7f8f8);
  text-decoration: none;
  cursor: pointer;
  transition: background-color 150ms ease;
  border: none;
  background: transparent;
  text-align: left;
}

.header__dropdown-item:hover,
.header__dropdown-item:focus-visible {
  background: var(--color-surface-2, #141516);
  outline: none;
}

.header__dropdown-item--danger {
  color: var(--color-error, #ef4444);
}

.header-fade-enter-active, .header-fade-leave-active {
  transition: opacity 150ms ease, transform 150ms ease;
}
.header-fade-enter-from, .header-fade-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

@media (max-width: 768px) {
  .header__user-name {
    max-width: 60px;
  }
  .header__dropdown--notifications {
    width: 280px;
    right: -8px;
  }
  .header__dropdown--user {
    right: -8px;
  }
}

@media (max-width: 480px) {
  .header__right {
    gap: var(--space-1, 4px);
  }
  .header__action {
    width: 32px;
    height: 32px;
  }
  .header__user-name {
    display: none;
  }
  .header__action--user {
    padding: 0;
  }
}

@media (prefers-reduced-motion: reduce) {
  .header,
  .header__sidebar-toggle,
  .header__action,
  .header__user-arrow,
  .header__dropdown-action,
  .header__notification,
  .header__dropdown-item,
  .header-fade-enter-active,
  .header-fade-leave-active {
    transition: none !important;
    animation: none !important;
  }
}

[data-theme="light"] .header {
  background: var(--color-inverse-surface-1, #f5f6f6);
  border-bottom-color: var(--color-hairline-inverse, #e3e8ee);
  color: var(--color-inverse-ink, #000000);
}

[data-theme="light"] .header__sidebar-toggle {
  color: var(--color-inverse-ink-muted, #64748d);
}
[data-theme="light"] .header__sidebar-toggle:hover {
  background: var(--color-inverse-surface-2, #f6f7f7);
  color: var(--color-inverse-ink, #000000);
}

[data-theme="light"] .header__title,
[data-theme="light"] .header__user-name {
  color: var(--color-inverse-ink, #000000);
}

[data-theme="light"] .header__action {
  color: var(--color-inverse-ink-muted, #64748d);
}
[data-theme="light"] .header__action:hover {
  background: var(--color-inverse-surface-2, #f6f7f7);
  color: var(--color-inverse-ink, #000000);
}

[data-theme="light"] .header__user-arrow,
[data-theme="light"] .header__avatar-placeholder {
  color: var(--color-inverse-ink-muted, #64748d);
}

[data-theme="light"] .header__kbd {
  background: var(--color-inverse-surface-2, #f6f7f7);
  border-color: var(--color-hairline-inverse, #e3e8ee);
  color: var(--color-inverse-ink-muted, #64748d);
}

[data-theme="light"] .header__dropdown {
  background: var(--color-inverse-surface-1, #f5f6f6);
  border-color: var(--color-hairline-inverse, #e3e8ee);
  box-shadow: 0 10px 15px -3px rgb(0 0 0 / 0.12);
}

[data-theme="light"] .header__dropdown-header {
  border-bottom-color: var(--color-hairline-inverse, #e3e8ee);
}

[data-theme="light"] .header__dropdown-title {
  color: var(--color-inverse-ink, #000000);
}

[data-theme="light"] .header__dropdown-action {
  color: var(--color-inverse-ink-muted, #64748d);
}
[data-theme="light"] .header__dropdown-action:hover {
  background: var(--color-inverse-surface-2, #f6f7f7);
}

[data-theme="light"] .header__notification {
  border-bottom-color: var(--color-hairline-inverse, #e3e8ee);
}
[data-theme="light"] .header__notification:hover {
  background: var(--color-inverse-surface-2, #f6f7f7);
}
[data-theme="light"] .header__notification--unread {
  background: color-mix(in srgb, var(--color-primary, #5e6ad2) 8%, transparent);
}
[data-theme="light"] .header__notification-message {
  color: var(--color-inverse-ink, #000000);
}
[data-theme="light"] .header__notification-time {
  color: var(--color-inverse-ink-muted, #64748d);
}
[data-theme="light"] .header__notification-empty {
  color: var(--color-inverse-ink-muted, #64748d);
}

[data-theme="light"] .header__dropdown-divider {
  background: var(--color-hairline-inverse, #e3e8ee);
}

[data-theme="light"] .header__dropdown-item {
  color: var(--color-inverse-ink, #000000);
}
[data-theme="light"] .header__dropdown-item:hover,
[data-theme="light"] .header__dropdown-item:focus-visible {
  background: var(--color-inverse-surface-2, #f6f7f7);
}
</style>
