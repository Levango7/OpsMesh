<template>
  <nav class="k8s-subnav" role="tablist" :aria-label="$t('k8s.title')">
    <RouterLink
      v-for="item in items"
      :key="item.to"
      :to="item.to"
      class="k8s-subnav__item"
      :class="{ 'is-active': isActive(item.to) }"
      role="tab"
      :aria-selected="isActive(item.to) ? 'true' : 'false'"
      :data-testid="'k8s-subnav-' + item.key"
    >
      <Icon :name="item.icon" :size="15" />
      <span>{{ $t(item.labelKey) }}</span>
    </RouterLink>
  </nav>
</template>

<script setup>
// K8s 模块子页签 — 集群管理 / 资源管理
// 两个子页面共用，切换不重载整页（RouterLink 走 SPA 路由）。
import { useRoute } from 'vue-router'
import Icon from '@/components/Icon.vue'

const route = useRoute()

const items = [
  { key: 'clusters', to: '/k8s/clusters', icon: 'k8s', labelKey: 'k8s.clusters' },
  { key: 'resources', to: '/k8s/resources', icon: 'device', labelKey: 'k8s.resources' }
]

const isActive = (to) => route.path === to || route.path.startsWith(to + '/')
</script>
