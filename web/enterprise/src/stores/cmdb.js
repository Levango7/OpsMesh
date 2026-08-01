// CMDB store — 类型 / 实例 / 关系图 / 属性模板
import { defineStore } from 'pinia'
import { getCMDBTypes, getCIs, createCI, getCIGraph, getAttrTemplates } from '@/api/cmdb'

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
      try { this.types = await getCMDBTypes() || [] } catch (e) { this.error = e.j?.error || 'CMDB 类型拉取失败' }
    },
    async fetchInstances(type) {
      this.currentType = type || this.currentType
      if (!this.currentType) { this.instances = []; return }
      this.loading = true
      try { this.instances = await getCIs(this.currentType) || [] }
      catch (e) { this.error = e.j?.error || '配置项拉取失败' }
      finally { this.loading = false }
    },
    async fetchTemplates(type) {
      try { this.templates = await getAttrTemplates(type) || [] }
      catch (e) { this.error = e.j?.error || '属性模板拉取失败' }
    },
    async openGraph(id) {
      try { this.graph = await getCIGraph(id) }
      catch (e) { this.error = e.j?.error || '关系图拉取失败' }
    },
    async create(body) {
      const r = await createCI(body)
      await this.fetchInstances()
      return r
    }
  }
})