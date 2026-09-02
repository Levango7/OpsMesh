// Plugin 插件市场 store — 插件列表、安装/卸载、版本
import { defineStore } from 'pinia'
import {
  getPlugins,
  getPlugin,
  installPlugin,
  uninstallPlugin,
  getPluginVersions,
  getPluginCategories
} from '@/api/plugin'
import { t } from '@/i18n'

export const usePluginStore = defineStore('plugin', {
  state: () => ({
    plugins: [],
    categories: [],
    selectedPlugin: null,
    versions: [],
    searchQuery: '',
    selectedCategory: '',
    loading: false,
    error: ''
  }),
  getters: {
    filteredPlugins: (s) => {
      let result = s.plugins
      if (s.selectedCategory) {
        result = result.filter((p) => p.category === s.selectedCategory)
      }
      if (s.searchQuery) {
        const q = s.searchQuery.toLowerCase()
        result = result.filter((p) =>
          (p.name && p.name.toLowerCase().includes(q)) ||
          (p.description && p.description.toLowerCase().includes(q))
        )
      }
      return result
    },
    installedPlugins: (s) => s.plugins.filter((p) => p.status === 'installed')
  },
  actions: {
    async fetchPlugins() {
      this.loading = true; this.error = ''
      try {
        const data = await getPlugins(this.selectedCategory, this.searchQuery)
        this.plugins = (data && data.plugins) || data || []
      } catch (e) {
        this.error = e.j?.error || t('error.pluginsFailed')
      } finally {
        this.loading = false
      }
    },
    async fetchPlugin(id) {
      try {
        const data = await getPlugin(id)
        this.selectedPlugin = data
      } catch (e) {
        this.error = e.j?.error || t('error.pluginDetailFailed')
      }
    },
    async install(id) {
      return await installPlugin(id)
    },
    async uninstall(id) {
      return await uninstallPlugin(id)
    },
    async fetchVersions(id) {
      try {
        const data = await getPluginVersions(id)
        this.versions = (data && data.versions) || data || []
      } catch {
        this.versions = []
      }
    },
    async fetchCategories() {
      try {
        const data = await getPluginCategories()
        this.categories = (data && data.categories) || data || []
      } catch {
        this.categories = []
      }
    },
    setSearch(query) {
      this.searchQuery = query || ''
    },
    setCategory(category) {
      this.selectedCategory = category || ''
    }
  }
})
