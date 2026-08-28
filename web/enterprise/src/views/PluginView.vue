<template>
  <div>
    <h2 data-testid="plugin-title">{{ $t('plugin.title') }}</h2>
    <p class="muted">{{ $t('plugin.desc') }}</p>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <!-- 搜索和过滤 -->
    <div class="flowbar search-bar">
      <div class="field search-field">
        <input
          v-model="searchInput"
          :placeholder="$t('plugin.searchPlaceholder')"
          data-testid="plugin-search-input"
          @input="onSearch"
        />
      </div>
      <div class="field">
        <label>{{ $t('plugin.category') }}</label>
        <select v-model="store.selectedCategory" @change="store.fetchPlugins()" data-testid="plugin-category-select">
          <option value="">{{ $t('plugin.allCategories') }}</option>
          <option v-for="c in store.categories" :key="c.id" :value="c.id">{{ c.name }} ({{ c.count }})</option>
        </select>
      </div>
      <button class="xs outline" @click="store.fetchPlugins()">↻ {{ $t('common.refresh') }}</button>
    </div>

    <!-- 插件网格 -->
    <div v-if="store.loading && !store.plugins.length" class="muted">{{ $t('common.loading') }}</div>
    <div v-else-if="!store.filteredPlugins.length" class="muted">{{ $t('plugin.noPlugins') }}</div>
    <div v-else class="plugin-grid">
      <div
        v-for="p in store.filteredPlugins"
        :key="p.id"
        class="plugin-card"
        :data-testid="'plugin-card-' + p.id"
        @click="openDetail(p.id)"
      >
        <div class="plugin-head">
          <h4 class="plugin-name">{{ p.name }}</h4>
          <StatusBadge :status="p.status === 'installed' ? 'success' : 'info'" :text="p.status === 'installed' ? $t('plugin.installed') : $t('plugin.available')" />
        </div>
        <p class="plugin-desc">{{ p.description }}</p>
        <div class="plugin-meta">
          <span class="plugin-version">v{{ p.version }}</span>
          <span class="plugin-author">{{ p.author }}</span>
          <span class="plugin-downloads">{{ p.downloads || 0 }} {{ $t('plugin.downloads') }}</span>
        </div>
        <div class="plugin-actions" @click.stop>
          <button
            v-if="p.status !== 'installed'"
            class="xs primary"
            @click="onInstall(p.id)"
            data-testid="plugin-install-btn"
          >{{ $t('plugin.install') }}</button>
          <button
            v-else
            class="xs outline"
            style="color: var(--fail); border-color: var(--fail)"
            @click="onUninstall(p.id)"
            data-testid="plugin-uninstall-btn"
          >{{ $t('plugin.uninstall') }}</button>
        </div>
      </div>
    </div>

    <!-- 插件详情抽屉 -->
    <DetailDrawer :open="detailOpen" :title="store.selectedPlugin?.name || ''" @close="detailOpen = false">
      <div v-if="store.selectedPlugin">
        <p class="muted">{{ store.selectedPlugin.description }}</p>
        <table>
          <tr><th>{{ $t('plugin.version') }}</th><td>v{{ store.selectedPlugin.version }}</td></tr>
          <tr><th>{{ $t('plugin.author') }}</th><td>{{ store.selectedPlugin.author }}</td></tr>
          <tr><th>{{ $t('plugin.category') }}</th><td>{{ store.selectedPlugin.category }}</td></tr>
          <tr><th>{{ $t('plugin.downloads') }}</th><td>{{ store.selectedPlugin.downloads || 0 }}</td></tr>
          <tr><th>{{ $t('plugin.status') }}</th><td>{{ store.selectedPlugin.status }}</td></tr>
        </table>
        <h4>{{ $t('plugin.versionHistory') }}</h4>
        <div v-if="!store.versions.length" class="muted">{{ $t('plugin.noVersions') }}</div>
        <table v-else>
          <thead>
            <tr><th>{{ $t('plugin.version') }}</th><th>{{ $t('plugin.changelog') }}</th><th>{{ $t('plugin.releasedAt') }}</th></tr>
          </thead>
          <tbody>
            <tr v-for="v in store.versions" :key="v.version">
              <td>v{{ v.version }}</td>
              <td>{{ v.changelog || '-' }}</td>
              <td>{{ v.releasedAt || '-' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </DetailDrawer>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { usePluginStore } from '@/stores/plugin'
import { t } from '@/i18n'
import StatusBadge from '@/components/StatusBadge.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'

const store = usePluginStore()
const searchInput = ref('')
const detailOpen = ref(false)

let searchTimer = null
function onSearch() {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    store.setSearch(searchInput.value)
    store.fetchPlugins()
  }, 300)
}

async function openDetail(id) {
  await store.fetchPlugin(id)
  await store.fetchVersions(id)
  detailOpen.value = true
}

async function onInstall(id) {
  if (!confirm(t('plugin.installConfirm'))) return
  try {
    const r = await store.install(id)
    if (r.s >= 200 && r.s < 300) {
      await store.fetchPlugins()
    } else {
      alert(r.j?.error || t('plugin.installFail'))
    }
  } catch (e) {
    alert(e.j?.error || t('plugin.installFail'))
  }
}

async function onUninstall(id) {
  if (!confirm(t('plugin.uninstallConfirm'))) return
  try {
    const r = await store.uninstall(id)
    if (r.s >= 200 && r.s < 300) {
      await store.fetchPlugins()
    } else {
      alert(r.j?.error || t('plugin.uninstallFail'))
    }
  } catch (e) {
    alert(e.j?.error || t('plugin.uninstallFail'))
  }
}

onMounted(() => {
  store.fetchPlugins()
  store.fetchCategories()
})
</script>

<style scoped>
.poll-err {
  padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}
.flowbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 16px; }
.search-bar { margin-bottom: 16px; }
.search-field { flex: 1; min-width: 200px; }
.search-field input { width: 100%; }
.flowbar .field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 0; }
.flowbar .field label { margin: 0; }

.plugin-grid {
  display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 14px;
}
.plugin-card {
  background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius);
  padding: 16px; cursor: pointer; transition: .15s;
}
.plugin-card:hover { border-color: var(--accent); box-shadow: var(--shadow); }
.plugin-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px; }
.plugin-name { margin: 0; font-size: 14px; }
.plugin-desc { font-size: 12.5px; color: var(--text-2); margin: 0 0 10px; line-height: 1.5; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
.plugin-meta { display: flex; flex-wrap: wrap; gap: 8px; font-size: 11.5px; color: var(--text-3); margin-bottom: 10px; }
.plugin-actions { display: flex; gap: 6px; }
</style>
