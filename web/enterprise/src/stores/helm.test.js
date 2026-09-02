// helm store 单元测试
// 覆盖：初始状态、fetchRepos/createRepo/removeRepo/fetchRepoCharts/searchCharts/
//      fetchReleases/createRelease/upgradeRelease/removeRelease/rollbackRelease/
//      fetchHistory/fetchCatalog 动作、loading/error 状态。
// 说明：源码中方法名为 removeRepo/createRelease/removeRelease（任务描述中的
//      deleteRepo/installRelease/uninstallRelease 为别名语义），本测试按实际导出方法验证。
// 注意：API 函数 searchCharts 与 store action 同名，import 时使用别名 searchChartsApi。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

// mock @/api/helm：避免真实网络请求，用 vi.fn 控制返回值
vi.mock('@/api/helm', () => ({
  getHelmRepos: vi.fn(),
  createHelmRepo: vi.fn(),
  deleteHelmRepo: vi.fn(),
  getRepoCharts: vi.fn(),
  searchCharts: vi.fn(),
  getHelmReleases: vi.fn(),
  createHelmRelease: vi.fn(),
  upgradeHelmRelease: vi.fn(),
  deleteHelmRelease: vi.fn(),
  rollbackHelmRelease: vi.fn(),
  getReleaseHistory: vi.fn(),
  getHelmCatalog: vi.fn(),
}))

import { useHelmStore } from '@/stores/helm'
import {
  getHelmRepos, createHelmRepo, deleteHelmRepo,
  searchCharts as searchChartsApi,
  getHelmReleases, createHelmRelease, deleteHelmRelease,
  rollbackHelmRelease, getReleaseHistory, getHelmCatalog
} from '@/api/helm'

describe('useHelmStore', () => {
  beforeEach(() => {
    // 每个测试前创建新 pinia 实例，隔离状态
    setActivePinia(createPinia())
    // 重置 mock 调用记录
    vi.clearAllMocks()
  })

  describe('初始状态', () => {
    it('repos 初始为空数组', () => {
      const store = useHelmStore()
      expect(store.repos).toEqual([])
    })

    it('charts 初始为空数组', () => {
      const store = useHelmStore()
      expect(store.charts).toEqual([])
    })

    it('releases 初始为空数组', () => {
      const store = useHelmStore()
      expect(store.releases).toEqual([])
    })

    it('history 初始为空数组', () => {
      const store = useHelmStore()
      expect(store.history).toEqual([])
    })

    it('catalog 初始为空数组', () => {
      const store = useHelmStore()
      expect(store.catalog).toEqual([])
    })

    it('loading 初始为 false', () => {
      const store = useHelmStore()
      expect(store.loading).toBe(false)
    })

    it('error 初始为空字符串', () => {
      const store = useHelmStore()
      expect(store.error).toBe('')
    })
  })

  describe('fetchRepos 动作', () => {
    it('成功时设置 repos（r.repos 形态）', async () => {
      const mockRepos = [{ name: 'bitnami', url: 'https://charts.bitnami.com/bitnami' }]
      getHelmRepos.mockResolvedValueOnce({ repos: mockRepos })

      const store = useHelmStore()
      await store.fetchRepos()

      expect(getHelmRepos).toHaveBeenCalled()
      expect(store.repos).toEqual(mockRepos)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('成功时设置 repos（裸数组形态）', async () => {
      const mockRepos = [{ name: 'bitnami' }]
      getHelmRepos.mockResolvedValueOnce(mockRepos)

      const store = useHelmStore()
      await store.fetchRepos()

      expect(store.repos).toEqual(mockRepos)
    })

    it('API 返回 null 时 repos 为空数组', async () => {
      getHelmRepos.mockResolvedValueOnce(null)

      const store = useHelmStore()
      await store.fetchRepos()

      expect(store.repos).toEqual([])
    })

    it('失败时设置 error 并结束 loading', async () => {
      const err = { j: { error: '仓库服务不可用' } }
      getHelmRepos.mockRejectedValueOnce(err)

      const store = useHelmStore()
      await store.fetchRepos()

      expect(store.error).toBe('仓库服务不可用')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getHelmRepos.mockRejectedValueOnce({})

      const store = useHelmStore()
      await store.fetchRepos()

      expect(store.error).toBe('Helm 仓库列表拉取失败')
    })
  })

  describe('createRepo 动作', () => {
    it('成功时调用 API 并刷新列表', async () => {
      const mockResp = { s: 201, j: { name: 'bitnami', url: 'https://charts.bitnami.com/bitnami' } }
      createHelmRepo.mockResolvedValueOnce(mockResp)
      getHelmRepos.mockResolvedValueOnce({ repos: [{ name: 'bitnami' }] })

      const store = useHelmStore()
      const body = { name: 'bitnami', url: 'https://charts.bitnami.com/bitnami' }
      const r = await store.createRepo(body)

      expect(createHelmRepo).toHaveBeenCalledWith(body)
      expect(getHelmRepos).toHaveBeenCalled()
      expect(store.repos).toEqual([{ name: 'bitnami' }])
      expect(r).toEqual(mockResp)
    })

    it('API 抛错时向上抛出异常', async () => {
      createHelmRepo.mockRejectedValueOnce(new Error('create failed'))

      const store = useHelmStore()
      await expect(store.createRepo({})).rejects.toThrow('create failed')
    })
  })

  describe('removeRepo 动作（任务描述中的 deleteRepo）', () => {
    it('成功时调用 API 并刷新列表', async () => {
      const mockResp = { s: 200, j: { status: 'deleted' } }
      deleteHelmRepo.mockResolvedValueOnce(mockResp)
      getHelmRepos.mockResolvedValueOnce({ repos: [] })

      const store = useHelmStore()
      const r = await store.removeRepo('bitnami')

      expect(deleteHelmRepo).toHaveBeenCalledWith('bitnami')
      expect(getHelmRepos).toHaveBeenCalled()
      expect(store.repos).toEqual([])
      expect(r).toEqual(mockResp)
    })

    it('API 抛错时向上抛出异常', async () => {
      deleteHelmRepo.mockRejectedValueOnce(new Error('delete failed'))

      const store = useHelmStore()
      await expect(store.removeRepo('bitnami')).rejects.toThrow('delete failed')
    })
  })

  describe('searchCharts 动作', () => {
    it('成功时设置 charts（r.charts 形态）', async () => {
      const mockCharts = [{ name: 'nginx', version: '1.2.3' }]
      searchChartsApi.mockResolvedValueOnce({ charts: mockCharts })

      const store = useHelmStore()
      await store.searchCharts('nginx')

      expect(searchChartsApi).toHaveBeenCalledWith('nginx')
      expect(store.charts).toEqual(mockCharts)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('成功时设置 charts（裸数组形态）', async () => {
      const mockCharts = [{ name: 'nginx' }]
      searchChartsApi.mockResolvedValueOnce(mockCharts)

      const store = useHelmStore()
      await store.searchCharts('nginx')

      expect(store.charts).toEqual(mockCharts)
    })

    it('API 返回 null 时 charts 为空数组', async () => {
      searchChartsApi.mockResolvedValueOnce(null)

      const store = useHelmStore()
      await store.searchCharts('nginx')

      expect(store.charts).toEqual([])
    })

    it('失败时设置 error 并结束 loading', async () => {
      const err = { j: { error: '搜索超时' } }
      searchChartsApi.mockRejectedValueOnce(err)

      const store = useHelmStore()
      await store.searchCharts('nginx')

      expect(store.error).toBe('搜索超时')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      searchChartsApi.mockRejectedValueOnce({})

      const store = useHelmStore()
      await store.searchCharts('nginx')

      expect(store.error).toBe('Chart 搜索失败')
    })
  })

  describe('fetchReleases 动作', () => {
    it('成功时设置 releases（r.releases 形态）', async () => {
      const mockReleases = [{ name: 'my-app', namespace: 'default', status: 'deployed' }]
      getHelmReleases.mockResolvedValueOnce({ releases: mockReleases })

      const store = useHelmStore()
      await store.fetchReleases()

      expect(getHelmReleases).toHaveBeenCalled()
      expect(store.releases).toEqual(mockReleases)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('成功时设置 releases（裸数组形态）', async () => {
      const mockReleases = [{ name: 'my-app' }]
      getHelmReleases.mockResolvedValueOnce(mockReleases)

      const store = useHelmStore()
      await store.fetchReleases()

      expect(store.releases).toEqual(mockReleases)
    })

    it('API 返回 null 时 releases 为空数组', async () => {
      getHelmReleases.mockResolvedValueOnce(null)

      const store = useHelmStore()
      await store.fetchReleases()

      expect(store.releases).toEqual([])
    })

    it('失败时设置 error 并结束 loading', async () => {
      const err = { j: { error: 'tiller 不可用' } }
      getHelmReleases.mockRejectedValueOnce(err)

      const store = useHelmStore()
      await store.fetchReleases()

      expect(store.error).toBe('tiller 不可用')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getHelmReleases.mockRejectedValueOnce({})

      const store = useHelmStore()
      await store.fetchReleases()

      expect(store.error).toBe('Helm Release 列表拉取失败')
    })
  })

  describe('createRelease 动作（任务描述中的 installRelease）', () => {
    it('成功时调用 API 并刷新列表', async () => {
      const mockResp = { s: 201, j: { name: 'my-app', status: 'deployed', revision: 1 } }
      createHelmRelease.mockResolvedValueOnce(mockResp)
      getHelmReleases.mockResolvedValueOnce({ releases: [{ name: 'my-app' }] })

      const store = useHelmStore()
      const body = { name: 'my-app', chart: 'bitnami/nginx', namespace: 'default' }
      const r = await store.createRelease(body)

      expect(createHelmRelease).toHaveBeenCalledWith(body)
      expect(getHelmReleases).toHaveBeenCalled()
      expect(store.releases).toEqual([{ name: 'my-app' }])
      expect(r).toEqual(mockResp)
    })

    it('API 抛错时向上抛出异常', async () => {
      createHelmRelease.mockRejectedValueOnce(new Error('install failed'))

      const store = useHelmStore()
      await expect(store.createRelease({})).rejects.toThrow('install failed')
    })
  })

  describe('removeRelease 动作（任务描述中的 uninstallRelease）', () => {
    it('成功时调用 API 并刷新列表', async () => {
      const mockResp = { s: 200, j: { status: 'deleted' } }
      deleteHelmRelease.mockResolvedValueOnce(mockResp)
      getHelmReleases.mockResolvedValueOnce({ releases: [] })

      const store = useHelmStore()
      const r = await store.removeRelease('my-app')

      expect(deleteHelmRelease).toHaveBeenCalledWith('my-app')
      expect(getHelmReleases).toHaveBeenCalled()
      expect(store.releases).toEqual([])
      expect(r).toEqual(mockResp)
    })

    it('失败时设置 error、结束 loading 并向上抛出', async () => {
      const err = { j: { error: 'Release 不存在' } }
      deleteHelmRelease.mockRejectedValueOnce(err)

      const store = useHelmStore()
      await expect(store.removeRelease('my-app')).rejects.toThrow()

      expect(store.error).toBe('Release 不存在')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      deleteHelmRelease.mockRejectedValueOnce({})

      const store = useHelmStore()
      await expect(store.removeRelease('my-app')).rejects.toThrow()

      expect(store.error).toBe('Release 卸载失败')
    })
  })

  describe('rollbackRelease 动作', () => {
    it('成功时调用 API 并刷新列表', async () => {
      const mockResp = { s: 200, j: { name: 'my-app', revision: 2, status: 'deployed' } }
      rollbackHelmRelease.mockResolvedValueOnce(mockResp)
      getHelmReleases.mockResolvedValueOnce({ releases: [{ name: 'my-app', revision: 2 }] })

      const store = useHelmStore()
      const body = { revision: 1 }
      const r = await store.rollbackRelease('my-app', body)

      expect(rollbackHelmRelease).toHaveBeenCalledWith('my-app', body)
      expect(getHelmReleases).toHaveBeenCalled()
      expect(store.releases).toEqual([{ name: 'my-app', revision: 2 }])
      expect(r).toEqual(mockResp)
    })

    it('失败时设置 error、结束 loading 并向上抛出', async () => {
      const err = { j: { error: 'revision 无效' } }
      rollbackHelmRelease.mockRejectedValueOnce(err)

      const store = useHelmStore()
      await expect(store.rollbackRelease('my-app', {})).rejects.toThrow()

      expect(store.error).toBe('revision 无效')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      rollbackHelmRelease.mockRejectedValueOnce({})

      const store = useHelmStore()
      await expect(store.rollbackRelease('my-app', {})).rejects.toThrow()

      expect(store.error).toBe('Release 回滚失败')
    })
  })

  describe('fetchHistory 动作', () => {
    it('成功时设置 history（r.history 形态）', async () => {
      const mockHistory = [{ revision: 1, chart: 'nginx', status: 'deployed' }]
      getReleaseHistory.mockResolvedValueOnce({ history: mockHistory })

      const store = useHelmStore()
      await store.fetchHistory('my-app')

      expect(getReleaseHistory).toHaveBeenCalledWith('my-app')
      expect(store.history).toEqual(mockHistory)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('成功时设置 history（裸数组形态）', async () => {
      const mockHistory = [{ revision: 1 }]
      getReleaseHistory.mockResolvedValueOnce(mockHistory)

      const store = useHelmStore()
      await store.fetchHistory('my-app')

      expect(store.history).toEqual(mockHistory)
    })

    it('API 返回 null 时 history 为空数组', async () => {
      getReleaseHistory.mockResolvedValueOnce(null)

      const store = useHelmStore()
      await store.fetchHistory('my-app')

      expect(store.history).toEqual([])
    })

    it('失败时设置 error 并结束 loading', async () => {
      const err = { j: { error: '历史不可用' } }
      getReleaseHistory.mockRejectedValueOnce(err)

      const store = useHelmStore()
      await store.fetchHistory('my-app')

      expect(store.error).toBe('历史不可用')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getReleaseHistory.mockRejectedValueOnce({})

      const store = useHelmStore()
      await store.fetchHistory('my-app')

      expect(store.error).toBe('Release 历史拉取失败')
    })
  })

  describe('fetchCatalog 动作', () => {
    it('成功时设置 catalog（r.categories 形态）', async () => {
      const mockCatalog = [{ name: '数据库', charts: [] }, { name: '消息队列', charts: [] }]
      getHelmCatalog.mockResolvedValueOnce({ categories: mockCatalog })

      const store = useHelmStore()
      await store.fetchCatalog()

      expect(getHelmCatalog).toHaveBeenCalled()
      expect(store.catalog).toEqual(mockCatalog)
      expect(store.loading).toBe(false)
      expect(store.error).toBe('')
    })

    it('成功时设置 catalog（裸数组形态）', async () => {
      const mockCatalog = [{ name: '数据库' }]
      getHelmCatalog.mockResolvedValueOnce(mockCatalog)

      const store = useHelmStore()
      await store.fetchCatalog()

      expect(store.catalog).toEqual(mockCatalog)
    })

    it('API 返回 null 时 catalog 为空数组', async () => {
      getHelmCatalog.mockResolvedValueOnce(null)

      const store = useHelmStore()
      await store.fetchCatalog()

      expect(store.catalog).toEqual([])
    })

    it('失败时设置 error 并结束 loading', async () => {
      const err = { j: { error: '目录服务不可用' } }
      getHelmCatalog.mockRejectedValueOnce(err)

      const store = useHelmStore()
      await store.fetchCatalog()

      expect(store.error).toBe('目录服务不可用')
      expect(store.loading).toBe(false)
    })

    it('失败且无 error 字段时使用 i18n 默认错误码', async () => {
      getHelmCatalog.mockRejectedValueOnce({})

      const store = useHelmStore()
      await store.fetchCatalog()

      expect(store.error).toBe('应用目录拉取失败')
    })
  })
})