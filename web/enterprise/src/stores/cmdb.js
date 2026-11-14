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
    _instSeq: 0, // 审计整改批次22：类型切换竞态守卫
    msg: ''
  }),
  actions: {
    async fetchTypes() {
      try { this.types = await getCMDBTypes() || [] } catch (e) { this.error = e.j?.error || t('cmdb.typesFetchFailed') }
    },
    async fetchInstances(type) {
      this.currentType = type || this.currentType
      if (!this.currentType) { this.instances = []; return }
      const seq = ++this._instSeq
      this.loading = true
      try {
        const data = await getCIs(this.currentType) || []
        if (seq !== this._instSeq) return // 类型已切换/已有更新请求
        this.instances = data
      } catch (e) {
        if (seq !== this._instSeq) return
        this.error = e.j?.error || t('cmdb.instancesFetchFailed')
      } finally {
        if (seq === this._instSeq) this.loading = false
      }
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