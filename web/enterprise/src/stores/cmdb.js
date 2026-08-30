// CMDB store — 类型 / 实例 / 关系图 / 属性模板
import { defineStore } from 'pinia'
import { getCMDBTypes, getCIs, createCI, getCIGraph, getAttrTemplates } from '@/api/cmdb'
import { t } from '@/i18n'

export const useCmdbStore = defineStore('cmdb', {
  state: () => ({
    types: [],
    currentType: '',
    instances: [],
    templates: [],
    graph: null,           // { centerCI, relations }
    loading: false,
    error: '',
    msg: ''
  }),
  actions: {
    async fetchTypes() {
      try { this.types = await getCMDBTypes() || [] } catch (e) { this.error = e.j?.error || t('cmdb.typesFetchFailed') }
    },
    async fetchInstances(type) {
      this.currentType = type || this.currentType
      if (!this.currentType) { this.instances = []; return }
      this.loading = true
      try { this.instances = await getCIs(this.currentType) || [] }
      catch (e) { this.error = e.j?.error || t('cmdb.instancesFetchFailed') }
      finally { this.loading = false }
    },
    async fetchTemplates(type) {
      try { this.templates = await getAttrTemplates(type) || [] }
      catch (e) { this.error = e.j?.error || t('cmdb.templatesFetchFailed') }
    },
    async openGraph(id) {
      try { this.graph = await getCIGraph(id) }
      catch (e) { this.error = e.j?.error || t('cmdb.graphFetchFailed') }
    },
    async create(body) {
      const r = await createCI(body)
      await this.fetchInstances()
      return r
    }
  }
})