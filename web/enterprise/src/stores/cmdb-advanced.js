// CMDB 高级 store — 变更审批 / 关系 / 待审批 CI / 属性模板
import { defineStore } from 'pinia'
import {
  collectCMDB,
  getCMDBChanges, createCMDBChange, getCMDBChange, approveCMDBChange, rejectCMDBChange,
  getRelations, createRelation,
  exportCIs, importCIs, getPendingCIs, getCIRelations, updateCI, deleteCI, approveCI, rejectCI
} from '@/api/cmdb-advanced'
import {
  getAttrTemplates, createAttrTemplate, getAttrTemplate, updateAttrTemplate, deleteAttrTemplate
} from '@/api/cmdb-attr-templates'
import { t } from '@/i18n'

export const useCMDBAdvancedStore = defineStore('cmdbAdvanced', {
  state: () => ({
    // 变更审批
    changes: [],
    currentChange: null,
    // 关系
    relations: [],
    // 待审批 CI
    pendingCIs: [],
    // CI 关系（按 CI 缓存）
    ciRelations: [],
    // 属性模板
    templates: [],
    currentTemplate: null,
    // 最近一次采集 / 导入 / 导出结果
    lastCollect: null,
    lastImport: null,
    lastExport: null,
    loading: false,
    error: ''
  }),
  actions: {
    // ---------- 采集 ----------
    async collect() {
      this.loading = true; this.error = ''
      try {
        const r = await collectCMDB()
        this.lastCollect = r.j || null
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.cmdbCollectFailed')
        throw e
      } finally {
        this.loading = false
      }
    },

    // ---------- 变更审批 ----------
    async fetchChanges(params) {
      this.loading = true; this.error = ''
      try {
        const r = await getCMDBChanges(params) || {}
        this.changes = r.changes || []
      } catch (e) {
        this.error = e.j?.error || t('error.cmdbChangesFailed')
      } finally {
        this.loading = false
      }
    },
    async createChange(body) {
      this.loading = true; this.error = ''
      try {
        const r = await createCMDBChange(body)
        await this.fetchChanges()
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.cmdbChangeCreateFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    async fetchChange(id) {
      this.loading = true; this.error = ''
      try {
        this.currentChange = await getCMDBChange(id)
        return this.currentChange
      } catch (e) {
        this.error = e.j?.error || t('error.cmdbChangeDetailFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    async approveChange(id) {
      this.loading = true; this.error = ''
      try {
        const r = await approveCMDBChange(id)
        await this.fetchChanges()
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.cmdbChangeApproveFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    async rejectChange(id) {
      this.loading = true; this.error = ''
      try {
        const r = await rejectCMDBChange(id)
        await this.fetchChanges()
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.cmdbChangeRejectFailed')
        throw e
      } finally {
        this.loading = false
      }
    },

    // ---------- 关系 ----------
    async fetchRelations(params) {
      this.loading = true; this.error = ''
      try {
        const r = await getRelations(params) || {}
        this.relations = r.relations || []
      } catch (e) {
        this.error = e.j?.error || t('error.cmdbRelationsFailed')
      } finally {
        this.loading = false
      }
    },
    async createRelation(body) {
      this.loading = true; this.error = ''
      try {
        const r = await createRelation(body)
        await this.fetchRelations()
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.cmdbRelationCreateFailed')
        throw e
      } finally {
        this.loading = false
      }
    },

    // ---------- CI 导入导出与审批 ----------
    async exportCIs(params) {
      this.loading = true; this.error = ''
      try {
        const r = await exportCIs(params)
        this.lastExport = r || []
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.cmdbExportFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    async importCIs(body) {
      this.loading = true; this.error = ''
      try {
        const r = await importCIs(body)
        this.lastImport = r.j || null
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.cmdbImportFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    async fetchPendingCIs() {
      this.loading = true; this.error = ''
      try {
        const r = await getPendingCIs() || {}
        this.pendingCIs = r.items || []
      } catch (e) {
        this.error = e.j?.error || t('error.cmdbPendingFailed')
      } finally {
        this.loading = false
      }
    },
    async fetchCIRelations(id) {
      this.loading = true; this.error = ''
      try {
        const r = await getCIRelations(id) || {}
        this.ciRelations = r.relations || []
      } catch (e) {
        this.error = e.j?.error || t('error.cmdbCIRelationsFailed')
      } finally {
        this.loading = false
      }
    },
    async updateCI(id, body) {
      this.loading = true; this.error = ''
      try {
        const r = await updateCI(id, body)
        await this.fetchPendingCIs()
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.cmdbCIUpdateFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    async removeCI(id) {
      this.loading = true; this.error = ''
      try {
        const r = await deleteCI(id)
        await this.fetchPendingCIs()
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.cmdbCIDeleteFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    async approveCI(id) {
      this.loading = true; this.error = ''
      try {
        const r = await approveCI(id)
        await this.fetchPendingCIs()
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.cmdbCIApproveFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    async rejectCI(id) {
      this.loading = true; this.error = ''
      try {
        const r = await rejectCI(id)
        await this.fetchPendingCIs()
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.cmdbCIRejectFailed')
        throw e
      } finally {
        this.loading = false
      }
    },

    // ---------- 属性模板 ----------
    async fetchTemplates() {
      this.loading = true; this.error = ''
      try {
        const r = await getAttrTemplates() || {}
        this.templates = r.templates || []
      } catch (e) {
        this.error = e.j?.error || t('error.cmdbAttrTemplatesFailed')
      } finally {
        this.loading = false
      }
    },
    async createTemplate(body) {
      this.loading = true; this.error = ''
      try {
        const r = await createAttrTemplate(body)
        await this.fetchTemplates()
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.cmdbAttrTemplateCreateFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    async fetchTemplate(id) {
      this.loading = true; this.error = ''
      try {
        this.currentTemplate = await getAttrTemplate(id)
        return this.currentTemplate
      } catch (e) {
        this.error = e.j?.error || t('error.cmdbAttrTemplateDetailFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    async updateTemplate(id, body) {
      this.loading = true; this.error = ''
      try {
        const r = await updateAttrTemplate(id, body)
        await this.fetchTemplates()
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.cmdbAttrTemplateUpdateFailed')
        throw e
      } finally {
        this.loading = false
      }
    },
    async removeTemplate(id) {
      this.loading = true; this.error = ''
      try {
        const r = await deleteAttrTemplate(id)
        await this.fetchTemplates()
        return r
      } catch (e) {
        this.error = e.j?.error || t('error.cmdbAttrTemplateDeleteFailed')
        throw e
      } finally {
        this.loading = false
      }
    }
  }
})