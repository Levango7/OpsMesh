// cmdb-advanced store 单元测试
// 覆盖：初始状态、collect/fetchChanges/approveChange/rejectChange/fetchRelations/
//       createRelation/exportCIs/importCIs/fetchPendingCIs/fetchTemplates/createTemplate
//       动作、loading/error 状态、成功/失败分支、i18n 默认错误码回退。
// 说明：源码中方法名为 fetchTemplates/createTemplate（任务描述中的
//      fetchAttrTemplates/createAttrTemplate 为别名语义），本测试按实际导出方法验证。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

// mock @/api/cmdb-advanced：避免真实网络请求，用 vi.fn 控制返回值
vi.mock('@/api/cmdb-advanced', () => ({
  collectCMDB: vi.fn(),
  getCMDBChanges: vi.fn(),
  createCMDBChange: vi.fn(),
  getCMDBChange: vi.fn(),
  approveCMDBChange: vi.fn(),
  rejectCMDBChange: vi.fn(),
  getRelations: vi.fn(),
  createRelation: vi.fn(),
  exportCIs: vi.fn(),
  importCIs: vi.fn(),
  getPendingCIs: vi.fn(),
  getCIRelations: vi.fn(),
  updateCI: vi.fn(),
  deleteCI: vi.fn(),
  approveCI: vi.fn(),
  rejectCI: vi.fn(),
}))

// mock @/api/cmdb-attr-templates：避免真实网络请求，用 vi.fn 控制返回值
vi.mock('@/api/cmdb-attr-templates', () => ({
  getAttrTemplates: vi.fn(),
  createAttrTemplate: vi.fn(),
  getAttrTemplate: vi.fn(),
  updateAttrTemplate: vi.fn(),
  deleteAttrTemplate: vi.fn(),
}))

import { useCMDBAdvancedStore } from '@/stores/cmdb-advanced'
import {
  collectCMDB,
  getCMDBChanges,
  approveCMDBChange,
  rejectCMDBChange,
  getRelations,
  createRelation,
  exportCIs,
  importCIs,
  getPendingCIs,
} from '@/api/cmdb-advanced'
import { getAttrTemplates, createAttrTemplate } from '@/api/cmdb-attr-templates'

describe('useCMDBAdvancedStore', () => {
  beforeEach(() => {
    // 每个测试前创建新 pinia 实例，隔离状态
    setActivePinia(createPinia())
    // 重置 mock 调用记录
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('changes 初始为空数组', () => {
      const store = useCMDBAdvancedStore()
      expect(store.changes).toEqual([])
    })

    it('currentChange 初始为 null', () => {
      const store = useCMDBAdvancedStore()
      expect(store.currentChange).toBeNull()
    })

    it('relations 初始为空数组', () => {
      const store = useCMDBAdvancedStore()
      expect(store.relations).toEqual([])
    })

    it('pendingCIs 初始为空数组', () => {
      const store = useCMDBAdvancedStore()
      expect(store.pendingCIs).toEqual([])
    })

    it('ciRelations 初始为空数组', () => {
      const store = useCMDBAdvancedStore()
      expect(store.ciRelations).toEqual([])
    })

    it('templates 初始为空数组', () => {
      const store = useCMDBAdvancedStore()
      expect(store.templates).toEqual([])
    })

    it('currentTemplate 初始为 null', () => {
      const store = useCMDBAdvancedStore()
      expect(store.currentTemplate).toBeNull()
    })

    it('lastCollect 初始为 null', () => {
      const store = useCMDBAdvancedStore()
      expect(store.lastCollect).toBeNull()
    })

    it('lastImport 初始为 null', () => {
      const store = useCMDBAdvancedStore()
      expect(store.lastImport).toBeNull()
    })

    it('lastExport 初始为 null', () => {
      const store = useCMDBAdvancedStore()
      expect(store.lastExport).toBeNull()
    })

    it('loading 初始为 false', () => {
      const store = useCMDBAdvancedStore()
      expect(store.loading).toBe(false)
    })

    it('error 初始为空字符串', () => {
      const store = useCMDBAdvancedStore()
      expect(store.error).toBe('')
    })
  })

  describe('collect 动作', () => {
    it('成功时设置 lastCollect 并返回结果', async () => {
      const mockResp = { s: 200, j: { collected: 42, ts: '2026-09-03' } }
      collectCMDB.mockResolvedValueOnce(mockResp)

      const store = useCMDBAdvancedStore()
      const r = await store.collect()

      expect(collectCMDB).toHaveBeenCalled()
      expect(store.lastCollect).toEqual({ collected: 42, ts: '2026-09-03' })
      expect(r).toEqual(mockResp)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('响应体 j 为空时 lastCollect 为 null', async () => {
      collectCMDB.mockResolvedValueOnce({ s: 200, j: null })

      const store = useCMDBAdvancedStore()
      await store.collect()

      expect(store.lastCollect).toBeNull()
      expect(store.loading).toBe(false)
    })

    it('失败时设置 error 并向上抛出异常', async () => {
      const err = { j: { error: '采集器未就绪' } }
      collectCMDB.mockRejectedValueOnce(err)

      const store = useCMDBAdvancedStore()
      await expect(store.collect()).rejects.toEqual(err)

      expect(store.error).toBe('采集器未就绪')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码并抛出', async () => {
      collectCMDB.mockRejectedValueOnce(new Error('network'))

      const store = useCMDBAdvancedStore()
      await expect(store.collect()).rejects.toThrow('network')

      // i18n 默认回退到 zh：error.cmdbCollectFailed → "CMDB 采集触发失败"
      expect(store.error).toBe('CMDB 采集触发失败')
      expect(store.loading).toBe(false)
    })
  })

  describe('fetchChanges 动作', () => {
    it('成功时从 r.changes 设置 changes', async () => {
      const mockResp = { changes: [{ id: 'ch-1', status: 'pending' }] }
      getCMDBChanges.mockResolvedValueOnce(mockResp)

      const store = useCMDBAdvancedStore()
      await store.fetchChanges({ status: 'pending' })

      expect(getCMDBChanges).toHaveBeenCalledWith({ status: 'pending' })
      expect(store.changes).toEqual([{ id: 'ch-1', status: 'pending' }])
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('响应缺少 changes 字段时 changes 为空数组', async () => {
      getCMDBChanges.mockResolvedValueOnce({})

      const store = useCMDBAdvancedStore()
      await store.fetchChanges()

      expect(store.changes).toEqual([])
      expect(store.loading).toBe(false)
    })

    it('API 返回 null 时 changes 为空数组', async () => {
      getCMDBChanges.mockResolvedValueOnce(null)

      const store = useCMDBAdvancedStore()
      await store.fetchChanges()

      expect(store.changes).toEqual([])
      expect(store.loading).toBe(false)
    })

    it('失败时优先使用后端返回的 error 文案', async () => {
      getCMDBChanges.mockRejectedValueOnce({ j: { error: '变更服务不可达' } })

      const store = useCMDBAdvancedStore()
      await store.fetchChanges()

      expect(store.error).toBe('变更服务不可达')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getCMDBChanges.mockRejectedValueOnce({})

      const store = useCMDBAdvancedStore()
      await store.fetchChanges()

      expect(store.error).toBe('CMDB 变更列表拉取失败')
      expect(store.loading).toBe(false)
    })
  })

  describe('approveChange 动作', () => {
    it('成功时调用 API 并刷新列表，返回结果', async () => {
      const mockResp = { s: 200, j: { id: 'ch-1', status: 'approved' } }
      approveCMDBChange.mockResolvedValueOnce(mockResp)
      getCMDBChanges.mockResolvedValueOnce({ changes: [{ id: 'ch-1', status: 'approved' }] })

      const store = useCMDBAdvancedStore()
      const r = await store.approveChange('ch-1')

      expect(approveCMDBChange).toHaveBeenCalledWith('ch-1')
      expect(getCMDBChanges).toHaveBeenCalled()
      expect(store.changes).toEqual([{ id: 'ch-1', status: 'approved' }])
      expect(r).toEqual(mockResp)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('API 抛错时设置 error 并向上抛出异常', async () => {
      const err = { j: { error: '变更已处理' } }
      approveCMDBChange.mockRejectedValueOnce(err)

      const store = useCMDBAdvancedStore()
      await expect(store.approveChange('ch-1')).rejects.toEqual(err)

      expect(store.error).toBe('变更已处理')
      expect(store.loading).toBe(false)
      // 失败时不应触发列表刷新
      expect(getCMDBChanges).not.toHaveBeenCalled()
    })

    it('失败且无 error 字段时使用 i18n 默认错误码并抛出', async () => {
      approveCMDBChange.mockRejectedValueOnce(new Error('network'))

      const store = useCMDBAdvancedStore()
      await expect(store.approveChange('ch-1')).rejects.toThrow('network')

      expect(store.error).toBe('CMDB 变更审批失败')
      expect(store.loading).toBe(false)
    })
  })

  describe('rejectChange 动作', () => {
    it('成功时调用 API 并刷新列表，返回结果', async () => {
      const mockResp = { s: 200, j: { id: 'ch-1', status: 'rejected' } }
      rejectCMDBChange.mockResolvedValueOnce(mockResp)
      getCMDBChanges.mockResolvedValueOnce({ changes: [{ id: 'ch-1', status: 'rejected' }] })

      const store = useCMDBAdvancedStore()
      const r = await store.rejectChange('ch-1')

      expect(rejectCMDBChange).toHaveBeenCalledWith('ch-1')
      expect(getCMDBChanges).toHaveBeenCalled()
      expect(store.changes).toEqual([{ id: 'ch-1', status: 'rejected' }])
      expect(r).toEqual(mockResp)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('API 抛错时设置 error 并向上抛出异常', async () => {
      const err = { j: { error: '变更已处理' } }
      rejectCMDBChange.mockRejectedValueOnce(err)

      const store = useCMDBAdvancedStore()
      await expect(store.rejectChange('ch-1')).rejects.toEqual(err)

      expect(store.error).toBe('变更已处理')
      expect(store.loading).toBe(false)
      expect(getCMDBChanges).not.toHaveBeenCalled()
    })

    it('失败且无 error 字段时使用 i18n 默认错误码并抛出', async () => {
      rejectCMDBChange.mockRejectedValueOnce(new Error('network'))

      const store = useCMDBAdvancedStore()
      await expect(store.rejectChange('ch-1')).rejects.toThrow('network')

      expect(store.error).toBe('CMDB 变更拒绝失败')
      expect(store.loading).toBe(false)
    })
  })

  describe('fetchRelations 动作', () => {
    it('成功时从 r.relations 设置 relations', async () => {
      const mockResp = { relations: [{ id: 'rel-1', type: 'depends_on' }] }
      getRelations.mockResolvedValueOnce(mockResp)

      const store = useCMDBAdvancedStore()
      await store.fetchRelations({ type: 'depends_on' })

      expect(getRelations).toHaveBeenCalledWith({ type: 'depends_on' })
      expect(store.relations).toEqual([{ id: 'rel-1', type: 'depends_on' }])
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('响应缺少 relations 字段时 relations 为空数组', async () => {
      getRelations.mockResolvedValueOnce({})

      const store = useCMDBAdvancedStore()
      await store.fetchRelations()

      expect(store.relations).toEqual([])
      expect(store.loading).toBe(false)
    })

    it('API 返回 null 时 relations 为空数组', async () => {
      getRelations.mockResolvedValueOnce(null)

      const store = useCMDBAdvancedStore()
      await store.fetchRelations()

      expect(store.relations).toEqual([])
      expect(store.loading).toBe(false)
    })

    it('失败时优先使用后端返回的 error 文案', async () => {
      getRelations.mockRejectedValueOnce({ j: { error: '关系服务不可达' } })

      const store = useCMDBAdvancedStore()
      await store.fetchRelations()

      expect(store.error).toBe('关系服务不可达')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getRelations.mockRejectedValueOnce({})

      const store = useCMDBAdvancedStore()
      await store.fetchRelations()

      expect(store.error).toBe('CMDB 关系列表拉取失败')
      expect(store.loading).toBe(false)
    })
  })

  describe('createRelation 动作', () => {
    it('成功时调用 API 并刷新列表，返回结果', async () => {
      const mockResp = { s: 200, j: { id: 'rel-new', type: 'depends_on' } }
      createRelation.mockResolvedValueOnce(mockResp)
      getRelations.mockResolvedValueOnce({ relations: [{ id: 'rel-new' }] })

      const store = useCMDBAdvancedStore()
      const r = await store.createRelation({ from: 'ci-1', to: 'ci-2', type: 'depends_on' })

      expect(createRelation).toHaveBeenCalledWith({ from: 'ci-1', to: 'ci-2', type: 'depends_on' })
      expect(getRelations).toHaveBeenCalled()
      expect(store.relations).toEqual([{ id: 'rel-new' }])
      expect(r).toEqual(mockResp)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('API 抛错时设置 error 并向上抛出异常', async () => {
      const err = { j: { error: '关系已存在' } }
      createRelation.mockRejectedValueOnce(err)

      const store = useCMDBAdvancedStore()
      await expect(store.createRelation({})).rejects.toEqual(err)

      expect(store.error).toBe('关系已存在')
      expect(store.loading).toBe(false)
      expect(getRelations).not.toHaveBeenCalled()
    })

    it('失败且无 error 字段时使用 i18n 默认错误码并抛出', async () => {
      createRelation.mockRejectedValueOnce(new Error('network'))

      const store = useCMDBAdvancedStore()
      await expect(store.createRelation({})).rejects.toThrow('network')

      expect(store.error).toBe('CMDB 关系创建失败')
      expect(store.loading).toBe(false)
    })
  })

  describe('exportCIs 动作', () => {
    it('成功时设置 lastExport 并返回结果', async () => {
      const mockResp = [{ id: 'ci-1', name: 'app-1' }, { id: 'ci-2', name: 'app-2' }]
      exportCIs.mockResolvedValueOnce(mockResp)

      const store = useCMDBAdvancedStore()
      const r = await store.exportCIs({ type: 'application' })

      expect(exportCIs).toHaveBeenCalledWith({ type: 'application' })
      expect(store.lastExport).toEqual(mockResp)
      expect(r).toEqual(mockResp)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('API 返回 null 时 lastExport 为空数组', async () => {
      exportCIs.mockResolvedValueOnce(null)

      const store = useCMDBAdvancedStore()
      await store.exportCIs()

      expect(store.lastExport).toEqual([])
      expect(store.loading).toBe(false)
    })

    it('失败时设置 error 并向上抛出异常', async () => {
      const err = { j: { error: '导出失败' } }
      exportCIs.mockRejectedValueOnce(err)

      const store = useCMDBAdvancedStore()
      await expect(store.exportCIs()).rejects.toEqual(err)

      expect(store.error).toBe('导出失败')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码并抛出', async () => {
      exportCIs.mockRejectedValueOnce(new Error('network'))

      const store = useCMDBAdvancedStore()
      await expect(store.exportCIs()).rejects.toThrow('network')

      expect(store.error).toBe('CMDB 导出失败')
      expect(store.loading).toBe(false)
    })
  })

  describe('importCIs 动作', () => {
    it('成功时设置 lastImport 并返回结果', async () => {
      const mockResp = { s: 200, j: { imported: 10, failed: 1 } }
      importCIs.mockResolvedValueOnce(mockResp)

      const store = useCMDBAdvancedStore()
      const r = await store.importCIs({ data: 'csv,content' })

      expect(importCIs).toHaveBeenCalledWith({ data: 'csv,content' })
      expect(store.lastImport).toEqual({ imported: 10, failed: 1 })
      expect(r).toEqual(mockResp)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('响应体 j 为空时 lastImport 为 null', async () => {
      importCIs.mockResolvedValueOnce({ s: 200, j: null })

      const store = useCMDBAdvancedStore()
      await store.importCIs({})

      expect(store.lastImport).toBeNull()
      expect(store.loading).toBe(false)
    })

    it('失败时设置 error 并向上抛出异常', async () => {
      const err = { j: { error: '导入格式非法' } }
      importCIs.mockRejectedValueOnce(err)

      const store = useCMDBAdvancedStore()
      await expect(store.importCIs({})).rejects.toEqual(err)

      expect(store.error).toBe('导入格式非法')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码并抛出', async () => {
      importCIs.mockRejectedValueOnce(new Error('network'))

      const store = useCMDBAdvancedStore()
      await expect(store.importCIs({})).rejects.toThrow('network')

      expect(store.error).toBe('CMDB 导入失败')
      expect(store.loading).toBe(false)
    })
  })

  describe('fetchPendingCIs 动作', () => {
    it('成功时从 r.items 设置 pendingCIs', async () => {
      const mockResp = { items: [{ id: 'ci-1', name: 'app-1', status: 'pending' }] }
      getPendingCIs.mockResolvedValueOnce(mockResp)

      const store = useCMDBAdvancedStore()
      await store.fetchPendingCIs()

      expect(getPendingCIs).toHaveBeenCalled()
      expect(store.pendingCIs).toEqual([{ id: 'ci-1', name: 'app-1', status: 'pending' }])
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('响应缺少 items 字段时 pendingCIs 为空数组', async () => {
      getPendingCIs.mockResolvedValueOnce({})

      const store = useCMDBAdvancedStore()
      await store.fetchPendingCIs()

      expect(store.pendingCIs).toEqual([])
      expect(store.loading).toBe(false)
    })

    it('API 返回 null 时 pendingCIs 为空数组', async () => {
      getPendingCIs.mockResolvedValueOnce(null)

      const store = useCMDBAdvancedStore()
      await store.fetchPendingCIs()

      expect(store.pendingCIs).toEqual([])
      expect(store.loading).toBe(false)
    })

    it('失败时优先使用后端返回的 error 文案', async () => {
      getPendingCIs.mockRejectedValueOnce({ j: { error: '待审批列表不可达' } })

      const store = useCMDBAdvancedStore()
      await store.fetchPendingCIs()

      expect(store.error).toBe('待审批列表不可达')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getPendingCIs.mockRejectedValueOnce({})

      const store = useCMDBAdvancedStore()
      await store.fetchPendingCIs()

      expect(store.error).toBe('待审批 CI 拉取失败')
      expect(store.loading).toBe(false)
    })
  })

  describe('fetchTemplates 动作', () => {
    it('成功时从 r.templates 设置 templates', async () => {
      const mockResp = { templates: [{ id: 'tpl-1', name: 'app-template' }] }
      getAttrTemplates.mockResolvedValueOnce(mockResp)

      const store = useCMDBAdvancedStore()
      await store.fetchTemplates()

      expect(getAttrTemplates).toHaveBeenCalled()
      expect(store.templates).toEqual([{ id: 'tpl-1', name: 'app-template' }])
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('响应缺少 templates 字段时 templates 为空数组', async () => {
      getAttrTemplates.mockResolvedValueOnce({})

      const store = useCMDBAdvancedStore()
      await store.fetchTemplates()

      expect(store.templates).toEqual([])
      expect(store.loading).toBe(false)
    })

    it('API 返回 null 时 templates 为空数组', async () => {
      getAttrTemplates.mockResolvedValueOnce(null)

      const store = useCMDBAdvancedStore()
      await store.fetchTemplates()

      expect(store.templates).toEqual([])
      expect(store.loading).toBe(false)
    })

    it('失败时优先使用后端返回的 error 文案', async () => {
      getAttrTemplates.mockRejectedValueOnce({ j: { error: '模板服务不可达' } })

      const store = useCMDBAdvancedStore()
      await store.fetchTemplates()

      expect(store.error).toBe('模板服务不可达')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getAttrTemplates.mockRejectedValueOnce({})

      const store = useCMDBAdvancedStore()
      await store.fetchTemplates()

      expect(store.error).toBe('属性模板列表拉取失败')
      expect(store.loading).toBe(false)
    })
  })

  describe('createTemplate 动作', () => {
    it('成功时调用 API 并刷新列表，返回结果', async () => {
      const mockResp = { s: 200, j: { id: 'tpl-new', name: 'db-template' } }
      createAttrTemplate.mockResolvedValueOnce(mockResp)
      getAttrTemplates.mockResolvedValueOnce({ templates: [{ id: 'tpl-new' }] })

      const store = useCMDBAdvancedStore()
      const r = await store.createTemplate({ name: 'db-template', attrs: [] })

      expect(createAttrTemplate).toHaveBeenCalledWith({ name: 'db-template', attrs: [] })
      expect(getAttrTemplates).toHaveBeenCalled()
      expect(store.templates).toEqual([{ id: 'tpl-new' }])
      expect(r).toEqual(mockResp)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('API 抛错时设置 error 并向上抛出异常', async () => {
      const err = { j: { error: '模板名重复' } }
      createAttrTemplate.mockRejectedValueOnce(err)

      const store = useCMDBAdvancedStore()
      await expect(store.createTemplate({})).rejects.toEqual(err)

      expect(store.error).toBe('模板名重复')
      expect(store.loading).toBe(false)
      expect(getAttrTemplates).not.toHaveBeenCalled()
    })

    it('失败且无 error 字段时使用 i18n 默认错误码并抛出', async () => {
      createAttrTemplate.mockRejectedValueOnce(new Error('network'))

      const store = useCMDBAdvancedStore()
      await expect(store.createTemplate({})).rejects.toThrow('network')

      expect(store.error).toBe('属性模板创建失败')
      expect(store.loading).toBe(false)
    })
  })
})